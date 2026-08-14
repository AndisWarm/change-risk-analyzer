# ADR-002：首版禁止执行不可信 Pull Request 代码

- 状态：Accepted
- 日期：2026-08-14
- 范围：安全边界

## 背景

代码审查机器人处理的是外部提交内容。Pull Request 可以修改脚本、Workflow、依赖、Makefile 和测试。如果在分析阶段执行这些内容，Runner 上的 Token、模型 Key、环境变量和网络权限都可能被窃取。

## 决策

首版只分析 GitHub API 返回的元数据、文件列表和 patch：

- 不运行 `go test`、`go vet`、构建、安装、生成器或仓库脚本。
- 不使用 `pull_request_target` checkout 外部代码。
- 不把 PR 内容拼进 Shell。
- 确定性分析器只处理文本和 diff。
- 外部静态工具结果未来只能作为已验证的 Artifact 输入。

## 原因

这是比 Prompt 约束更可靠的边界。模型无法保证不可信代码不会被误执行，Action Workflow 也不能仅靠“作者可信”判断。

## 代价

- 首版不能自动运行项目测试来补充证据。
- 不能完整解析依赖图或编译语义。
- 一部分结论只能标记为候选或建议。

## 后续条件

如果需要执行静态工具，必须使用隔离、最小权限、无 Secret 的环境，并单独评估 Fork、Artifact 和 Runner 风险。改变本决策必须新增安全决策记录和攻击测试。