# Change Risk Analyzer 设计索引

## 目标

本项目是一个面向 Pull Request 的代码变更风险分析器。首版以 GitHub Action 运行，读取 Pull Request 的元数据和 diff，结合确定性分析与可选 AI 分析，输出有证据的风险报告。

## 文档阅读顺序

1. [00-overview.md](00-overview.md)
2. [01-product-requirements.md](01-product-requirements.md)
3. [02-risk-model.md](02-risk-model.md)
4. [03-architecture.md](03-architecture.md)
5. [04-domain-model.md](04-domain-model.md)
6. [05-github-action-contract.md](05-github-action-contract.md)
7. [06-ai-review-contract.md](06-ai-review-contract.md)
8. [07-deterministic-analyzers.md](07-deterministic-analyzers.md)
9. [08-security-privacy.md](08-security-privacy.md)
10. [09-evaluation.md](09-evaluation.md)
11. [10-roadmap.md](10-roadmap.md)

机器可读协议位于 [schemas/risk-report.schema.json](schemas/risk-report.schema.json)。固定评测样例位于 [fixtures/](fixtures/)，重要决策位于 [decisions/](decisions/)。

## 当前状态

- 状态：设计冻结后进入实现准备阶段。
- 运行形态：GitHub Action 优先。
- 分析模式：只读，不执行 Pull Request 代码。
- 发布方式：JSON Artifact + Markdown 摘要 + 幂等机器人评论。
- 默认门禁：不阻塞合并。

## 核心原则

- 事实提取、风险策略和模型解释分层。
- 模型不是最终评分器，也不是安全边界。
- 无模型或模型失败时，确定性报告仍然可用。
- 高严重度结论必须有可复核证据。
- 任何协议变化都必须同步 schema、fixtures 和测试。