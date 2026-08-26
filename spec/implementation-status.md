# Implementation Status

## 当前状态

- 项目阶段：Phase 1 - 离线确定性内核
- 当前检查点：C5（Fake GitHub Client）
- 当前功能：`C5 Fake GitHub Client`（下一功能）
- 总体状态：in_progress（C4 completed）
- 最后更新：2026-08-26

## 检查点列表

| 检查点 | 功能                                   | 状态        |
| ------ | -------------------------------------- | ----------- |
| C0     | 设计文档、agents.md、Schema 和 Fixture | completed   |
| C1     | 领域对象和报告协议                     | completed   |
| C2     | unified diff 解析                      | completed   |
| C3     | 确定性风险规则                         | completed   |
| C4     | 风险策略和报告构建                     | completed   |
| C5     | Fake GitHub Client                     | pending     |
| C6     | GitHub API 接入                        | pending     |
| C7     | Artifact 和 Step Summary               | pending     |
| C8     | 评论幂等发布                           | pending     |
| C9     | AI Provider 和结构化输出校验           | pending     |

## 已完成工作

### A0：GitHub 仓库规范文件（非检查点辅助交付）

状态：completed（2026-08-14）

完成内容：

- `README.md`：中文主文档，含项目定位、核心特性、工作原理、快速开始、输入配置、输出、安全设计、开发说明和文档索引。
- `README.en.md`：英文版，与中文版结构对应并互相链接。
- `LICENSE`：标准 MIT 许可证文本（版权人 `Change Risk Analyzer Authors`，可替换）。
- `.gitignore`：覆盖 Go 编译产物、构建输出、覆盖率、本地报告输出目录、日志、密钥、IDE 和操作系统文件。

约束遵循：

- Action 用法示例标注为契约草案，`uses` 地址未虚构正式值。
- README 中的安全表述与 `spec/08-security-privacy.md` 一致。
- 未创建 `action.yml`、`go.mod`，留给对应实现检查点。

验证结果：

- 所有 spec 相对链接有效。
- 无 linter 错误。

### C0：设计和协议冻结

状态：completed

完成内容：

- 建立 `agents.md`
- 建立 `spec/` 文档体系
- 建立 `risk-report.schema.json`
- 建立评测 Fixture 清单
- 确定 GitHub Action 运行形态
- 确定不执行不可信 PR 代码

验证结果：

- JSON Schema 可解析
- Fixture 配置可解析
- 文档没有已知 linter 错误

## 当前工作

### A1：开发总控计划（非软件检查点记录）

- 2026-08-15 新增根目录 `DEVELOPMENT_PLAN.md`，用于约束后续会话的切片范围、状态证据、验收命令和实施日志。
- 该记录仅说明计划文档已写入；不改变 C1-C9 的实现状态，也不代表目录迁移、GitHub Action 或任何未实现能力已经完成。
- 本次未修改已有风险规则实现。（更正：本段原写有“工程目录迁移仍是下一项 PLANNED 工作”，与下方 A2 段落矛盾——迁移已于 2026-08-15 完成，此为历史遗留句子的修正记录。）

### A2：工程目录迁移（非检查点辅助交付）

状态：completed（2026-08-15）

完成内容：

- 新增根目录 `go.work`，使用 `./server` 作为本地 Go 工作区模块。
- 将根目录 `go.mod`、`go.sum` 和全部 `internal/` 原样迁移至 `server/`；模块路径保持 `change-risk-analyzer`，既有 Go 导入路径不变。
- 新增 `client/README.md`，明确该目录是未来 GitHub Action 包装层，不是网页客户端，也尚未包含 `action.yml`。
- 新增 `spec/decisions/004-repository-module-layout.md`，记录客户端包装层与服务端分析内核的模块边界。
- 更新架构说明、README 和 `.gitignore`，使 `server/internal/report` 始终作为源代码跟踪。

验证结果：

- 根目录 `go test ./server/...` 和 `go vet ./server/...` 通过。
- `server/` 内 `go test ./...`、`go test -race ./...` 和 `go vet ./...` 通过。
- `gofmt` 检查和 `git diff --check` 通过。

已知限制：

- 目录迁移不提供 CLI、GitHub Action、二进制发布或网页功能。
- `client/` 只能在版本化二进制发布完成后开始实现可安装 Action。

### Phase 1 Slice：离线事件解析

状态：completed（2026-08-15）

完成内容：

- 新增 `server/internal/event/pull_request.go`，提供纯函数 `ParsePullRequestEvent([]byte)`，将完整 Pull Request Action event JSON 转为经过领域校验的 `ReviewRequest`。
- 映射 `opened`、`synchronize`、`reopened`；其他动作保留为 `unknown`，使调用方能按策略决定是否分析。
- 根据 head repository 与目标 repository 区分 `same_repository`、`fork` 和 `unknown`，不读取 GitHub API。
- 为空输入、非法 JSON、缺失 PR、非法 SHA 和 `workflow_dispatch` 返回不包含原始 payload 的结构化错误。
- 新增 `server/internal/event/pull_request_test.go`，覆盖同仓、Fork、未知动作、未知来源和全部失败路径。

验证结果：

- `go test ./server/internal/event` 通过。
- 根目录 `go test ./server/...` 和 `go vet ./server/...` 通过。
- `server/` 内 `go test -race ./...`、`go vet ./...` 和 `gofmt` 检查通过。

已知限制：

- 真实 `workflow_dispatch` 不携带可发布的 base/head SHA；当前解析器明确拒绝，后续 C6 GitHub 适配器必须根据显式 PR number 重新读取并验证身份。
- 本切片不读取事件文件路径、不调用 GitHub API，也不提供 CLI。

### C1：领域对象和报告协议

状态：completed（2026-08-15）

目标：

- 实现 `ReviewRequest`
- 实现 `ChangeSet`
- 实现 `RiskSignal`
- 实现 `Finding`
- 实现 `RiskReport`
- 实现报告基本校验

目标文件：

- `internal/domain/request.go`
- `internal/domain/change.go`
- `internal/domain/risk.go`
- `internal/report/validate.go`
- `internal/domain/request_test.go`
- `internal/domain/change_test.go`
- `internal/domain/risk_test.go`
- `internal/report/validate_test.go`

验收标准：

- 所有领域对象能够序列化。
- 非法 `head_sha` 被拒绝。
- 风险分数限制在 0 到 100。
- 高风险 finding 必须包含 Evidence。
- 输出能够通过 `risk-report.schema.json`。
- 有对应单元测试。

已实现行为：

- 领域对象提供 JSON 序列化字段和不变式校验。
- 请求校验拒绝格式错误的仓库引用、事件动作、来源类型和 base/head SHA。
- 变更集合校验汇总文件计数、增删行数和截断原因的一致性。
- 风险信号、Evidence、Finding、维度、测试缺口、降级原因和运行元数据执行枚举、范围和证据约束。
- 报告校验同时执行领域校验和 `risk-report/v1` JSON Schema 校验，并区分两类错误。
- 高/严重 Finding 必须包含有效 Evidence；行级发布候选必须定位到右侧正数行。
- 风险级别按 0/25/50/75 阈值由分数确定；Finding 和维度提供稳定排序函数。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 已应用于新增测试文件。
- 内置 schema 与 `spec/schemas/risk-report.schema.json` SHA-256 一致。

已知限制：

- 当前仅实现领域校验和 schema 校验，不包含报告构建、Markdown 渲染或策略评分。
- JSON Schema 校验使用仓库内置副本；后续协议变更必须同步更新两份文件并增加一致性检查。

### C2：unified diff 解析

状态：completed（2026-08-15）

完成内容：

- 新增 `internal/change/parser.go`，提供离线 `ParseUnifiedDiff` 入口和资源上限选项。
- 解析新增、修改、删除、重命名、复制、二进制和无 hunk 文件。
- 解析 hunk 行号，统计完整 additions/deletions，并返回新增右侧行号索引。
- 规范化仓库相对路径，拒绝绝对路径、Unix/Windows 穿越路径和 NUL 输入。
- 支持默认及自定义单文件/总 patch 上限，超限时设置显式截断状态和稳定原因。
- 新增 `internal/change/parser_test.go`，覆盖正常、边界、畸形、恶意路径、重复解析和超限输入。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 检查通过。

已知限制：

- 解析器只接受 git unified diff 文件段，不接受无 `diff --git` 文件头的裸 hunk。
- 新增行号索引是 C2 内部结果，尚未接入 Evidence、风险规则或报告构建。

### C3 Slice：`CR-SEC-001` Workflow write permission

状态：completed（2026-08-15）

本轮功能：`CR-SEC-001` Workflow write permission。

完成内容：

- 新增 `internal/signals/workflow_permissions.go`，实现 `Analyzer` 接口和 Workflow 写权限分析器。
- 新增 `internal/signals/workflow_permissions_test.go`，覆盖正例、反例、边界和恶意文本输入。
- 识别新增的细粒度 `write` 权限、inline permissions 和 `write-all`。
- 只处理 Workflow 文件的新增 patch 行，按文件合并 signal，并生成右侧行级 Evidence。
- 对 `read`、普通配置文件、注释、二进制和无 patch 输入保持无信号。
- 新增正例、反例、边界、注释、缺失 patch、稳定排序和取消上下文测试。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 检查通过。

已知限制：

- 当时只实现 `CR-SEC-001`；后续已补充 `CR-API-001`，外部输入、迁移、依赖和 Go 并发规则仍未实现。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-DATA-001` destructive migration

状态：completed（2026-08-15）

完成内容：

- 新增 `internal/signals/destructive_migration.go`，检测迁移 SQL 中新增的破坏性数据操作。
- 覆盖 `DROP TABLE`、`DROP COLUMN`、`TRUNCATE` 和无界 `DELETE`/`WHERE 1=1`。
- 限定迁移目录 SQL 文件，忽略安全变更、注释/字符串、删除侧旧操作、二进制和无 patch 文件。
- 新增 `internal/signals/destructive_migration_test.go`，覆盖正例、反例、边界、路径、畸形 patch、稳定排序和取消上下文。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 检查通过。

已知限制：

- 当前规则不分析回滚、备份、双写或跨行 SQL 数据流。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-EXEC-001` untrusted command execution signal

状态：completed（2026-08-15）

完成内容：

- 新增 `internal/signals/command_execution.go`，检测新增 Go patch 行中的动态 `exec.Command`/`exec.CommandContext` 调用。
- 覆盖动态命令名、shell `-c`/`/c`/`-Command`、字符串拼接和 `fmt.Sprintf` 参数。
- 只输出带右侧行号 Evidence 的候选 signal，不执行命令、不判断外部输入可达性。
- 新增 `internal/signals/command_execution_test.go`，覆盖正例、静态反例、注释/字符串、文件边界、畸形 patch、稳定排序和取消上下文。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 检查通过。

已知限制：

- 当前规则只处理单行 `exec.Command` 调用，不分析跨行参数和复杂数据流。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-API-001` exported API change

状态：completed（2026-08-15）

完成内容：

- 新增 `internal/signals/api_changes.go`，实现 Go 导出函数、类型、变量和常量的删除/签名替换检测。
- 新增 `internal/signals/api_changes_test.go`，覆盖删除、签名替换、泛型函数、类型/变量/常量、兼容性新增和路径边界。
- Evidence 同时支持删除侧和新增侧行号，signal 按文件和行号稳定排序。
- 排除 `internal/`、`vendor/`、`_test.go`、注释、字符串和只新增 API，控制词法规则误报。

验证结果：

- `go test ./...` 通过。
- `go test -race ./...` 通过。
- `go vet ./...` 通过。
- `gofmt` 检查通过。

已知限制：

- 当前只实现导出声明级线索，不分析路由、协议字段、消费者兼容性或接口方法体变化。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-REL-001` external request without timeout

状态：completed（2026-08-15）

完成内容：

- 新增 `server/internal/signals/external_request_timeout.go`，检测新增 Go patch 行中缺少可见 timeout 或取消边界的有限范围 HTTP 调用。
- 覆盖 `http.Get`、`http.Head`、`http.Post`、`http.PostForm`、`http.DefaultClient.Do` 和内联 `http.Client.Do`。
- 对可见 `WithContext`、`NewRequestWithContext`、`WithTimeout`、`WithDeadline` 或 `http.Client.Timeout` 保持无信号；命名 client 不作配置推断。
- 只输出带新增右侧行号的候选 signal，不执行网络请求、不判断真实可达性或最终严重度。
- 新增 `server/internal/signals/external_request_timeout_test.go`，覆盖正例、反例、边界、注释/字符串、删除侧、稳定排序、取消上下文和畸形 patch。

验证结果：

- `go test ./server/internal/signals -run ExternalRequestWithoutTimeout` 通过。
- 根目录 `go test ./server/...` 和 `go vet ./server/...` 通过。
- `server/` 内 `go test -race ./...`、`go vet ./...` 和 `gofmt` 检查通过。

已知限制：

- 规则不分析命名 client 的构造、跨行 request/context 传播、重试和服务入口暴露度。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-CON-001` new goroutine lifecycle signal

状态：completed（2026-08-15）

完成内容：

- 新增 `server/internal/signals/goroutine_lifecycle.go`，检测新增 Go goroutine 缺少可见生命周期或取消信号的候选。
- 覆盖命名 `go` 调用与匿名闭包；在调用行或新增闭包内识别 `ctx`、停止通道、`cancel` 和 `WaitGroup.Done` 等线索。
- 只输出启动行的新增右侧行号 Evidence，不执行 goroutine、不分析被调用函数或跨文件数据流。
- 新增 `server/internal/signals/goroutine_lifecycle_test.go`，覆盖正例、可见生命周期反例、边界、注释/字符串、稳定排序、取消上下文和畸形 patch。

验证结果：

- `go test ./server/internal/signals -run GoroutineLifecycle` 通过。
- 根目录 `go test ./server/...` 和 `go vet ./server/...` 通过。
- `server/` 内 `go test -race ./...`、`go vet ./...` 和 `gofmt` 检查通过。

已知限制：

- 规则不证明 goroutine 泄漏，不分析跨函数取消、channel 生产/消费、真实 WaitGroup 配对或长闭包。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-SC-001` floating dependency reference

状态：completed（2026-08-25）

完成内容：

- 新增 `server/internal/signals/floating_dependency.go`，实现 `CR-SC-001` 浮动依赖引用分析器。
- 覆盖三类新增行形态：Workflow YAML 的 `uses:` 分支/大版本浮动标签；`.yml`/`.yaml` 的 `image:` 键与 `Dockerfile*` 的 `FROM` 行中 `latest` 或无标签镜像（Dockerfile 裸单词名跳过以避开构建阶段误报）；shell/Makefile/Dockerfile `RUN`/workflow `run:` 中 `go get|install` 的 `@latest`、`@master`、`@main` 引用。
- 固定判定：完整 40/64 位 commit SHA、`@sha256:`/`@sha512:` digest、三段式精确版本、显式非 latest 标签。
- 输出 `supply_chain` 类别 signal（confidence 0.8，weight 20），按文件合并证据，行号来自 C2 解析的右侧新增行。
- 新增 `server/internal/signals/floating_dependency_test.go`，覆盖正例、固定引用反例、注释/删除侧/二进制/无 patch/无关路径/穿越路径、compose 与脚本路径、稳定排序、重复分析幂等、取消上下文、非法 ChangeSet 和畸形 patch。

验证结果：

- `go test ./internal/signals -run FloatingReference -v` 通过（6 个测试）。
- `server/` 内 `go test ./...` 通过（5 包 ok）、`go test -race ./...` 通过、`go vet ./...` 退出码 0、`gofmt -l .` 无输出。

已知限制：

- 词法规则不识别 `releases/*` 等未列举的浮动形态，不判断依赖真实可达性或是否被替换。
- YAML 中无标签裸单词镜像按真实镜像处理，若为部署工具自建别名会产生候选误报。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-SEC-002` secret-like literal

状态：completed（2026-08-26）

完成内容：

- 新增 `server/internal/signals/secret_literal.go`，实现 `CR-SEC-002` 疑似密钥字面量分析器。
- 覆盖三类新增行形态：已知令牌格式（GitHub Token、AWS Access Key ID、Slack Token、Google API Key）；私钥块起始标记（`-----BEGIN ... PRIVATE KEY-----` 含各变体）；向凭据命名变量（password/passwd/secret/token/api_key/access_key/access_token/private_key/auth_token，允许作为更长变量名尾部）赋予长度至少 6 的非空字面量的赋值形态，支持 `=`、`:=` 与 YAML/JSON 的 `:` 赋值符。
- 安全不变量：Evidence 刻意不设置 Excerpt，Fact 仅含线索类型、键名（赋值形态）和行号描述，并注明原始值未写入报告；有专项测试构造完整假 token 输入并遍历全部 Evidence 字符串字段断言不含原文。
- 豁免（误报控制）：官方文档标准示例假值（如 `AKIAIOSFODNN7EXAMPLE`）、尖括号占位符、`${VAR}`/模板引用、env 引用（`os.Getenv`、`process.env`）、changeme/重复字符类占位、短于 6 字符的值、字符种类不超过 2 的值、纯数字与纯字母值、整行与行内注释（`#`/`//`）、删除侧旧内容、二进制/无 patch 文件、路径穿越输入。所有带 patch 的普通文件均在扫描范围内（含测试文件），依赖上述过滤器压低噪声。
- 扫描范围覆盖所有带 patch 的非二进制文件（密钥可出现在任何配置/代码文件），复用路径安全检查拒绝穿越路径；不引入熵计算。
- 输出 security 类别 signal（confidence 0.85，weight 30），按文件合并证据，行级去重稳定排序，同一输入重复分析结果完全一致。
- 新增 `server/internal/signals/secret_literal_test.go`（5 个测试）：四类格式+私钥正例与打码断言、官方示例假值反例、占位符/env/注释反例、作用域边界（删除侧/Go 注释/二进制/无 patch/穿越路径）、多文件稳定排序与幂等 DeepEqual、取消上下文、非法 ChangeSet、畸形 patch 报错。

验证结果：

- `go test ./internal/signals -run SecretLiteral -v` 通过（5 个测试）。
- `go test ./...` 通过（5 包 ok）。
- `go test -race ./...` 通过（5 包 ok）。
- `go vet ./...` 退出码 0。
- `gofmt -l .` 无输出。

已知限制：

- 格式与赋值启发式不引入熵计算，无法验证凭据是否真实有效或已被轮换。
- 要求赋值字面量至少含一个数字或符号：纯字母口令会漏报；含空格口令只截取首段，可能整体漏报。
- 仅匹配英文凭据关键词，不识别未列举的自定义令牌格式；不分析跨行赋值和多行字符串。
- signal 尚未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-SEC-003` authorization boundary change

状态：completed（2026-08-26）

完成内容：

- 新增 `server/internal/signals/authorization_boundary.go`，实现 `CR-SEC-003` 授权边界变化分析器。
- 删除侧检测：授权关键词（authorize/authenticate/check_auth/require_auth/require_permission/verify_token/is_admin/has_role/enforce_policy 等，大小写不敏感）与 `.Use(...auth...)` 中间件注册形态的行被删除时输出 `side=left` Evidence。
- 新增侧检测：skip_auth/disable_auth/allow_all/permit_all/no_auth/without_auth/anonymous_access/public_route 等显式放宽写法（含 `-`/`_` 变体、要求独立单词边界）输出 `side=right` Evidence；新增正常鉴权强化代码不报。
- Go 注释与字符串字面量经 `goCodeLine` 脱敏后不报；测试文件、`vendor/`、二进制、无 patch 与穿越路径全部排除。
- 输出 security 类别 signal（confidence 0.7，weight 25），按文件合并证据，行号+侧别稳定排序去重。
- 新增 `server/internal/signals/authorization_boundary_test.go`（4 个测试）：删除+放宽双证据正例、新增鉴权强化反例、注释/字符串/测试文件/vendor/二进制/无 patch/穿越路径反例、多文件稳定排序幂等、取消上下文、非法 ChangeSet、畸形 patch 报错。

验证结果：

- `go test ./internal/signals -run AuthorizationBoundary` 通过（4 个测试）。
- `server/` 内 `go test ./...` 通过（5 包 ok）、`go test -race ./...` 通过、`go vet ./...` 退出码 0、`gofmt -l .` 无输出。

已知限制：

- 词法规则不构建调用图，不判断被删代码是否有等价替代防护；重构改名场景可能同时产生删除与新增两类候选。
- 仅覆盖 Go 源文件，其他语言的授权配置不在范围。
- signal 未进入策略引擎，不计算 Finding、风险分数或门禁。

### C3 Slice：`CR-TEST-001` risk-bearing change without test evidence

状态：completed（2026-08-26）

完成内容：

- 新增 `server/internal/signals/test_evidence.go`，实现 `CR-TEST-001` 测试证据缺失候选分析器。
- 敏感路径定义：任一路径段包含 migration/auth/payment/billing/security/admin（大小写不敏感子串匹配，`author.go` 类名字也会命中，已文档化）。
- 测试证据判定：同一 ChangeSet 存在 `_test.go` 文件或 `tests/`/`test`/`testdata` 目录变更且与敏感文件共享顶级目录即视为覆盖（粗粒度，宁可漏报不误报）。
- 输出 `testability` 类别 signal（confidence 0.6，weight 15），每个未覆盖敏感文件一条 `side=file` Evidence，Fact 使用「未观察到……建议补充验证」的观察式措辞；按顶级目录分组稳定排序。
- 纯删除的敏感文件（无 patch）同样纳入；测试类文件自身、二进制、穿越路径排除。
- 新增 `server/internal/signals/test_evidence_test.go`（3 个测试）：未覆盖正例与分组排序、覆盖/非敏感/测试自身反例、删除纳入、二进制/穿越/取消上下文/非法 ChangeSet/幂等边界。

验证结果：

- `go test ./internal/signals -run TestEvidence` 通过（3 个测试）。
- `server/` 内 `go test ./...` 通过（5 包 ok）、`go test -race ./...` 通过、`go vet ./...` 退出码 0、`gofmt -l .` 无输出。

已知限制：

- 关键词子串匹配可能把 author.go 等非敏感命名误标为敏感。
- 顶级目录粗匹配可能把无关测试变更当作覆盖证据造成漏报。
- signal 未进入规则运行器（切片 10），不计算分数或门禁。

### C3 Slice：信号运行器与 fixture 接入

状态：completed（2026-08-26）

完成内容：

- 新增 `server/internal/signals/runner.go` 与 `runner_test.go`：`DefaultRunner` 按 spec/07 第 4 节顺序注册全部十条已实现规则；`NewRunner` 支持自定义注册表并拒绝空 ID、nil 分析器与重复 ID。
- 聚合语义：按「RuleID+Fact+全部 Evidence 签名」去重；按「类别固定维度顺序→首个证据文件路径→起始行→侧别→规则 ID→事实」全局稳定排序；任一分析器失败或上下文取消立即返回携带规则 ID 的错误，后续分析器不再执行。
- fixture：新增 `server/internal/signals/testdata/golden_runner.json` 金样快照，覆盖十条规则代表场景的完整聚合输出，由 `GOLDEN_UPDATE=1` 显式刷新；`spec/fixtures/cases.json` 保持面向未来端到端评测的既有定位。
- 新增 6 个测试：全规则聚合与排序断言（10 条规则 ID 全覆盖）、去重生效、错误传播带 Rule ID 且后续分析器不执行、取消上下文、空 ChangeSet 返回空结果、金样比对。

验证结果：

- `go test ./internal/signals -run Runner` 全部通过。
- `server/` 内 `go test ./...` 通过（5 包 ok）、`go test -race ./...` 通过、`go vet ./...` 退出码 0、`gofmt -l .` 无输出。

已知限制：

- 运行器只做聚合、去重与排序，不计算分数、门禁或降级记录（属 C4 及以后切片）。
- golden 快照需随协议或规则演进显式人工刷新。

### C4 Slice：策略引擎

状态：completed（2026-08-26）

完成内容：

- 新增 `server/internal/policy/policy.go` 与 `policy_test.go`：纯函数 `Evaluate([]domain.RiskSignal)` 把运行器聚合输出转换为 Finding，计算各维度分数、总分与级别，并给出默认门禁建议（ShouldBlock 恒为 false）。
- 证据校验红线：逐条 Evidence 先于整体校验执行，任一条失败立即返回包含规则 ID 与文件路径的错误；没有任何证据、weight 超出 spec/02 规定的 1-40 范围的信号同样被拒绝，绝不静默跳过。
- Signal→Finding 转换：ID 为确定性复合键 `<RuleID>:<首个证据文件>:<起始行>`（路径中不属于领域 ID 字符集的字段替换为 `_`，超长截断）；Category 继承、EvidenceStatus=confirmed、Confidence 继承、RuleIDs 单元素、Evidence 深拷贝继承；InlineEligible=存在 side=right 正数行号证据；Title 取自 Fact 截断，Impact/Recommendation 使用不断言漏洞、只描述待确认事实与复核动作的谨慎措辞。
- 分数口径（spec/02 第 5 节初始配置）：raw=Σ(signal_weight×evidence_factor×exposure_factor)，evidence_factor 行级 1.0／文件级 0.7／仅模型线索 0.3；exposure_factor 在缺少公共/内部上下文时固定为 1.0（规格未定义缺省值，此为已记录的中性口径）；mitigation_credit 无数值目录、结构性恒为 0；final=min(100, max(0, raw))。单条 Finding 的 severity = LevelFromScore(round(单信号贡献分))，未引入任何新常量，单个弱线索天然不高于 medium，符合 spec/07 第 5 节「单个弱线索不应直接制造 high」。
- 维度分数按同一公式作用于该类别信号子集并封顶 [0,100]，级别同样由 LevelFromScore 得出；仅输出涉及维度（spec/04 第 3 节允许省略无信号维度），Findings 与 Dimensions 分别经 domain.SortFindings / domain.SortDimensions 固定排序。
- 确定性保证：输入先按与运行器一致的全局键规范化排序再累加浮点贡献分，重复求值与乱序输入结果 reflect.DeepEqual 一致；空输入输出零分、low、空 Findings、空维度且不阻塞合并。
- 新增 6 个测试：多类别正例（security/data/api/reliability/concurrency/supply_chain/testability 七类信号，断言 ID、维度分组与分数、severity、门禁）、空输入、非法证据五组反例（空文件路径、右侧零行号、无证据、超上限权重、零权重，均断言错误含规则 ID 与路径）、阈值边界 24/25/49/50/74/75 与 LevelFromScore 一致性、重复与乱序 DeepEqual 稳定、默认门禁恒 false 及 MitigationIDs 中性。

验证结果：

- `go test ./internal/policy -v` 通过（6 个测试）。
- `go test ./...` 通过（6 包 ok）。
- `go test -race ./...` 通过（6 包 ok）。
- `go vet ./...` 退出码 0。
- `gofmt -l .` 无输出（新增文件已格式化）。

已知限制：

- spec/02 未单独定义 finding 级 severity 映射与维度分数公式；当前实现以「单信号贡献分过同一 LevelFromScore 阈值」「总分公式作用于类别子集」作为已记录的解释口径。如需调整口径只需修改本包并同步测试。
- exposure_factor 固定 1.0、mitigation_credit 恒为 0：真实暴露度判定与缓解抵扣数值目录留待 Phase 4 调优切片补充。
- findings 数量上限（spec/03 第 6 节 20 条）与 degradation_reasons 属报告构建职责，本切片不做裁剪或降级记录。
- 显式多信号组合升级规则（权限扩大+不可信执行路径等）未实现，属 Phase 4 调优范围。

### C4 Slice：报告构建与渲染

状态：completed（2026-08-26）。C4 检查点（风险策略和报告构建）整体完成。

完成内容：

- 新增 `server/internal/report/build.go`：`BuilderInput` 显式入参（ReviewRequest、ChangeSummary、Findings、Dimensions、总分/级别、AnalyzerVersion、GeneratedAt、上游降级原因列表、Runtime 元数据）与纯函数 `Build`；另提供确定性缩进 JSON 渲染 `RenderJSON`。不读取全局状态或环境变量。
- 输入校验红线：request/change_summary/runtime 非法、analyzer_version 为空白、generated_at 为零值、总分越界或 overall_level 与分数不一致时立即返回错误；组装结果必须通过包内既有双重校验（领域 + 内置 risk-report/v1 schema）后才输出，绝不带病发布。
- findings 上限裁剪：上限取 spec/03 第 6 节明文规定的 20 条（与 schema `maxItems:20` 一致），非实现层默认值；超限时先经 domain.SortFindings 稳定排序再保留前 20 条（严重级别高者优先），并追加 code=`findings-truncated` 的显式降级原因（消息含截断前后数量与依据）；总分与维度统计仍基于全部线索，不被裁剪影响。空数组序列化为 `[]` 而非 null 以满足协议数组类型。
- 状态推导口径（已记录的实现层解释）：降级原因列表（含截断原因）非空 ⇒ degraded，否则 completed；保证不出现「completed 带降级原因」或「degraded 无原因」的矛盾形态，对应 spec/01 第 6 节状态模型。
- 新增 `server/internal/report/markdown.go`：`RenderMarkdown` 确定性渲染——段落遵循 spec/01 第 5.2 节推荐顺序（状态与级别→一句话结论→变更概览→维度表→发现→测试缺口建议→分析范围与降级原因→协议版本）；每条 Finding 输出标题、类别、严重度、来源、规则、证据位置（`文件:行号`+侧别，文件级只列路径）、影响与建议；表格单元格竖线转义为 `\|` 并折叠换行；时间固定为 UTC RFC3339 文本；渲染器只读不改语义，不引入新泄露面。
- 金样：新增 `server/internal/report/testdata/golden_report.md`，由 `GOLDEN_UPDATE=1` 显式刷新，普通模式逐字节比对（沿用 signals 包 runner 的既有模式）。
- 新增 11 个测试（build_test.go / markdown_test.go）：策略结果正例（断言排序、维度顺序与 70/high）、JSON 序列化往返 DeepEqual、空信号产出 completed/low 且数组非 nil、25 条超限截断（保留前缀等于独立排序副本前 20、状态 degraded、原因码与数量断言、总分不变）、8 组非法输入反例（缺请求身份、负统计、空白版本、零时间、越界分数、级别不一致、非法 runtime、inline_eligible 违规）、双次构建与乱序输入 DeepEqual、金样比对、逐字节稳定、必需段落断言、表格竖线转义、nil 报告拒绝。

验证结果：

- `go test ./internal/report -v` 通过（16 个测试，含既有 5 个校验测试）。
- `server/` 内 `go test ./...` 通过（6 包 ok）、`go test -race ./...` 通过（6 包 ok）、`go vet ./...` 退出码 0、`gofmt -l .` 无输出。

已知限制：

- 状态推导规则（有降级原因即 degraded）与 `findings-truncated` 原因码措辞为实现层口径，等待用户追认后如需调整仅影响本包及其调用方。
- Runtime 元数据无法由其他输入推导，作为显式入参传入；TestGaps 尚无生产者，当前报告恒为空列表。
- Markdown 尚未接入 Step Summary、Artifact 或评论发布链路（属后续切片）；无 CLI 入口。

## 已知限制

- 当前没有接入真实 GitHub API。
- 当前没有接入真实模型。
- 当前没有实现 PR 评论发布。
- 当前已实现 `CR-SEC-001`、`CR-API-001`、`CR-EXEC-001`、`CR-DATA-001`、`CR-REL-001`、`CR-CON-001`、`CR-SC-001`、`CR-SEC-002`、`CR-SEC-003` 和 `CR-TEST-001`，其余风险规则仍在设计/实现阶段。

## 阻塞事项

无。

## 下一步计划

### 下一功能：离线 CLI（切片 13；按 DEVELOPMENT_PLAN 第 7 节任务表顺序，先于 C5 Fake GitHub Client 实施）

目标：

- 新增 `server/cmd/go-risk-analyzer` CLI 入口：`go-risk-analyzer analyze --event <path> --diff <path> --output <path>` 在完全无网络条件下完成「事件解析 → diff 解析 → 信号运行 → 策略求值 → 报告构建与渲染」全链路，并把通过校验的 JSON 与 Markdown 报告写入 output 指定位置。
- 仅复用 internal/event、change、signals、policy、report 既有能力；不接 GitHub API、不接模型、不发布评论。

前置条件：

- 报告构建器与 Markdown/JSON 渲染器可离线产出通过双重校验的报告（本切片已完成）。
- 事件解析器（C1 前置切片）、unified diff 解析器（C2）、信号运行器与策略引擎均已就绪并有金样保障。

验收标准：

- 给定合法 event.json 与 change.patch，命令成功生成通过 `risk-report/v1` schema 校验的 risk-report JSON 和确定性 Markdown；空 diff 可产出合法 low 报告。
- 全程无网络访问；缺失参数、非法 JSON、畸形 diff 或不可写输出路径返回明确错误信息，不泄露原始 payload 内容。
- 相同输入重复执行产生一致的报告内容（generated_at 由显式输入或固定策略提供，具体口径在切片内说明）。
- 全量既有测试继续通过，CLI 层补充正例、反例与边界测试。
