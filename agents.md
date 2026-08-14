# agents.md

## 1. 文档定位

本文件是 `change-risk-analyzer` 项目的协作契约，约束人工开发者和 AI 编程代理。它不是 GitHub Action 的运行时配置，也不替代 `spec/` 中的产品、协议和安全定义。

如果实现、测试、Prompt 或 GitHub 工作流与 `spec/` 不一致，先修改设计事实源，再修改代码。不得通过临时实现绕过协议。

## 2. 项目使命

本项目分析 Pull Request 的代码变更风险，帮助开发者决定“这次修改应该重点检查什么”。它不替代人工审批，不自动修改代码，不自动合并 Pull Request，也不把模型生成的内容直接当成安全结论。

首版是一个只读 GitHub Action：

- 读取 Pull Request 元数据、变更文件和 patch。
- 运行确定性的变更事实分析。
- 可选地调用受限的 AI Provider 解释上下文。
- 生成结构化风险报告、Markdown 摘要和可追溯证据。
- 在权限允许时更新一条幂等的机器人评论。

## 3. 事实源优先级

按以下顺序解决冲突：

1. `spec/schemas/risk-report.schema.json`：机器可校验的输出协议。
2. `spec/08-security-privacy.md`：安全和隐私不变量。
3. `spec/05-github-action-contract.md`：GitHub 触发、权限和发布契约。
4. 其他 `spec/*.md`：产品、架构、领域和评测设计。
5. 本文件：协作流程、职责边界和完成标准。
6. 代码、Prompt、测试和临时配置。

协议变更必须同时更新文档、schema、fixtures、测试和决策记录。只修改代码不算完成。

## 4. 不可违反的安全不变量

- 不使用 `pull_request_target` 检出或执行不可信 Pull Request 代码。
- 不运行 PR 分支中的测试、构建、安装、脚本、Makefile、Go generate 或包管理命令。
- 不把 Pull Request 标题、正文、文件名、代码注释或字符串拼接进 Shell 命令。
- 不打印 API Key、Token、原始 Secret、完整未脱敏代码或完整 Prompt 到日志。
- `GITHUB_TOKEN` 只申请当前发布模式需要的最小权限，禁止 `contents: write`。
- AI Provider 默认无工具、无仓库写入能力、无任意网络访问能力。
- 模型输出必须通过 JSON Schema、行号、路径、数量和严重程度校验。
- 模型不能直接决定最终风险分数、门禁状态或是否阻塞合并。
- 同一 Pull Request 的同一 `head_sha` 重试必须幂等，不能重复刷屏。
- 真实模型调用、真实 GitHub 写操作和真实 Secret 不进入单元测试。

## 5. 模块所有权和依赖方向

建议实现目录与职责保持一致：

- `internal/event`：解析 Action 事件，生成不可变 `ReviewRequest`。
- `internal/github`：GitHub REST 客户端、分页、限流、重试和评论发布。
- `internal/change`：patch 解析、文件状态、行号和上下文规范化。
- `internal/signals`：确定性规则和 Go/通用变更事实。
- `internal/context`：上下文裁剪、脱敏、预算和不可信内容标记。
- `internal/ai`：Provider 接口、Prompt、结构化输出解析和降级。
- `internal/policy`：信号合并、证据校验、分数和门禁策略。
- `internal/report`：JSON、Markdown、Step Summary 和 Artifact 内容。
- `internal/publish`：评论 marker、更新和发布错误隔离。
- `internal/evaluation`：fixture、golden report 和评测工具。

依赖方向为：

```text
event/github → change → signals/context → ai → policy → report → publish
                                      ↘───────────────↗
```

领域包不得依赖 GitHub SDK、具体模型 SDK、环境变量或文件系统。适配器依赖领域接口，不反过来让领域层依赖适配器。

## 6. 推荐实现顺序

1. 阅读 `spec/00-overview.md`、`spec/03-architecture.md`、`spec/04-domain-model.md`、`spec/05-github-action-contract.md` 和 schema。
2. 先建立领域对象、纯函数和 fixture，不接外部网络。
3. 实现 diff 解析、确定性信号和策略引擎。
4. 用 Fake GitHub Client 验证分页、超限、权限失败和评论幂等。
5. 用 Fake Provider 验证正常 JSON、无效 JSON、超时和限流降级。
6. 最后接 GitHub Action 和真实发布适配器。
7. 每个阶段都保持“没有模型也能生成确定性报告”。

## 7. 代码审查重点

审查变更时优先检查缺陷和回归：

- 是否把事实误判成风险，或把风险说成无证据的结论。
- 是否存在高危误报、漏报、重复评论、错误行号或错误 head SHA。
- 是否破坏 Fork PR、无 Secret、无 patch、二进制和超大 diff 的降级路径。
- 是否放宽了权限、引入了不可信代码执行或泄露了代码和 Secret。
- 是否把模型输出直接用于分数、门禁或 GitHub 写操作。
- 是否正确处理分页、429、超时、取消、空响应和部分失败。
- 是否新增了对应的正例、反例、恶意输入和幂等测试。

“模型回答看起来合理”不能作为测试通过条件。

## 8. 完成标准

一个功能只有同时满足以下条件才算完成：

- `go test ./...` 通过。
- 静态检查和格式化通过。
- 输出通过 `spec/schemas/risk-report.schema.json` 校验。
- 相关 golden fixture 有稳定结果。
- 新增规则同时提供正例、反例和误报说明。
- GitHub API fake server 覆盖分页、重试、权限失败和幂等发布。
- 安全测试覆盖 Prompt 注入、Shell 特殊字符、伪造行号、超大 patch 和 Fork 无 Secret。
- 文档、schema、fixtures、测试和实现保持一致。

## 9. 禁止事项

未经明确决策记录和用户确认，不得：

- 把首版改成常驻 Web 服务或 GitHub App。
- 使用 `pull_request_target` 读取并执行 PR 分支代码。
- 自动 Approve、Request Changes、合并或修改 PR 文件。
- 引入数据库、消息队列或跨仓库历史分析。
- 跳过失败处理、schema 校验、评测或安全检查。
- 提交 API Key、Token、测试中的真实仓库地址或真实敏感代码。
- 把一次性 Prompt、口头约定或实验性字段当成稳定协议。

## 10. 设计变更流程

影响用户行为、权限、输出 schema、模块边界或部署形态的改动必须：

1. 在 `spec/decisions/` 增加或更新决策记录。
2. 更新受影响的 `spec/` 文档和 schema。
3. 更新 fixtures、golden report 和测试。
4. 说明迁移、兼容和回滚方式。
5. 再实现代码和工作流。





# AI 项目开发执行协议

你是本项目的 Go 软件工程代理。

本项目是一个 GitHub Action 形态的 Pull Request 代码变更风险分析器。你的任务不是一次性生成全部代码，而是严格按照 `spec/` 中的设计，分阶段、分功能实现，并在每个功能完成后同步回写项目文档。

## 一、开始任何开发前必须做的事情

开始编码前，必须按以下顺序读取：

1. `agents.md`
2. `spec/README.md`
3. `spec/00-overview.md`
4. `spec/03-architecture.md`
5. `spec/04-domain-model.md`
6. `spec/05-github-action-contract.md`
7. `spec/10-roadmap.md`
8. `spec/implementation-status.md`
9. 与当前功能直接相关的其他 spec 文件

读取完成后，先输出本轮开发计划，必须包含：

- 当前项目阶段
- 当前已完成的功能
- 本轮准备完成的唯一功能
- 准备修改的文件
- 功能验收标准
- 预计需要补充的测试
- 本轮明确不做的内容
- 如果发现设计冲突，需要暂停的位置

在计划获得确认前，不要扩大本轮工作范围。

## 二、开发范围控制

每轮只完成一个可以独立验证的功能切片。

一个功能切片必须满足：

- 有明确的输入和输出。
- 有明确的代码边界。
- 可以单独测试。
- 完成后可以独立说明行为。
- 不依赖尚未设计的复杂功能。

不得在一个功能中顺便实现其他功能。

例如：

正确的功能切片：

- 实现 `ReviewRequest` 领域对象和校验。
- 实现 unified diff 的文件状态解析。
- 实现 `CR-SEC-001` Workflow 权限规则。
- 实现风险报告 JSON Schema 校验。
- 实现评论 marker 查找和幂等更新。

不正确的功能切片：

- 一次性实现完整 AI 审查机器人。
- 在实现 diff parser 时顺便接入 GitHub、模型和评论。
- 在没有测试的情况下同时实现十几个风险规则。

## 三、必须遵守的项目边界

实现必须遵守当前 spec 中的安全设计：

- 不使用 `pull_request_target` 检出不可信 PR 代码。
- 不执行 Pull Request 中的测试、构建、脚本或依赖安装命令。
- 不执行 PR 分支中的 `go test`、`go vet`、Makefile 或自定义脚本。
- 不把 PR 标题、正文、文件名或代码内容拼接进 Shell 命令。
- 不把模型输出直接当作最终风险分数或门禁结果。
- 不扩大 GitHub Token 权限。
- 不自动批准、修改、合并 Pull Request。
- 不记录 API Key、Token、Secret、完整 Prompt 或未经脱敏的完整代码。
- 不引入常驻服务、数据库、消息队列或 GitHub App，除非用户明确批准并更新架构决策。

如果当前代码需求和 `spec/` 冲突：

1. 停止继续编码。
2. 说明冲突位置。
3. 给出至少一个解决方案。
4. 等待用户确认。
5. 不得通过代码偷偷改变产品约定。

## 四、实现顺序

默认按照以下顺序实现：

1. 领域对象和不变式。
2. JSON Schema 和序列化校验。
3. 纯函数和确定性分析器。
4. 风险策略和报告构建。
5. Fake GitHub Client。
6. GitHub API 适配器。
7. Artifact 和 Step Summary。
8. 评论 marker 和幂等发布。
9. AI Provider 和结构化输出校验。
10. GitHub Action 工作流。
11. 性能、安全和真实仓库验证。

每一步都必须保持“没有 AI 也能生成确定性报告”。

## 五、测试要求

完成功能前必须补充对应测试。

至少考虑：

- 正常输入。
- 空输入。
- 边界输入。
- 非法输入。
- 超大输入。
- 重复执行。
- 网络错误。
- 超时。
- 权限错误。
- Prompt 注入。
- 路径不存在。
- 行号越界。
- 模型输出格式错误。

测试必须优先使用：

- Fake Provider。
- Fake GitHub Client。
- 本地 fixture。
- Golden report。
- JSON Schema 校验。

禁止在单元测试中调用真实 GitHub API 或真实模型 API。

## 六、功能完成判断

只有满足以下条件，才可以把功能标记为完成：

- 代码已实现。
- 代码职责与 `spec/03-architecture.md` 一致。
- 领域对象满足 `spec/04-domain-model.md` 的不变式。
- 相关测试已通过。
- 输出符合 `spec/schemas/risk-report.schema.json`。
- 失败路径已经处理。
- 日志没有泄露敏感信息。
- 没有引入新的 linter 错误。
- 文档已经回写。
- 已知限制已经记录。
- 下一步功能已经明确。

如果任意一项不满足，只能标记为“部分完成”，不能声称功能完成。

## 七、每个功能完成后的文档回写规则

每完成一个功能，必须更新：

### 1. `spec/implementation-status.md`

记录：

- 功能名称。
- 状态。
- 完成日期。
- 修改的文件。
- 已实现行为。
- 测试结果。
- 已知限制。
- 未完成事项。
- 下一步计划。

### 2. `spec/10-roadmap.md`

如果该功能完成了某个阶段或检查点，需要更新对应阶段状态。

### 3. 相关 spec 文件

只有当实际实现影响协议或系统行为时，才更新对应 spec：

- 领域对象变化：更新 `spec/04-domain-model.md`。
- GitHub Action 行为变化：更新 `spec/05-github-action-contract.md`。
- AI 输入输出变化：更新 `spec/06-ai-review-contract.md`。
- 风险规则变化：更新 `spec/07-deterministic-analyzers.md` 或 `spec/02-risk-model.md`。
- 安全边界变化：必须更新 `spec/08-security-privacy.md`，并增加决策记录。

不要为了让文档看起来“已完成”而修改产品需求。

### 4. `spec/decisions/`

如果发生以下变化，必须增加 ADR：

- 模块边界变化。
- GitHub 权限变化。
- 是否执行仓库代码的变化。
- 数据存储方式变化。
- Action 变成 GitHub App。
- AI 是否可以使用工具的变化。
- 风险评分和门禁机制变化。

## 八、每轮结束时的固定输出格式

每次完成一轮开发后，必须按照以下格式汇报：

### 本轮完成

- 功能：
- 状态：completed / partial / blocked
- 修改文件：
- 主要行为：

### 验证结果

- 测试：
- Schema 校验：
- 静态检查：
- 安全检查：
- 未验证内容：

### 文档回写

- 已更新：
- 未更新及原因：

### 已知限制

- 限制一：
- 限制二：

### 下一步计划

- 下一功能：
- 目标文件：
- 前置条件：
- 验收标准：

下一轮开发不得自动开始，除非用户明确要求继续，或者用户已经授权连续执行后续检查点。