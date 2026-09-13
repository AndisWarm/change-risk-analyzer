#!/usr/bin/env bash
# client/test/install_test.sh — install.sh 的本地集成测试。
#
# 全部请求打向 python3 http.server 起的 127.0.0.1 本地服务器，不访问任何
# 真实发布源；fixture 二进制由 go build 现场构建并注入测试版本号。
#
# 覆盖场景：
#   1. 正常安装（哈希匹配、成员路径合法、--version 冒烟一致）+ 重复执行幂等
#   2. 发布包被篡改 → SHA-256 拒绝
#   3. 非精确版本引用（latest / v9 / main）在任何下载发生前拒绝
#   4. 校验清单缺项拒绝
#   5. 校验清单多行歧义拒绝
#   6. 安装后自报版本与请求不符拒绝
#   7. 恶意归档成员（穿越路径 / 绝对路径）拒绝且零逃逸
#   8. 下载源不可达拒绝
#   9. GITHUB_OUTPUT 存在时写入 path= 与 version=
set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$script_dir/../.." && pwd)
work=$(mktemp -d)
pids_file="$work/server.pids"
: > "$pids_file"

cleanup() {
  while read -r pid; do
    kill "$pid" 2>/dev/null || true
  done < "$pids_file"
  rm -rf "$work"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

FAKE_VERSION=v9.9.9
PKG="go-risk-analyzer-$FAKE_VERSION-linux-amd64"

echo "==> 构建 fixture 二进制（原生平台，注入版本 $FAKE_VERSION）"
cd "$root/server"
mkdir -p "$work/fixture-bin"
go build -ldflags "-X main.AnalyzerVersion=$FAKE_VERSION" \
  -o "$work/fixture-bin/go-risk-analyzer" ./cmd/go-risk-analyzer
go build -ldflags "-X main.AnalyzerVersion=v9.9.8" \
  -o "$work/fixture-bin/go-risk-analyzer-old" ./cmd/go-risk-analyzer
cd "$root"

# make_release <release-root> <binary-source>
# 按切片 16 的发布布局打包：顶层目录 + go-risk-analyzer + RELEASE_INFO.txt，
# 并生成 sha256sum 校验清单。
make_release() {
  local rel_root=$1 bin=$2
  local tag_dir="$rel_root/$FAKE_VERSION"
  rm -rf "$rel_root"
  mkdir -p "$tag_dir/$PKG"
  cp "$bin" "$tag_dir/$PKG/go-risk-analyzer"
  printf 'package: change-risk-analyzer\nversion: %s\n' "$FAKE_VERSION" > "$tag_dir/$PKG/RELEASE_INFO.txt"
  tar --sort=name --mtime='1970-01-01 00:00:00 UTC' --owner=0 --group=0 --numeric-owner \
    -C "$tag_dir" -cf - "$PKG" | gzip -n > "$tag_dir/$PKG.tar.gz"
  (cd "$tag_dir" && sha256sum "$PKG.tar.gz" > "checksums-$FAKE_VERSION-linux-amd64.txt")
}

# run_server <release-root>：在空闲端口起本地 HTTP 服务器并设置 base 变量。
# python 段把选中的端口写到 stdout，由调用方捕获进 $base。
run_server() {
  local dir=$1
  local port
  port=$(python3 - "$dir" "$pids_file" <<'PY'
import socket, subprocess, sys, time
s = socket.socket()
s.bind(('127.0.0.1', 0))
port = s.getsockname()[1]
s.close()
proc = subprocess.Popen(
    [sys.executable, '-m', 'http.server', str(port), '--bind', '127.0.0.1', '--directory', sys.argv[1]],
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
open(sys.argv[2], 'a').write(f"{proc.pid}\n")
for _ in range(50):
    try:
        with socket.create_connection(('127.0.0.1', port), timeout=0.2):
            break
    except OSError:
        time.sleep(0.1)
else:
    proc.terminate()
    sys.exit('server failed to start')
print(port)
PY
)
  base="http://127.0.0.1:$port"
}

# ---- 用例 1：正常安装 + 幂等 ----
echo "==> 用例 1：正常安装"
rel1="$work/rel-ok"
make_release "$rel1" "$work/fixture-bin/go-risk-analyzer"
run_server "$rel1"
out1=$(bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case1")
grep -q "^path=$work/case1/go-risk-analyzer$" <<< "$out1" || fail "用例1 stdout 缺少 path= 行: $out1"
grep -q "^version=$FAKE_VERSION$" <<< "$out1" || fail "用例1 stdout 缺少 version= 行: $out1"
[ -f "$work/case1/go-risk-analyzer" ] || fail "用例1 二进制未安装"
[ -f "$work/case1/RELEASE_INFO.txt" ] || fail "用例1 来源说明未安装"
"$work/case1/go-risk-analyzer" --version | grep -q "$FAKE_VERSION" || fail "用例1 安装二进制版本不符"

echo "==> 用例 1b：重复执行幂等"
out1b=$(bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case1") ||
  fail "用例1b 重复安装应成功"
grep -q "^version=$FAKE_VERSION$" <<< "$out1b" || fail "用例1b 输出异常: $out1b"

echo "==> 用例 9：GITHUB_OUTPUT 写入"
gh_out="$work/github_output.txt"
: > "$gh_out"
GITHUB_OUTPUT="$gh_out" bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case9" >/dev/null ||
  fail "用例9 安装应成功"
grep -q "^version=$FAKE_VERSION$" "$gh_out" || fail "用例9 GITHUB_OUTPUT 缺少 version="
grep -q "^path=$work/case9/go-risk-analyzer$" "$gh_out" || fail "用例9 GITHUB_OUTPUT 缺少 path="

# ---- 用例 2：篡改发布包 → SHA-256 拒绝 ----
echo "==> 用例 2：篡改哈希拒绝"
rel2="$work/rel-tampered"
make_release "$rel2" "$work/fixture-bin/go-risk-analyzer"
printf 'X' >> "$rel2/$FAKE_VERSION/$PKG.tar.gz"
run_server "$rel2"
if bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case2" 2>"$work/err2"; then
  fail "用例2 篡改包不应安装成功"
fi
grep -q "SHA-256 校验失败" "$work/err2" || fail "用例2 错误信息缺少 SHA-256 说明: $(cat "$work/err2")"
[ ! -e "$work/case2" ] || fail "用例2 失败后不应留下安装目录"

# ---- 用例 3：非精确版本在任何下载前拒绝 ----
echo "==> 用例 3：浮动版本拒绝"
# 故意指向不可达端口：若安装器先做网络请求就会得到 curl 错误而非版本校验错误。
for bad in latest v9 main; do
  if bash "$root/client/install.sh" --release-base "http://127.0.0.1:1" --version "$bad" --install-dir "$work/case3" 2>"$work/err3"; then
    fail "用例3 版本 $bad 不应通过"
  fi
  grep -q "不是精确版本" "$work/err3" || fail "用例3 版本 $bad 错误信息不符: $(cat "$work/err3")"
  if grep -q "curl" "$work/err3"; then
    fail "用例3 版本 $bad 不应发起下载"
  fi
done

# ---- 用例 4：校验清单缺项拒绝 ----
echo "==> 用例 4：清单缺项拒绝"
rel4="$work/rel-missing"
make_release "$rel4" "$work/fixture-bin/go-risk-analyzer"
printf 'deadbeef  some-other-file.tar.gz\n' > "$rel4/$FAKE_VERSION/checksums-$FAKE_VERSION-linux-amd64.txt"
run_server "$rel4"
if bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case4" 2>"$work/err4"; then
  fail "用例4 清单缺项不应安装成功"
fi
grep -q "恰好一行" "$work/err4" || fail "用例4 错误信息不符: $(cat "$work/err4")"

# ---- 用例 5：清单多行歧义拒绝 ----
echo "==> 用例 5：清单多行拒绝"
rel5="$work/rel-dup"
make_release "$rel5" "$work/fixture-bin/go-risk-analyzer"
hash=$(sha256sum "$rel5/$FAKE_VERSION/$PKG.tar.gz" | awk '{print $1}')
printf '%s  %s.tar.gz\n%s  %s.tar.gz\n' "$hash" "$PKG" "$hash" "$PKG" > "$rel5/$FAKE_VERSION/checksums-$FAKE_VERSION-linux-amd64.txt"
run_server "$rel5"
if bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case5" 2>"$work/err5"; then
  fail "用例5 清单多行不应安装成功"
fi
grep -q "恰好一行" "$work/err5" || fail "用例5 错误信息不符: $(cat "$work/err5")"

# ---- 用例 6：安装后自报版本不符拒绝 ----
echo "==> 用例 6：版本冒烟不符拒绝"
rel6="$work/rel-smoke"
make_release "$rel6" "$work/fixture-bin/go-risk-analyzer-old"
run_server "$rel6"
if bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case6" 2>"$work/err6"; then
  fail "用例6 版本冒烟不符不应安装成功"
fi
grep -q "自报版本与请求不符" "$work/err6" || fail "用例6 错误信息不符: $(cat "$work/err6")"

# ---- 用例 7：恶意归档成员拒绝且零逃逸 ----
echo "==> 用例 7：恶意路径成员拒绝"
rel7="$work/rel-evil"
mkdir -p "$rel7"
python3 - "$rel7" "$FAKE_VERSION" <<'PY'
import io, os, sys, tarfile
rel_root, version = sys.argv[1], sys.argv[2]
pkg = f"go-risk-analyzer-{version}-linux-amd64"
tag_dir = os.path.join(rel_root, version)
os.makedirs(tag_dir, exist_ok=True)
with tarfile.open(os.path.join(tag_dir, pkg + ".tar.gz"), "w:gz") as t:
    d = tarfile.TarInfo(f"{pkg}/")
    d.type = tarfile.DIRTYPE
    t.addfile(d)

    def add_file(name, data):
        info = tarfile.TarInfo(name)
        info.size = len(data)
        t.addfile(info, io.BytesIO(data))

    add_file(f"{pkg}/go-risk-analyzer", b"not-a-real-binary")
    add_file("../evil-escaped.txt", b"pwned")
    add_file("/tmp/evil-absolute.txt", b"pwned")
PY
(cd "$rel7/$FAKE_VERSION" && sha256sum "$PKG.tar.gz" > "checksums-$FAKE_VERSION-linux-amd64.txt")
run_server "$rel7"
if bash "$root/client/install.sh" --release-base "$base" --version "$FAKE_VERSION" --install-dir "$work/case7" 2>"$work/err7"; then
  fail "用例7 恶意成员不应安装成功"
fi
grep -qE "可疑路径成员|意外顶层成员" "$work/err7" || fail "用例7 错误信息不符: $(cat "$work/err7")"
[ ! -e "$work/evil-escaped.txt" ] || fail "用例7 穿越文件逃逸"
[ ! -e "/tmp/evil-absolute.txt" ] || fail "用例7 绝对路径文件逃逸"

# ---- 用例 8：下载源不可达拒绝 ----
echo "==> 用例 8：下载源不可达拒绝"
if bash "$root/client/install.sh" --release-base "http://127.0.0.1:1" --version "$FAKE_VERSION" --install-dir "$work/case8" 2>"$work/err8"; then
  fail "用例8 不可达源不应安装成功"
fi
grep -qE "curl|下载" "$work/err8" || fail "用例8 错误信息应体现下载失败: $(cat "$work/err8")"

echo "全部用例（1/1b/2/3/4/5/6/7/8/9）通过。"
