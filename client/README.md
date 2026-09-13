# GitHub Action 客户端（client/）

本目录是 change-risk-analyzer 的 GitHub Action 包装层，不是网页客户端。
它只负责一件事：**按精确版本下载已发布的分析二进制、校验 SHA-256 后启动分析**。
分析逻辑全部在 `server/` 的二进制内；本目录不包含、也不导入任何 `server/internal` 代码。

## 当前状态：PARTIAL

- 已实现并通过本地集成测试：版本精确匹配下载、SHA-256 校验、归档成员路径校验、
  安装后 `--version` 冒烟身份校验（`install.sh`、`action.yml`）。
- 离线分析模式可用：调用时显式提供 `diff-path` 输入即可对给定 diff 出报告。
- **Action 模式（自动获取 PR 文件与 patch）尚未接线**，属后续切片
  （见 `spec/implementation-status.md`）；未提供 `diff-path` 时本 Action 会
  明确报错退出，不会静默跳过分析。
- 真实 GitHub Releases 上尚无发布产物；在正式发布可用前，本目录保持
  `PARTIAL`，不得声称 Action 已可安装使用。

## 用法（发布可用后）

```yaml
- name: Change risk analysis
  uses: <owner>/change-risk-analyzer/client@v0.1.0  # 必须固定精确版本标签
  with:
    diff-path: ${{ runner.temp }}/change.patch
```

安全约定（与 `spec/08-security-privacy.md` 一致）：

- `analyzer-version` 默认取调用引用；`latest`、`v1` 等浮动引用会被安装器
  直接拒绝——请固定完整版本标签或显式传入精确版本。
- 发布包与校验清单来自同一发布版本，逐字符比对 SHA-256，不匹配即拒绝安装。
- 安装后执行 `--version` 冒烟校验，保证「安装的即所请求的」。
- 归档成员路径逐个校验，拒绝绝对路径与 `..` 穿越。
- 本 Action 不执行被分析仓库（含 PR 分支）中的任何脚本，不读取任何 Secret。

## 输入与输出

输入：`analyzer-version`、`release-base`、`diff-path`、`output-dir`；
输出：`analyzer-version`、`binary-path`、`report-path`。语义见 `action.yml` 注释。

## 本地测试

```bash
bash client/test/install_test.sh
```

测试使用 python3 `http.server` 起本地服务器、`go build` 现场构建注入版本号
的 fixture 二进制，全部请求只打向 127.0.0.1，不访问任何真实发布源。
