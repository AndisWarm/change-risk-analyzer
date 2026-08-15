# Change Risk Analyzer

> 面向 GitHub Pull Request 的代码变更风险分析器。它不回答"代码风格是否漂亮"，而回答：这次变更可能影响什么，风险证据在哪里，合并前应该优先验证什么？

[English](./README.en.md)

Change Risk Analyzer 是一个只读的 GitHub Action。它读取 Pull Request 的元数据、变更文件和 patch，先运行确定性的变更事实分析，再可选地调用受限的 AI Provider 解释上下文，最终生成有证据、可追溯的结构化风险报告。

它不替代人工审批，不自动修改代码，不自动合并 Pull Request，也不把模型生成的内容直接当作安全结论。

## 核心特性

- **确定性分析优先**：不依赖模型即可生成相同输入、相同输出的报告；模型不可用、无 Secret 或超时时，确定性报告仍然完整可用。
- **有证据的发现**：所有高/严重级别风险结论都定位到文件、行号或变更片段，可人工复核。
- **只读安全边界**：不执行 Pull Request 分支中的任何代码、脚本、测试或构建命令，不申请 `contents: write` 权限。
- **可选 AI 解释**：模型只负责解释上下文和提出候选发现，不决定最终风险分数、门禁状态或是否阻塞合并。
- **幂等机器人评论**：同一 Pull Request 的同一 `head_sha` 重复运行不会重复刷屏，只更新同一条带 marker 的评论。
- **显式降级**：分页失败、patch 超限、权限不足、模型失败等场景都有明确状态和降级原因，不静默丢弃关键事实。

## 工作原理

```text
GitHub pull_request event
        │
        ▼
┌────────────────────────────────────────────┐
│  解析事件 → 锁定 repository / PR / base / head SHA │
│  读取 PR 元数据和文件列表（分页、重试、限流）        │
│  规范化文件状态和 patch（二进制、超限、截断）        │
│  运行确定性信号分析器                              │
│  按预算组装上下文（裁剪、脱敏、不可信内容标记）       │
│  [可选] 调用 AI Provider → 校验结构化输出           │
│  合并信号 → 计算分数和门禁 → 构建不可变报告          │
│  输出 JSON Artifact + Markdown 摘要                │
│  [权限允许时] 幂等更新机器人评论                    │
└────────────────────────────────────────────┘
```

详细架构见 [`spec/03-architecture.md`](spec/03-architecture.md)。

## 快速开始

> 以下示例基于 `spec/05-github-action-contract.md` 的契约草案。正式发布前，`uses` 地址必须替换为真实 Action 并固定到完整 commit SHA。

```yaml
name: Change Risk Analysis

on:
  pull_request:
    types: [opened, synchronize, reopened]
  workflow_dispatch:
    inputs:
      pull_request_number:
        required: true
        type: number

permissions:
  contents: read
  pull-requests: write

jobs:
  analyze:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Run change risk analyzer
        uses: example/change-risk-analyzer@<full-commit-sha>
        with:
          model-provider: ${{ vars.RISK_MODEL_PROVIDER }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          MODEL_API_KEY: ${{ secrets.MODEL_API_KEY }}
```

### 输入配置

| 输入 | 默认值 | 说明 |
| ---- | ------ | ---- |
| `config` | `.risk-analyzer.yml` | 配置文件路径 |
| `model-provider` | `disabled` | `disabled` / `openai-compatible` / `anthropic-compatible` |
| `model-name` | Provider 默认 | 模型名 |
| `max-files` | `300` | 最大分析文件数 |
| `max-patch-bytes` | `1048576` | 最大总 patch 字节数（单文件上限 128 KiB） |
| `publish-comment` | `true` | 是否发布总评论 |
| `upload-report` | `true` | 是否上传 JSON/Markdown Artifact |
| `fail-on` | `never` | `never` / `high` / `critical`，默认不阻塞合并 |
| `debug` | `false` | 是否输出非敏感调试元数据 |

Secret 只能通过环境变量传入（`GITHUB_TOKEN`、`MODEL_API_KEY`），不允许作为命令行参数、配置文件内容或报告字段。

### 输出

每次分析产生：

- `risk-report.json`：机器可读完整报告，满足 `spec/schemas/risk-report.schema.json`。
- `risk-report.md`：人类可读报告。
- `run-metadata.json`：版本、耗时、输入计数和降级原因，不含原始代码和 Secret。

权限允许时，还会在 PR 上创建或更新一条带 marker 的幂等总评论。

## 安全设计

- 不使用 `pull_request_target` 检出或执行不可信 Pull Request 代码。
- 不运行 PR 分支中的测试、构建、安装、脚本或包管理命令。
- 不把 PR 标题、正文、文件名、代码注释拼接进 Shell 命令。
- `GITHUB_TOKEN` 只申请最小权限，禁止 `contents: write`。
- 模型输出必须通过 JSON Schema、行号、路径、数量和严重程度校验。
- 不打印 API Key、Token、原始 Secret、完整未脱敏代码或完整 Prompt 到日志。

完整安全不变量见 [`spec/08-security-privacy.md`](spec/08-security-privacy.md)。

## 开发

本项目是一个 Go 项目，设计文档和协作契约是事实源，详见 [`agents.md`](agents.md)。

```text
client/                   GitHub Action packaging layer (not a web UI)
server/                   Go analyzer module and future CLI
spec/                     设计文档、协议 schema、fixture 和决策记录
  README.md               设计索引
  schemas/                机器可读协议（risk-report.schema.json）
  fixtures/               固定评测样例
  decisions/              架构决策记录（ADR）
go.work                   Root Go workspace
```

当前处于 Phase 1（离线确定性内核），实施状态见 [`spec/implementation-status.md`](spec/implementation-status.md)，路线图见 [`spec/10-roadmap.md`](spec/10-roadmap.md)。

## 文档索引

| 文档 | 内容 |
| ---- | ---- |
| [`spec/00-overview.md`](spec/00-overview.md) | 项目定义、目标、非目标、成功标准 |
| [`spec/01-product-requirements.md`](spec/01-product-requirements.md) | 产品需求 |
| [`spec/02-risk-model.md`](spec/02-risk-model.md) | 风险模型 |
| [`spec/03-architecture.md`](spec/03-architecture.md) | 架构与模块边界 |
| [`spec/04-domain-model.md`](spec/04-domain-model.md) | 领域对象与不变式 |
| [`spec/05-github-action-contract.md`](spec/05-github-action-contract.md) | GitHub Action 契约 |
| [`spec/06-ai-review-contract.md`](spec/06-ai-review-contract.md) | AI 审查契约 |
| [`spec/07-deterministic-analyzers.md`](spec/07-deterministic-analyzers.md) | 确定性分析器 |
| [`spec/08-security-privacy.md`](spec/08-security-privacy.md) | 安全与隐私 |
| [`spec/09-evaluation.md`](spec/09-evaluation.md) | 评测与发布门槛 |

## 许可证

[MIT](./LICENSE)
