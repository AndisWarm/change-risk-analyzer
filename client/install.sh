#!/usr/bin/env bash
# client/install.sh — 按精确版本下载、校验并安装 change-risk-analyzer 分析二进制。
#
# 安全设计（spec/08-security-privacy.md、DEVELOPMENT_PLAN 第 8 节）：
#   - 版本必须精确匹配 vMAJOR.MINOR.PATCH；latest、主版本浮动标签、分支名
#     等一切非精确引用一律拒绝（本仓库自己的 CR-SC-001 规则同样禁止浮动引用）；
#   - 下载后与同一发布版本的 SHA-256 校验清单逐字符比对，不匹配即拒绝安装；
#     校验清单缺行或多行视为清单异常，同样拒绝；
#   - 解压前逐个校验归档成员路径：只允许顶层目录内的常规成员，拒绝绝对路径
#     与穿越路径（..），防止恶意发布包在解压时写出目标目录；
#   - 安装完成后执行二进制 --version 冒烟校验：无法执行（架构不符）或自报
#     版本与请求不符时拒绝安装，保证「安装的即所请求的」；
#   - 不执行发布包之外的任何脚本，不读取任何 Secret；全部输出不含敏感内容。
#
# 用法：
#   install.sh --release-base <url> --version <vX.Y.Z> [--install-dir <dir>] [--arch <platform>]
#
# --release-base 指向发布资产根地址，实际下载地址按
#   <release-base>/<version>/<资产名> 拼接（对应 GitHub Releases
#   releases/download/<tag>/<asset> 的扁平资产布局）。
# --arch 默认 linux-amd64（切片 16 目前唯一发布平台）。
#
# 输出约定：最后向 stdout 输出机器可读的 path= 与 version= 两行；
# 若运行于 GitHub Action（存在 GITHUB_OUTPUT），同时追加写入供后续步骤引用。
set -euo pipefail

usage() {
  echo "用法: install.sh --release-base <url> --version <vX.Y.Z> [--install-dir <dir>] [--arch <platform>]" >&2
}

release_base=""
version=""
install_dir="change-risk-analyzer-bin"
arch="linux-amd64"

while [ $# -gt 0 ]; do
  case "$1" in
    --release-base) release_base=${2:-}; shift 2 ;;
    --version) version=${2:-}; shift 2 ;;
    --install-dir) install_dir=${2:-}; shift 2 ;;
    --arch) arch=${2:-}; shift 2 ;;
    *) usage; exit 2 ;;
  esac
done

if [ -z "$release_base" ] || [ -z "$version" ]; then
  usage
  exit 2
fi

# 精确版本强制：任何非 vMAJOR.MINOR.PATCH 形态的引用（latest、v1、main、
# commit SHA、带预发布后缀的版本等）都在发起任何下载之前拒绝。
if ! [[ $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "错误：版本引用 $version 不是精确版本（要求 vMAJOR.MINOR.PATCH）。" >&2
  echo "禁止 latest、v1 等浮动引用；请固定完整版本标签，或在调用时显式传入 analyzer-version。" >&2
  exit 1
fi

case "$release_base" in
  http://* | https://* | file://*) ;;
  *)
    echo "错误：--release-base 必须是 http(s) 或 file:// 地址，收到 $release_base" >&2
    exit 2
    ;;
esac

pkg="go-risk-analyzer-$version-$arch"
archive="$pkg.tar.gz"
checksums="checksums-$version-$arch.txt"
base="${release_base%/}"
url_archive="$base/$version/$archive"
url_checksums="$base/$version/$checksums"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# fetch 统一下载：失败即中止；对暂时性网络错误做有限重试（spec/05 第 8 节），
# 并设置连接与总时长上限，避免异常源拖死安装。
fetch() {
  curl --fail --silent --show-error --location \
    --retry 3 --retry-delay 1 --connect-timeout 10 --max-time 300 \
    -o "$2" "$1"
}

echo "==> 下载校验清单 $url_checksums"
fetch "$url_checksums" "$tmp/$checksums"
echo "==> 下载发布包 $url_archive"
fetch "$url_archive" "$tmp/$archive"

# 校验清单必须恰好包含一行目标资产记录（兼容 sha256sum 的文本与二进制模式）。
hash=$(awk -v name="$archive" '$2 == name || $2 == "*"name { print $1; c++ } END { exit c == 1 ? 0 : 1 }' "$tmp/$checksums") || {
  echo "错误：校验清单 $checksums 中目标资产 $archive 的记录必须恰好一行（当前缺失或有歧义），拒绝安装。" >&2
  exit 1
}
actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
if [ "$actual" != "$hash" ]; then
  echo "错误：SHA-256 校验失败，发布包与校验清单不一致，已拒绝安装。" >&2
  echo "期望 $hash" >&2
  echo "实际 $actual" >&2
  exit 1
fi
echo "==> SHA-256 校验通过"

# 解压前逐个校验归档成员路径，拒绝穿越与意外顶层成员。
while IFS= read -r member; do
  case "$member" in
    "$pkg/") ;;
    "$pkg"/*)
      case "$member" in
        *..* | /*)
          echo "错误：发布包含可疑路径成员 $member，拒绝安装。" >&2
          exit 1
          ;;
      esac
      ;;
    *)
      echo "错误：发布包含意外顶层成员 $member，拒绝安装。" >&2
      exit 1
      ;;
  esac
done < <(tar -tzf "$tmp/$archive")

tar -xzf "$tmp/$archive" -C "$tmp"

mkdir -p "$install_dir"
install_dir_abs=$(cd "$install_dir" && pwd)
mv "$tmp/$pkg/go-risk-analyzer" "$install_dir_abs/go-risk-analyzer"
if [ -f "$tmp/$pkg/RELEASE_INFO.txt" ]; then
  mv "$tmp/$pkg/RELEASE_INFO.txt" "$install_dir_abs/RELEASE_INFO.txt"
fi

# 身份冒烟校验：安装的二进制必须可执行且自报版本与请求一致。
reported=$("$install_dir_abs/go-risk-analyzer" --version) || {
  echo "错误：安装的二进制无法执行（平台或架构不匹配？本安装器默认平台为 $arch），拒绝安装。" >&2
  exit 1
}
case "$reported" in
  *"version $version") ;;
  *)
    echo "错误：安装的二进制自报版本与请求不符（$reported），拒绝安装。" >&2
    exit 1
    ;;
esac

if [ -n "${GITHUB_OUTPUT:-}" ]; then
  {
    echo "path=$install_dir_abs/go-risk-analyzer"
    echo "version=$version"
  } >> "$GITHUB_OUTPUT"
fi
echo "path=$install_dir_abs/go-risk-analyzer"
echo "version=$version"
echo "==> 安装完成：$install_dir_abs/go-risk-analyzer ($version)"
