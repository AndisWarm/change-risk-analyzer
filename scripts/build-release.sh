#!/usr/bin/env bash
# 版本化发布构建脚本（DEVELOPMENT_PLAN 第 7 节任务表切片 16）。
#
# 用法（在仓库根目录执行）：
#   scripts/build-release.sh <version>
#   例如：scripts/build-release.sh v0.1.0
#
# 产物（写入仓库根 dist/，该目录已被 .gitignore 忽略）：
#   dist/go-risk-analyzer-<version>-linux-amd64.tar.gz   二进制发布包
#   dist/checksums-<version>-linux-amd64.txt             SHA-256 校验清单
#
# 包内容（顶层目录 go-risk-analyzer-<version>-linux-amd64/）：
#   go-risk-analyzer    Linux amd64 静态分析二进制（CGO_ENABLED=0）
#   RELEASE_INFO.txt    来源说明：版本、提交 SHA、平台、工具链（spec/08 第 10 节）
#
# 可重复性约定：同一次提交 + 同一 Go 工具链重复执行本脚本，产物逐字节一致。
#   - -trimpath -buildvcs=false：剥离本地路径与 VCS 状态烙印，二进制内容
#     只随源码与注入版本变化；提交 SHA 记录在 RELEASE_INFO.txt 作为来源说明；
#   - ldflags -X 注入版本，--version 与报告 analyzer_version 同步变化；
#   - 归档按文件名排序、固定 mtime/属主，gzip -n 去除时间戳。
# 校验清单可独立复核：
#   cd dist && sha256sum -c checksums-<version>-linux-amd64.txt
#
# 安全约束：脚本不接触任何 Secret，产物只包含构建出的二进制与来源说明。
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)

version=${1:-}
if [[ ! $version =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "用法: scripts/build-release.sh <version>（格式 vMAJOR.MINOR.PATCH，如 v0.1.0）" >&2
  exit 2
fi
command -v go >/dev/null 2>&1 || { echo "错误：未找到 go 命令" >&2; exit 1; }

cd "$root/server"

echo "==> 运行全量测试（发布门禁）"
go test ./...

echo "==> 构建 linux/amd64 静态二进制（版本 $version）"
pkg="go-risk-analyzer-$version-linux-amd64"
dist="$root/dist"
rm -rf "$dist/$pkg" "$dist/$pkg.tar.gz" "$dist/checksums-$version-linux-amd64.txt"
mkdir -p "$dist/$pkg"

commit=$(git -C "$root" rev-parse HEAD)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.AnalyzerVersion=$version" \
  -o "$dist/$pkg/go-risk-analyzer" ./cmd/go-risk-analyzer

echo "==> 写入来源说明 RELEASE_INFO.txt"
cat > "$dist/$pkg/RELEASE_INFO.txt" <<EOF
package: change-risk-analyzer
version: $version
commit: $commit
platform: linux/amd64 (CGO_ENABLED=0)
toolchain: $(go version)
build: scripts/build-release.sh $version
module: change-risk-analyzer (server/)
EOF

echo "==> 打包确定性 tar.gz"
tar --sort=name --mtime='1970-01-01 00:00:00 UTC' --owner=0 --group=0 --numeric-owner \
  -C "$dist" -cf - "$pkg" | gzip -n > "$dist/$pkg.tar.gz"

echo "==> 生成并自检 SHA-256 校验清单"
(
  cd "$dist" &&
    sha256sum "$pkg.tar.gz" > "checksums-$version-linux-amd64.txt" &&
    sha256sum -c "checksums-$version-linux-amd64.txt"
)

echo "==> 构建完成"
ls -l "$dist/$pkg.tar.gz" "$dist/checksums-$version-linux-amd64.txt"
