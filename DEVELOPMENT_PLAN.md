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

## 2. 给项目所有者的使用说明

本节用日常语言解释这份计划和项目结构，供不参与编码的项目所有者阅读。

**以后怎么继续开发。** 每次想推进项目时，新开一个开发会话，只需说一句“按照 DEVELOPMENT_PLAN.md 做第 N 步”（N 是第 7 节任务表里的行号）。AI 会自行读取必要的文件，只做该步的工作，完成后自动把状态和实施日志回写到本文件及相关记录里。不需要每次重新解释项目背景。

**各个文件夹是做什么的。**

- `spec/` 是设计图纸：记录产品应该长什么样、要遵守哪些规矩。平时不用动它，只有产品设计本身变化时才会修改。
- `server/` 是分析引擎：真正执行风险分析的程序代码都放在这里。
- `client/` 是插件外壳：将来让这个工具能作为 GitHub Action 在仓库里安装运行的包装层。目前还没有实际内容，只有一份边界说明。

**最终交付形态。** 交付形态已锁定为只读的 GitHub Action：不做网页后台，也不做常驻服务。安装到仓库后，它只在 GitHub 的 Pull Request 页面工作，分析结果以机器人评论和报告的形式出现在 Pull Request 里，供人工审阅参考，不会自动修改或合并代码。

## 3. 产品边界

产品是一个安装在 GitHub 仓库中的 Pull Request 风险分析工具，不是网页后台，也不是常驻服务。

- `server/`：Go 分析内核、离线命令行、GitHub 适配器、策略、报告和 AI 适配器。
- `client/`：GitHub Action 安装包装层。它下载经过校验的已发布分析程序，不提供网页界面。
- `spec/`：产品协议、设计决策、schema、fixture 和评测标准。

目标目录已在“工程目录迁移”切片（切片 2，`DONE`）中建立目录骨架；下图为目标形态，其中部分子项（如 `client/action.yml` 与 CLI 入口）仍待后续切片创建，不代表当前已存在：

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

## 4. 状态规则

状态只描述已核实的实际结果。

| 状态 | 含义 |
| --- | --- |
| `PLANNED` | 代码尚未写入当前项目。 |
| `IN_PROGRESS` | 代码已写入当前项目，且至少完成一次构建或测试；当前功能尚未完成全部验收。 |
| `PARTIAL` | 只完成一部分、验证不完整，或等待外部发布条件。 |
| `DONE` | 代码存在、该功能验收测试通过、文档与实施日志已回写。 |
| `BLOCKED` | 已连续三轮被同一外部条件阻塞，无法继续。 |

状态变更的前置条件：

- 只有相应代码已写入工作区，且至少通过一次构建或测试之后，状态才可以从 `PLANNED` 改为 `IN_PROGRESS` 或 `DONE`。
- 只完成一部分的功能必须标 `PARTIAL`，不得标 `DONE`。

下列情况不能单独证明功能完成：

- 方案已经设计（设计完成不等于已实现）。
- 函数已经存在（单个函数存在不等于整条链路已完成）。
- 指标已经展示但没有进入实际调度或策略（展示了指标不等于被实际使用）。
- 已采集流量或数据但没有使用这些数据（采集了数据不等于相应能力已实现）。
- 页面或文档写了某项能力。

除非已经完成对应的压测、并发测试和故障切换测试，任何文档、日志和汇报都不得声称“高并发稳定”。

「当前已核实状态」章节只允许写检查过工作区代码或实际执行过命令后得到的结论，禁止写推测性表述。

每次对工作区的实际改动都必须在第 9 节实施日志追加一条记录。

## 5. 当前已核实状态

核实日期：2026-08-25。以下结论仅来自当日检查过的工作区文件和实际执行过的命令。

阶段与进度：

- 当前阶段为 Phase 1（离线确定性内核），检查点为 C3（确定性风险规则）。
- 下一个待实现功能是切片 6 `CR-SC-001`（浮动依赖引用），状态仍为 `PLANNED`，尚未开始：`server/internal/signals/` 下不存在任何浮动依赖或供应链引用规则的实现文件；全仓检索命中的只有领域层与报告 schema 中既有的 `supply_chain` 风险类别枚举，属于协议定义，不是规则实现。

目录结构现状：

- 根目录包含 `.git`、`.gitignore`、`agents.md`、`DEVELOPMENT_PLAN.md`、`go.work`、`LICENSE`、`README.md` 以及 `client/`、`server/`、`spec/` 三个目录。
- `server/` 是唯一 Go module（模块路径 `change-risk-analyzer`，Go 1.26）；根目录 `go.work` 内容为 `use ./server`。
- `client/` 目前只有 `README.md`，不存在 `action.yml`，也没有任何可安装的 Action。
- `server/internal/` 现有包：`change`、`domain`、`event`、`report`（含内置 schema 副本）、`signals`（含 `CR-SEC-001`、`CR-API-001`、`CR-EXEC-001`、`CR-DATA-001`、`CR-REL-001`、`CR-CON-001` 六条规则及其测试）。

验证命令与结果（均在 2026-08-25 实际执行）：

- 在 `server/` 内执行 `go test ./...`：通过，5 个包全部 ok（change、domain、event、report、signals）。
- 在 `server/` 内执行 `go test -race ./...`：通过，5 个包全部 ok。
- 在 `server/` 内执行 `go vet ./...`：退出码 0。
- 在 `server/` 内执行 `gofmt -l .`：无输出（所有 Go 文件格式正确）。
- 在仓库根目录执行 `go test ./server/...`：通过，5 个包 ok（缓存命中）。
- 在仓库根目录执行 `git status --short`：无输出，工作区干净。2026-08-15 记录的“未提交的规则和文档改动”此后已被提交，该旧表述已作废。
- 在仓库根目录执行 `git log --oneline -5`：正常显示最近 5 条提交；当前分支为 `main`，最新提交为 `0c62598` “feat: add offline analysis foundations”（2026-08-15）。

尚未核实的部分（与此前一致）：真实 GitHub API、真实模型调用、PR 评论发布、Artifact、Step Summary、二进制版本化发布和 GitHub Action 的实际运行行为均未验证。

本轮只完成文档核实与刷新这一文档交付，不改变任何 C1-C9 软件功能的状态。

## 6. 每轮开发协议

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

## 7. 实施顺序

| 顺序 | 唯一功能切片 | 状态 | 完成条件 |
| --- | --- | --- | --- |
| 1 | 创建本开发总控文档 | `DONE` | 本文件存在，当前状态与后续顺序可核对。 |
| 2 | 工程目录迁移 | `DONE` | 已建立 `server/`、`client/`、`go.work`；既有测试通过，模块边界 ADR 已补充。 |
| 3 | 离线事件解析 | `DONE` | 事件 JSON 可生成 `ReviewRequest`，并覆盖空值、非法 SHA、Fork 和未知事件。 |
| 4 | `CR-REL-001` 外部请求超时线索 | `DONE` | 已提供正例、反例、边界例、误报说明和稳定 Evidence。 |
| 5 | `CR-CON-001` goroutine 生命周期线索 | `DONE` | 已提供正例、反例、边界例、误报说明和稳定 Evidence。 |
| 6 | `CR-SC-001` 浮动依赖引用 | `DONE` | 已提供正例、反例、边界例、误报说明和稳定 Evidence。 |
| 7 | `CR-SEC-002` Secret-like literal | `DONE` | 只报告新增的可复核线索，避免输出原始 Secret。 |
| 8 | `CR-SEC-003` 授权边界变化 | `DONE` | 已提供正例、反例、边界例、误报说明和双侧行号 Evidence。 |
| 9 | `CR-TEST-001` 风险变更测试证据 | `DONE` | 已提供正例、反例、边界例、误报说明和观察式措辞断言。 |
| 10 | 信号运行器与 fixture 接入 | `DONE` | 运行器聚合十条规则输出，去重与全局稳定排序有测试与金样固化。 |
| 11 | C4 策略引擎 | `DONE` | Signal 转 Finding、证据校验、缓解项、维度分数和总分均由确定性策略产生。 |
| 12 | C4 报告构建与渲染 | `DONE` | 生成通过 `risk-report/v1` schema 的 JSON 和稳定 Markdown/golden 报告。 |
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

## 8. 稳定接口

- CLI：`go-risk-analyzer analyze`，同时支持离线输入与 GitHub Action 运行。
- 报告：保持 `risk-report/v1`；字段变更必须同步 schema、fixture、测试和 ADR。
- Action 输入：`config`、模型配置、文件/patch 上限、评论与 Artifact 开关、`fail-on`、`debug`。
- Action 发布：仅下载精确版本的二进制并校验 SHA-256，不使用浮动的“latest”二进制。

## 9. 实施日志

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

### 2026-08-25 - 文档核实与刷新

- 状态：`DONE`（文档交付；不改变任何软件功能或 C1-C9 检查点状态）。
- 修改：仅更新本文件 `DEVELOPMENT_PLAN.md`。新增第 2 节「给项目所有者的使用说明」，其后各节顺延重新编号（原 2-8 节变为 3-9 节）；按当日核实结果重写第 5 节「当前已核实状态」；在第 4 节「状态规则」补充状态变更前置条件、PARTIAL 约束、「当前已核实状态」只写已核实结论、以及实施日志回写要求；将第 3 节中“目标目录将在工程目录迁移切片中建立”更新为已建立的事实表述。未修改 `spec/` 下任何文件，未修改任何代码。
- 核实命令与结果（2026-08-25 实际执行）：`server/` 内 `go test ./...` 通过（5 包 ok）、`go test -race ./...` 通过（5 包 ok）、`go vet ./...` 退出码 0、`gofmt -l .` 无输出；根目录 `go test ./server/...` 通过、`git status --short` 无输出（工作区干净）、`git log --oneline -5` 正常显示（分支 `main`，最新提交 `0c62598`，2026-08-15）。
- 目录核实：`client/` 仅含 `README.md`，无 `action.yml`；不存在任何 CR-SC-001 / 浮动依赖引用实现代码（检索命中的仅为领域层与 schema 中既有的 `supply_chain` 风险类别枚举）。切片 6 `CR-SC-001` 保持 `PLANNED`。
- 文档一致性核对：本文件第 7 节任务表（切片 1-5 `DONE`、6-24 `PLANNED`）与 `spec/implementation-status.md` 的检查点进度（C0-C2 completed、C3 in_progress、C4-C9 pending）一致，两文档的下一功能均为切片 6 `CR-SC-001`，无出入。另观察到 `spec/implementation-status.md` 内部 A1 段落仍保留“工程目录迁移仍是下一项 PLANNED 工作”的过时句子，与其自身 A2 段落（迁移已完成）矛盾；因约束本轮不改 `spec/` 文件，仅在此次记录该出入。
- 已修正旧结论：2026-08-15 记录的“工作区已有未提交的规则和文档改动”已过时——当前工作区干净，历史改动已在提交 `0c62598` 中入库。
- 未做：未新增或修改任何风险规则、测试或配置；未接入真实 GitHub API、模型或发布流程。
- 下一功能：`CR-SC-001` 浮动依赖引用。

### 2026-08-25 - `CR-SC-001` 浮动依赖引用

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/floating_dependency.go` 与 `floating_dependency_test.go`；更新 `spec/07-deterministic-analyzers.md`（新增 4.7 节）与 `spec/implementation-status.md`（进度、A1 过时句子更正、切片记录、下一步计划）；本文件任务表与日志同步。
- 行为：检测新增行中的浮动版本引用——Action 的分支/大版本标签（`@main`/`@v1` 等）、镜像 `latest` 或无标签（Dockerfile 裸单词名跳过）、`go get/install` 的 `@latest|@master|@main`；完整 SHA、digest 摘要和三段式精确版本视为固定。输出 `supply_chain` 类别 signal（confidence 0.8，weight 20），按文件合并右侧行级 Evidence。
- 执行方式说明：后台开发代理因模型服务商用量限额连续失败（含两次断点恢复与一次全新代理），改由主会话直接实现；实现前已完成必读规范阅读，验证标准未降低。
- 验证：`server/` 内定向测试 `-run FloatingReference -v` 通过（6 个）；`go test ./...` 通过（5 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：词法规则不识别 `releases/*` 等未列举浮动形态；YAML 无标签裸单词镜像可能对自建别名误报；signal 未进入策略引擎，不计算分数或门禁。
- 下一功能：`CR-SEC-002` Secret-like literal。

### 2026-08-26 - `CR-SEC-002` Secret-like literal

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/secret_literal.go` 与 `secret_literal_test.go`；更新 `spec/07-deterministic-analyzers.md`（新增 4.8 节）与 `spec/implementation-status.md`（进度、切片记录、已知限制、下一步计划）；本文件任务表与日志同步。
- 行为：检测所有非二进制文件新增行中的疑似密钥——GitHub/AWS/Slack/Google 已知令牌格式、私钥块起始标记、以及向凭据命名变量的非空字面量赋值（支持 `=`、`:=`、`:` 赋值符）；输出 security 类别 signal（confidence 0.85，weight 30），按文件合并证据并稳定排序。Evidence 绝不携带完整原始密钥值：不设置 Excerpt，Fact 只含类型、键名与行号并注明原始值未写入报告，有专项测试遍历全部字符串字段断言不含原文。豁免各家官方示例假值、占位符、env 引用、注释行以及过短/低字符多样性/纯字母或纯数字的字面量。
- 执行方式说明：后台开发代理两次因服务商通道错误中断，其恢复运行在断线前已完成一版实现与文档但未汇报；主会话在不知情下并行实现了同功能并覆盖了其代码文件，发现重叠后已逐项核对，三处文档均以实际落盘并验证通过的最终代码为准修正。
- 验证：`server/` 内定向测试 `-run SecretLiteral -v` 通过（5 个）；`go test ./...` 通过（5 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：格式与赋值启发式不验证凭据有效性，刻意不引入熵计算；要求赋值值含数字或符号导致纯字母口令漏报，含空格口令只截取首段可能整体漏报；仅匹配英文凭据关键词，不分析跨行赋值；signal 未进入策略引擎，不计算分数或门禁。
- 下一功能：`CR-SEC-003` 授权边界变化。

### 2026-08-26 - `CR-SEC-003` authorization boundary change

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/authorization_boundary.go` 与 `authorization_boundary_test.go`；更新 `spec/07-deterministic-analyzers.md`（新增 4.9 节）与 `spec/implementation-status.md`（进度、切片记录、已知限制、下一步计划）；本文件任务表与日志同步。
- 行为：删除侧检测授权关键词（authorize/authenticate/check_auth/is_admin/has_role 等）与 `.Use(...auth...)` 中间件注册被删，输出 side=left Evidence；新增侧检测 skip_auth/disable_auth/allow_all/permit_all/no_auth 等显式放宽写法（side=right，要求独立单词边界）；新增正常鉴权强化代码不报；注释与字符串经 goCodeLine 脱敏不报。输出 security 类别 signal（confidence 0.7，weight 25），按文件合并、行号+侧别稳定排序去重。
- 执行方式说明：后台代理两次因服务商通道错误中断且未产出任何改动，由主会话直接实现并验证。
- 验证：定向 `-run AuthorizationBoundary` 通过（4 个）；`go test ./...` 通过（5 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：词法规则不构建调用图，不判断被删代码是否有等价替代防护，重构改名可能产生双候选；仅覆盖 Go 源文件；signal 未进入策略引擎，不计算分数或门禁。
- 下一功能：`CR-TEST-001` 风险变更测试证据。

### 2026-08-26 - `CR-TEST-001` risk-bearing change without test evidence

- 状态：`DONE`。
- 修改：新增 `server/internal/signals/test_evidence.go` 与 `test_evidence_test.go`；更新 `spec/07-deterministic-analyzers.md`（新增 4.10 节）与 `spec/implementation-status.md`（进度、切片记录、已知限制、下一步计划）；本文件任务表与日志同步。
- 行为：敏感路径（路径段含 migration/auth/payment/billing/security/admin 子串）变更时，若同一顶级目录下未观察到 `_test.go`/tests/test/testdata 类测试变更，输出 testability 类别 signal（confidence 0.6，weight 15），每个未覆盖敏感文件一条 side=file Evidence；Fact 使用「未观察到……建议补充验证」观察式措辞，绝不断言「没有测试」；纯删除的敏感文件同样纳入。
- 执行方式说明：后台代理因服务商通道错误中断。其断线前已写入一版文档描述但未及汇报代码；主会话并行实现了同功能并落盘验证，发现重叠后删除了重复的 4.10 章节，文档以实际落盘并验证通过的最终代码为准。
- 验证：定向 `-run TestEvidence` 通过（3 个）；`go test ./...` 通过（5 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：关键词子串匹配可能把 author.go 等非敏感命名误标为敏感；顶级目录粗匹配可能造成漏报；signal 未进入规则运行器。
- 下一功能：信号运行器与 fixture 接入（切片 10）。

### 2026-08-26 - 信号运行器与 fixture 接入

- 状态：`DONE`。C3 检查点（确定性风险规则）整体完成。
- 修改：新增 `server/internal/signals/runner.go`、`runner_test.go` 与 `internal/signals/testdata/golden_runner.json`；更新 `spec/07-deterministic-analyzers.md`（新增 6.1 运行器小节）、`spec/implementation-status.md`（C3 收官、检查点表、切片记录、下一步计划）；本文件任务表与日志同步。
- 行为：默认运行器按 spec 顺序注册全部十条规则逐个执行；聚合结果按「RuleID+Fact+Evidence 签名」去重，并按「类别→文件→行号→侧别→规则→事实」全局稳定排序；任一分析器失败立即返回携带其规则 ID 的错误且后续不再执行；金样快照固化多规则聚合输出，由 `GOLDEN_UPDATE=1` 显式刷新。
- 执行方式说明：后台代理因服务商通道错误中断且零产出，由主会话直接实现并验证。
- 验证：定向 `-run Runner` 全部通过（5 个测试）；金样含全部十条规则 ID；`go test ./...` 通过（5 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：运行器不计算分数、门禁或降级记录（属 C4 及以后）；golden 快照需随协议演进显式刷新；`spec/fixtures/cases.json` 保持端到端评测定位未与本层耦合。
- 下一功能：C4 策略引擎（切片 11）。

### 2026-08-26 - C4 策略引擎

- 状态：`DONE`。
- 修改：新增 `server/internal/policy/policy.go` 与 `policy_test.go`；更新 `spec/implementation-status.md`（当前状态、C4 切片记录、下一步计划）；本文件任务表与日志同步。`spec/02-risk-model.md` 未修改。
- 行为：纯函数 `Evaluate([]domain.RiskSignal)` 把运行器输出的信号集合转换为带证据校验的 Finding——ID 为确定性复合键 `<RuleID>:<首个证据文件>:<起始行>`（非法字符替换为 `_`）、EvidenceStatus=confirmed、Evidence 深拷贝继承、InlineEligible 取自右侧正数行证据、Impact/Recommendation 使用不断言漏洞的谨慎措辞；任何一条非法证据立即返回含规则 ID 与文件路径的错误，无证据或 weight 超出 1-40 的信号同样拒绝。分数严格套用 spec/02 第 5 节初始配置：raw=Σ(weight×evidence_factor×exposure_factor)，行级因子 1.0／文件级 0.7，exposure 缺省固定 1.0（规格未定义缺省值的中性口径），mitigation 抵扣无数值目录、结构性为 0，final=min(100, max(0, raw))；维度分数按同一公式作用于类别子集并封顶 [0,100]；单条 Finding severity=LevelFromScore(单信号贡献分)，不引入新常量。默认门禁恒不阻塞合并。输入规范化排序后求值，重复与乱序输入结果 DeepEqual 一致。
- 验证：`go test ./internal/policy -v` 通过（6 个测试：七类正例、空输入、五组非法证据反例、24/25/49/50/74/75 阈值边界、重复与乱序稳定、门禁中性）；`go test ./...` 通过（6 包 ok）；`go test -race ./...` 通过；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：finding 级 severity 映射与维度分数公式在 spec/02 未明文定义，当前采用「单信号贡献分过同一 LevelFromScore 阈值」「总分公式作用于类别子集」的解释口径，已在代码注释与 implementation-status 中记录，等待用户确认后如需调整只影响本包；exposure_factor 与 mitigation_credit 的真实取值留待 Phase 4 调优；findings 上限与降级原因归切片 12 报告构建处理；显式多信号组合升级规则未实现。
- 下一功能：C4 报告构建与渲染（切片 12）。

### 2026-08-26 - C4 报告构建与渲染

- 状态：`DONE`。C4 检查点（风险策略和报告构建）整体完成。
- 修改：新增 `server/internal/report/build.go`（BuilderInput 显式入参 + Build 组装 + RenderJSON）、`server/internal/report/markdown.go`（RenderMarkdown 确定性渲染）、`build_test.go`、`markdown_test.go` 与 `internal/report/testdata/golden_report.md` 金样；更新 `spec/implementation-status.md`（顶部状态改为 C5、检查点表 C4 行 completed、新增 C4 切片小节、下一步计划替换为切片 13 离线 CLI）；本文件任务表与日志同步。未修改 `spec/schemas/`、`spec/fixtures/` 与任何协议文档，无需 ADR。
- 行为：Build 以显式入参组装 RiskReport 并强制通过包内既有双重校验（领域 + 内置 risk-report/v1 schema），非法输入立即报错；findings 超过 spec/03 第 6 节明文上限 20 条时按 domain.SortFindings 稳定排序保留前 20 条并追加 code=`findings-truncated` 的显式降级原因（消息含截断前后数量），总分与维度统计仍基于全部线索；降级原因非空推导 status=degraded、否则 completed（已记录的实现层口径）；RenderMarkdown 按 spec/01 第 5.2 节顺序确定性渲染总体分数/级别、维度表、每条 Finding 的标题/严重度/证据位置（文件:行号+侧别）/建议与降级原因，表格单元格竖线转义为 `\|`；金样由 `GOLDEN_UPDATE=1` 显式刷新，普通模式逐字节比对。
- 验证：`go test ./internal/report -v` 通过（16 个测试：正例、JSON 往返、空输入、超限截断、8 组非法输入、确定性、金样、转义、nil 拒绝等）；`go test ./...` 通过（6 包 ok）；`go test -race ./...` 通过（6 包 ok）；`go vet ./...` 退出码 0；`gofmt -l .` 无输出。
- 限制：状态推导规则与 `findings-truncated` 原因码措辞为待用户追认的实现层口径；Runtime 元数据作为显式入参传入、TestGaps 恒为空列表（无生产者）；Markdown 尚未接入 Step Summary、Artifact 或评论发布链路。
- 下一功能：离线 CLI（切片 13），随后进入 C5 Fake GitHub Client（切片 14）。
