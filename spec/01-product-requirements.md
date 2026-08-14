# 01. Product Requirements

## 1. 用户和问题

### 1.1 个人开发者

个人开发者通常能看到代码差异，但不容易快速判断：

- 这次改动影响了哪些边界。
- 是否需要数据库迁移、配置变更或回滚方案。
- 是否漏了关键测试。
- 一个看似简单的依赖或权限改动是否扩大了风险。

### 1.2 小型团队

小团队希望在人工审查前得到一份简短、可验证的风险摘要，而不是几十条泛化的 AI 评论。

## 2. 用户故事

- 作为 PR 作者，我希望看到本次变更影响的模块和风险维度。
- 作为 Reviewer，我希望每个高风险判断都指向具体文件、行或 patch 片段。
- 作为维护者，我希望模型故障时仍能获得确定性事实报告。
- 作为仓库管理员，我希望 Action 使用最小权限，不执行不可信 PR 代码。
- 作为项目负责人，我希望通过固定 fixture 衡量误报、漏报和行号准确率。
- 作为重复运行的 Action，我希望同一提交只保留一条机器人评论。

## 3. MVP 工作流

```text
Pull Request opened/synchronize/reopened
    → 读取事件和 PR 元数据
    → 获取文件列表及 patch
    → 规范化变更
    → 提取确定性信号
    → 可选调用 AI 解释上下文
    → 证据校验和策略评分
    → 生成 JSON、Markdown 和 Step Summary
    → 更新一条幂等机器人评论
```

## 4. MVP 输入

- GitHub Actions 事件 JSON。
- Pull Request 编号、仓库、base SHA、head SHA。
- Pull Request 标题和正文，视为不可信文本。
- changed files API 的文件状态、路径、增删行数和 patch。
- 可选的受限仓库规则文件：`agents.md`、`AGENTS.md`、`.github/copilot-instructions.md` 等。规则文件只作为上下文，不提供执行指令。
- 可选模型 Provider 的 API Key，不写入报告和日志。

## 5. MVP 输出

### 5.1 JSON Artifact

包含 schema 版本、请求身份、变更统计、风险维度、发现、测试缺口、降级原因、运行信息和可选成本信息。格式由 `schemas/risk-report.schema.json` 固定。

### 5.2 Markdown 摘要

推荐顺序：

1. 状态和风险级别。
2. 一句话结论。
3. 变更概览。
4. 风险维度摘要。
5. 需要优先确认的发现。
6. 建议补充的测试。
7. 分析范围和降级原因。
8. Artifact 链接和 schema 版本。

### 5.3 Pull Request 评论

MVP 只发布一条总评论，使用固定 marker：

```text
<!-- change-risk-analyzer:v1 repo=<repo> pr=<number> -->
```

正文中记录 `head_sha`。同一 marker 的评论必须更新，而不是新建。行级评论属于后续阶段。

## 6. 状态模型

- `completed`：确定性分析和可选模型分析均正常完成。
- `degraded`：模型、部分 API 或上下文能力失败，但确定性结果可用。
- `skipped`：由于无权限、无模型 Secret 或策略限制只执行了有限范围。
- `failed`：无法读取必要的 Pull Request 身份或生成合法报告。

`failed` 只用于无法安全建立报告的情况。单个模型请求失败不能覆盖确定性结果，应优先生成 `degraded`。

## 7. 非功能要求

- 可重复：同一输入和相同规则版本产生稳定的确定性部分。
- 可审计：每个发现包含来源、规则、证据和 schema 版本。
- 有界：限制文件数、patch 字节数、上下文 token 数、发现数量和评论长度。
- 安全：不执行 PR 代码，最小化 Token 权限，默认不保存原始代码。
- 可替换：GitHub API、模型 Provider 和发布方式通过接口隔离。
- 可测试：不依赖真实 GitHub 和真实模型即可运行核心测试。

## 8. 验收标准

- 用低风险文档变更 fixture 生成 `low` 或 `medium` 报告，不制造高危结论。
- 用认证和权限扩大 fixture 生成至少一个带证据的高风险候选。
- 用恶意 Prompt 注入 fixture，报告只把注入文本当作代码内容，不执行其指令。
- 用无模型配置运行时，生成 `degraded` 或 `skipped` 报告，并保留确定性信号。
- 同一 `head_sha` 连续发布两次，评论数量仍为一条。
- API 返回 429、404、空 patch 或超大 patch 时，状态和降级原因可解释。