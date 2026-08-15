# Change Risk Analyzer 开发总控计划

## 1. 文档用途

本文件是后续开发会话的执行清单和实施日志。它帮助每个会话只完成一个可验证的功能，不把设计、文件存在或页面展示误写成已经完成的产品能力。

本文件不是产品协议的事实源。发生冲突时，依次以以下文件为准：

1. `spec/schemas/risk-report.schema.json`
2. `spec/08-security-privacy.md`
3. `spec/05-github-action-contract.md`
4. 其他 `spec/` 文档
5. `agents.md`
6. 本文件

## 2. 产品边界

产品是一个安装在 GitHub 仓库中的 Pull Request 风险分析工具，不是网页后台，也不是常驻服务。

- `server/`：Go 分析内核、离线命令行、GitHub 适配器、策略、报告和 AI 适配器。
- `client/`：GitHub Action 安装包装层。它下载经过校验的已发布分析程序，不提供网页界面。
- `spec/`：产品协议、设计决策、schema、fixture 和评测标准。

目标目录将在“工程目录迁移”切片中建立，目标形态如下：

```text
client/                         GitHub Action 包装层
  action.yml
  package.json
  src/
  test/
server/                         Go 分析内核
  go.mod
  cmd/go-risk-analyzer/
  internal/
spec/                           设计与协议事实源
go.work                         根目录 Go 工作区
DEVELOPMENT_PLAN.md             本文件
```

`client/` 只会在分析二进制已经具备版本化发布能力后变成可安装功能；在此前必须标记为 `PARTIAL`，不能说 Action 已可用。

## 3. 状态规则

状态只描述已核实的实际结果。

| 状态 | 含义 |
| --- | --- |
| `PLANNED` | 代码尚未写入当前项目。 |
| `IN_PROGRESS` | 代码已写入当前项目，且至少完成一次构建或测试；当前功能尚未完成全部验收。 |
| `PARTIAL` | 只完成一部分、验证不完整，或等待外部发布条件。 |
| `DONE` | 代码存在、该功能验收测试通过、文档与实施日志已回写。 |
| `BLOCKED` | 已连续三轮被同一外部条件阻塞，无法继续。 |

下列情况不能单独证明功能完成：

- 方案已经设计。
- 函数已经存在。
- 指标已经展示但没有进入实际调度或策略。
- 已采集流量但没有使用这些数据。
- 页面或文档写了某项能力。

除非已经完成对应的压测、并发测试和故障切换测试，任何文档、日志和汇报都不得声称“高并发稳定”。

## 4. 当前已核实状态

以下结论仅来自已检查的工作区文件和已执行的命令。

- 当前阶段为 Phase 1（离线确定性内核），检查点为 C3（确定性风险规则）。
- `server/` 已成为唯一 Go module，包含既有的 `internal/` 包；根目录 `go.work` 已引用该模块。
- `client/` 已建立，但目前只有边界说明；尚不存在 `action.yml`、已发布二进制或 CLI 入口。
- `server/internal/event` 已能把完整 Pull Request 事件 JSON 转为合法 `ReviewRequest`，不访问网络或文件系统。
- `CR-REL-001` 已能识别有限范围内新增的 Go HTTP 请求缺少可见超时或取消边界的线索；命名 client 配置仍不推断。
- `CR-CON-001` 已能识别新增 goroutine 缺少可见生命周期线索的候选；它不证明 goroutine 泄漏。
- 已检查到领域对象、报告 schema 校验、unified diff 解析，以及 `CR-SEC-001`、`CR-API-001`、`CR-EXEC-001`、`CR-DATA-001` 的规则代码与测试。
- 已执行 `go test ./...` 和 `go vet ./...`，均通过；本次检查的工作区中没有 `git diff --check` 错误。
- 工作区已有未提交的规则和文档改动。后续切片必须保留这些改动，不能回退或覆盖。
- 尚未核实真实 GitHub API、真实模型、PR 评论、Artifact、Step Summary、二进制发布或 GitHub Action 的运行行为。

创建本文件只完成“开发计划文档”这一文档交付，不改变任何 C1-C9 软件功能的状态。

## 5. 每轮开发协议

每个实际开发会话必须遵守以下顺序：

1. 读取 `agents.md`、`spec/README.md`、`spec/00-overview.md`、`spec/03-architecture.md`、`spec/04-domain-model.md`、`spec/05-github-action-contract.md`、`spec/10-roadmap.md`、`spec/implementation-status.md` 和当前功能直接相关的规范。
2. 说明当前阶段、已完成能力、本轮唯一功能、待改文件、验收标准、测试、明确不做的内容与可能的设计冲突。
3. 只实现一个可独立测试的功能切片；不要顺带实现下一个功能。
4. 执行与该切片相匹配的验证，记录实际命令与结果。
5. 回写 `spec/implementation-status.md`；完成检查点时再更新 `spec/10-roadmap.md`；改变协议、模块边界、权限或部署方式时先补充 ADR。
6. 在本文件底部追加实施日志，写明真实改动、验证、限制和下一功能。
7. 不自动开始下一切片，除非用户明确要求继续。

每个 Go 功能默认验证：

```text
在 server/ 中执行 go test ./...
在 server/ 中执行 go test -race ./...
在 server/ 中执行 go vet ./...
gofmt 检查
```

报告输出变化还必须运行 schema 和 golden 测试；GitHub 与模型功能必须分别使用 Fake GitHub Server 和 Fake Provider，不能把真实 Token、真实 GitHub 写操作或真实模型调用放入单元测试。

## 6. 实施顺序

| 顺序 | 唯一功能切片 | 状态 | 完成条件 |
| --- | --- | --- | --- |
| 1 | 创建本开发总控文档 | `DONE` | 本文件存在，当前状态与后续顺序可核对。 |
| 2 | 工程目录迁移 | `DONE` | 已建立 `server/`、`client/`、`go.work`；既有测试通过，模块边界 ADR 已补充。 |
| 3 | 离线事件解析 | `DONE` | 事件 JSON 可生成 `ReviewRequest`，并覆盖空值、非法 SHA、Fork 和未知事件。 |
| 4 | `CR-REL-001` 外部请求超时线索 | `DONE` | 已提供正例、反例、边界例、误报说明和稳定 Evidence。 |
| 5 | `CR-CON-001` goroutine 生命周期线索 | `DONE` | 已提供正例、反例、边界例、误报说明和稳定 Evidence。 |
| 6 | `CR-SC-001` 浮动依赖引用 | `PLANNED` | 覆盖 Action、依赖或镜像的浮动版本线索。 |
| 7 | `CR-SEC-002` Secret-like literal | `PLANNED` | 只报告新增的可复核线索，避免输出原始 Secret。 |
| 8 | `CR-SEC-003` 授权边界变化 | `PLANNED` | 只产出可复核候选，不把词法匹配当作漏洞结论。 |
| 9 | `CR-TEST-001` 风险变更测试证据 | `PLANNED` | 使用谨慎措辞，不断言“没有测试”。 |
| 10 | 信号运行器与 fixture 接入 | `PLANNED` | 所有已实现规则可统一运行、去重、稳定排序并进入固定 fixture。 |
| 11 | C4 策略引擎 | `PLANNED` | Signal 转 Finding、证据校验、缓解项、维度分数和总分均由确定性策略产生。 |
| 12 | C4 报告构建与渲染 | `PLANNED` | 生成通过 `risk-report/v1` schema 的 JSON 和稳定 Markdown/golden 报告。 |
| 13 | 离线 CLI | `PLANNED` | `go-risk-analyzer analyze --event --diff --output` 可完成无网络分析。 |
| 14 | C5 Fake GitHub Client | `PLANNED` | 覆盖分页、429、超时、权限错误和 head SHA 校验。 |
| 15 | C6 GitHub 只读 REST 适配器 | `PLANNED` | 读取 PR、文件和 patch；不执行 PR 代码。 |
| 16 | 版本化二进制发布构建 | `PLANNED` | 构建 Linux amd64 包与 SHA-256 校验清单。 |
| 17 | GitHub Action 客户端 | `PLANNED` | 下载与 Action 版本匹配的二进制并校验哈希；真实发布前保持 `PARTIAL`。 |
| 18 | C7 Artifact 与 Step Summary | `PLANNED` | 生成 JSON、Markdown、运行元数据与 Action 输出。 |
| 19 | C8 幂等评论发布 | `PLANNED` | marker 查找、创建、更新和旧 head SHA 保护均由 Fake Server 验证。 |
| 20 | 上下文裁剪与脱敏 | `PLANNED` | 不可信文本、长度预算、脱敏和截断原因可验证。 |
| 21 | C9 Fake Provider 与模型输出校验 | `PLANNED` | 覆盖合法/非法 JSON、行号、路径、超时、429、一次修复和降级。 |
| 22 | 可配置真实兼容 Provider | `PLANNED` | 只在手工集成验证使用真实 Key；模型不决定分数或门禁。 |
| 23 | Phase 4 调优与可选门禁 | `PLANNED` | 达到固定评测阈值后才可启用 `fail-on`；默认仍不阻塞合并。 |
| 24 | Fork 双工作流、GitHub App、数据库或常驻服务 | `PLANNED` | 先有独立 ADR、威胁模型与集成测试；没有这些前不得开始。 |

## 7. 稳定接口

- CLI：`go-risk-analyzer analyze`，同时支持离线输入与 GitHub Action 运行。
- 报告：保持 `risk-report/v1`；字段变更必须同步 schema、fixture、测试和 ADR。
- Action 输入：`config`、模型配置、文件/patch 上限、评论与 Artifact 开关、`fail-on`、`debug`。
- Action 发布：仅下载精确版本的二进制并校验 SHA-256，不使用浮动的“latest”二进制。

## 8. 实施日志

### 2026-08-15 - 创建开发总控文档

- 状态：`DONE`（文档交付，不代表任何软件检查点完成）。
- 修改：新增 `DEVELOPMENT_PLAN.md`。
- 核实：读取工作区、现有规格和实现状态；已确认当前 Go 测试与 `go vet` 通过。
- 未做：未迁移目录、未创建 Action、未新增或修改任何风险规则、未改变 C1-C9 状态。
- 下一功能：工程目录迁移。

### 2026-08-15 - 工程目录迁移

- 状态：`DONE`。
- 修改：将 `go.mod`、`go.sum` 和全部 `internal/` 原样迁移到 `server/`；新增 `client/README.md`、根 `go.work` 和 ADR-004；更新架构说明、README 与忽略规则。
- 验证：根目录 `go test ./server/...` 与 `go vet ./server/...` 通过；`server/` 内 `go test ./...`、`go test -race ./...`、`go vet ./...` 和 `gofmt` 检查通过；`git diff --check` 通过。
- 限制：`client/` 尚无 `action.yml`，没有可发布二进制；`server/` 尚无 CLI，因此 GitHub Action 仍不可运行。
- 下一功能：离线事件解析。

### 2026-08-15 - 离线事件解析

- 状态：`DONE`。
- 修改：新增 `server/internal/event`，提供纯函数 `ParsePullRequestEvent([]byte)` 和结构化 `ParseError`。
- 行为：将完整 Pull Request 事件映射为领域 `ReviewRequest`；正确区分同仓、Fork、未知来源和未知事件动作；所有字段经领域校验。
- 验证：事件包定向测试、根目录 `go test ./server/...`、`server/` 内 `go test -race ./...`、`go vet` 与 `gofmt` 检查通过。
- 限制：真实 `workflow_dispatch` 必须由后续 GitHub 适配器按 PR 编号重新读取并验证 base/head SHA，因此当前解析器明确拒绝该事件。
- 下一功能：`CR-REL-001` 外部请求超时线索。

### 2026-08-15 - `CR-REL-001` 外部请求超时线索

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/external_request_timeout.go` 与单元测试；更新确定性规则文档。
- 行为：针对新增 Go patch 行识别默认 HTTP 辅助方法、无可见 context 的 `http.DefaultClient.Do`，以及无 `Timeout` 的内联 `http.Client.Do`；每个线索带稳定的右侧行号 Evidence。
- 验证：定向规则测试、根目录 `go test ./server/...`、`server/` 内 `go test -race ./...`、`go vet` 与 `gofmt` 检查通过。
- 限制：规则不推断命名 client、跨行 request 或 context 的真实配置，也不决定最终严重度。
- 下一功能：`CR-CON-001` goroutine 生命周期线索。

### 2026-08-15 - `CR-CON-001` goroutine 生命周期线索

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/goroutine_lifecycle.go` 与单元测试；更新确定性规则文档。
- 行为：识别新增命名或匿名 goroutine 中缺少可见 `ctx`、停止通道、`cancel` 或 WaitGroup 生命周期线索的候选，并生成稳定右侧行号 Evidence。
- 验证：定向规则测试、根目录 `go test ./server/...`、`server/` 内 `go test -race ./...`、`go vet` 与 `gofmt` 检查通过。
- 限制：规则不证明泄漏，不跟踪跨函数或跨文件的取消、channel 或 WaitGroup 配对。
- 下一功能：`CR-SC-001` 浮动依赖引用。
