# 00. Overview

## 1. 项目定义

Change Risk Analyzer 是一个面向 GitHub Pull Request 的代码变更风险分析器。它不回答“代码风格是否漂亮”，而回答：

> 这次变更可能影响什么，风险证据在哪里，合并前应该优先验证什么？

首版是一个 GitHub Action。它读取 Pull Request 的元数据、文件列表和 patch，先生成确定性的变更事实，再使用可选的 AI 对上下文进行解释，最终生成结构化报告。

## 2. 术语

- **ChangeSet**：经过统一规范化的 Pull Request 文件变化集合。
- **Signal**：从变更中提取的事实或风险线索，不等于最终结论。
- **Finding**：经过证据校验并可供开发者复核的风险发现。
- **Evidence**：定位到文件、行或变更片段的支持信息。
- **Deterministic analyzer**：不依赖模型、相同输入产生相同结果的分析器。
- **Model reviewer**：使用 LLM 解释上下文和提出候选发现的组件。
- **Policy engine**：合并确定性信号和模型候选结果，计算风险和发布策略的组件。
- **Degraded report**：部分能力失败但仍保留确定性结果的报告。

## 3. 目标

### 3.1 MVP 目标

- 在 Pull Request 创建、重新打开或新增提交时运行。
- 正确获取 base/head SHA、文件状态、增删行数和 patch。
- 识别变更影响的主要风险维度。
- 输出可验证的 JSON 和人类可读的 Markdown。
- 在允许写评论时更新同一条机器人评论。
- 在模型不可用、无 Secret、API 超时或 patch 超限时给出可解释的降级结果。
- 不执行 Pull Request 分支代码。

### 3.2 后续目标

- 行级评论。
- 公开 Fork 的安全双工作流。
- 可配置的风险门禁。
- GitLab、Bitbucket 等平台适配器。
- 在真实评测数据支持后，再评估 GitHub App 和历史风险分析。

## 4. 非目标

- 训练或托管大模型。
- 替代编译器、测试、静态检查器、CodeQL 或人工审批。
- 自动修改代码、自动创建提交或自动合并。
- 执行来自 Pull Request 的脚本、测试或构建。
- 首版引入常驻服务、数据库、消息队列或跨仓库知识库。
- 只凭模型直觉生成无文件、无行号、无事实依据的高危结论。

## 5. 成功标准

一个可用版本必须满足：

- 输入同一 `head_sha` 可重复生成等价报告。
- 输出始终满足 JSON Schema。
- 所有高/严重级别发现都有可复核证据。
- 同一提交重复运行不会重复创建评论。
- 模型失败不会使确定性报告丢失。
- 公开 Fork、无 patch、二进制、超大 diff 和权限不足都有明确状态。

## 6. 文档和实现关系

- 产品行为见 `01-product-requirements.md`。
- 风险判断见 `02-risk-model.md`。
- 模块关系见 `03-architecture.md`。
- 对象和 schema 见 `04-domain-model.md` 与 `schemas/risk-report.schema.json`。
- GitHub 工作流和权限见 `05-github-action-contract.md`。
- 模型边界见 `06-ai-review-contract.md`。
- 确定性规则见 `07-deterministic-analyzers.md`。
- 安全与隐私见 `08-security-privacy.md`。
- 评测与发布门槛见 `09-evaluation.md`。
- 实施阶段见 `10-roadmap.md`。