# 07. Deterministic Analyzers

## 1. 目标

确定性分析器负责回答“变更事实是什么”和“哪些风险线索值得进一步确认”。它不试图独立完成所有语义判断，也不依赖模型。

## 2. 分析器接口

```text
Analyzer.Analyze(ctx, ChangeSet) ([]RiskSignal, error)
```

规则必须：

- 输入明确。
- 输出稳定排序。
- 不读全局状态。
- 不调用网络。
- 不执行仓库代码。
- 每条 signal 带规则 ID、事实和证据。
- 可以单独测试并在 fixture 中复现。

## 3. 分析阶段

### 3.1 变更事实

- 文件数量、语言、状态、增删行数。
- 重命名、删除、二进制和 patch 截断。
- workflow、Dockerfile、部署配置、依赖清单和迁移文件变化。
- 公共 API、路由、配置键和导出符号的变化。

### 3.2 通用风险线索

- 权限块增加写权限。
- 外部输入流向命令、SQL、模板或文件路径。
- Secret 模式进入源代码、日志或测试输出。
- 删除或覆盖数据的操作。
- 依赖、Action 或镜像从固定版本变为浮动版本。
- 外部网络请求缺少超时或取消。
- 错误被忽略、资源未关闭、重试无界。

### 3.3 Go 风险线索

首版只做低误报的词法和结构线索，不声称替代 `go vet`、`staticcheck`、`gosec` 或编译器：

- 新增 goroutine 但没有可见生命周期或取消路径。
- channel 创建后缺少明显的关闭/消费路径，标为候选而不是确定泄漏。
- `http.Client` 或外部请求创建未见 timeout/context。
- `defer` 资源释放位置可能晚于错误返回。
- `sync.Mutex`、共享 map 或状态对象在新增并发路径中出现。
- `os/exec`、文件路径、SQL 拼接和反序列化入口发生变化。
- 导出函数或接口签名变化但没有对应测试变化。

所有 Go 规则必须在文档中标注“线索”还是“确认事实”，避免把文本匹配当成语义证明。

## 4. 规则结构

建议每条规则有稳定 ID：

```text
CR-SEC-001  workflow write permission
CR-SEC-002  secret-like literal
CR-SEC-003  authorization boundary change
CR-EXEC-001 untrusted command execution signal
CR-DATA-001 destructive migration
CR-API-001 exported API change
CR-REL-001 external request without timeout
CR-CON-001 new goroutine lifecycle signal
CR-SC-001 floating action/dependency reference
CR-TEST-001 risk-bearing change without test evidence
```

规则元数据：

```text
Rule {
  id: string
  category: RiskCategory
  description: string
  default_weight: int
  evidence_mode: line | file | summary
  false_positive_notes: string
  remediation_hint: string
}
```

### 4.1 已实现规则：`CR-SEC-001`

`CR-SEC-001`（Workflow write permission）是确定性线索规则，输入为 C2 规范化后的 `.github/workflows/*.yml` 或 `.yaml` 文件 patch。

- 命中：新增 `contents/actions/checks/deployments/discussions/id-token/issues/packages/pull-requests/security-events/statuses: write`、`permissions: write-all`，或 inline `permissions` 中的上述写权限。
- 证据：只定位到 patch 中新增行的 `side=right` 和正数行号；每个 Workflow 文件合并为一条 signal。
- 输出：`category=security`、`source=deterministic`、`confidence=1`、默认 `weight=30`，不直接产生 Finding、总分或门禁结果。
- 不命中：`read`/`read-all`、普通配置文件、二进制/无 patch 文件，以及注释中的权限文本。
- 误报边界：规则只把明确的 GitHub 权限键视为候选，不对任意 YAML 键的 `write` 值报警；后续需要结合 Workflow 上下文和策略层确认影响。

### 4.2 已实现规则：`CR-API-001`

`CR-API-001`（exported API change）是面向 Go 源文件的确定性线索规则，输入为 C2 规范化后的非 `internal/`、非 `_test.go` patch。

- 命中：导出函数、类型、变量或常量被删除，或同一导出声明在删除行和新增行之间发生签名替换。
- 证据：删除使用 `side=left`，替换同时保留删除侧和新增侧的正数行号；同一文件内按声明合并 signal。
- 输出：`category=api`、`source=deterministic`、`confidence=0.9`、默认 `weight=25`。
- 不命中：只新增的兼容性 API、未导出符号、`internal/`/`vendor/`/测试文件，以及注释或字符串中的声明文本。
- 误报边界：规则只做词法和结构线索，不判断消费者数量、语义兼容性或路由协议；策略层需要结合测试和其他信号确认严重程度。

### 4.3 已实现规则：`CR-EXEC-001`

`CR-EXEC-001`（untrusted command execution signal）是面向 Go 源文件新增 patch 行的确定性候选规则。

- 命中：新增 `exec.Command`/`exec.CommandContext` 调用包含非字面量参数、字符串拼接、`fmt.Sprintf` 或动态命令名；shell `-c`、`/c` 和 `-Command` 调用同样纳入。
- 证据：定位到新增调用行的 `side=right` 和正数行号。
- 输出：`category=security`、`source=deterministic`、`confidence=0.75`、默认 `weight=30`；事实表述明确为“需确认是否受外部输入影响”。
- 不命中：全为字面量的命令、注释/字符串中的文本、删除侧调用、非 Go/测试/二进制/无 patch 文件。
- 误报边界：词法规则不证明参数来自外部输入，也不处理跨行命令调用；后续策略或上下文阶段需要进一步确认来源和可达性。

### 4.4 已实现规则：`CR-DATA-001`

`CR-DATA-001`（destructive migration）是面向迁移目录 `.sql` 文件新增 patch 行的确定性线索规则。

- 命中：新增 `DROP TABLE`、`DROP COLUMN`、`TRUNCATE` 或无界 `DELETE`/`WHERE 1=1` 操作。
- 证据：定位到新增 SQL 操作行的 `side=right` 和正数行号。
- 输出：`category=data`、`source=deterministic`、`confidence=0.85`、默认 `weight=35`。
- 不命中：`ADD/ALTER` 等非破坏性操作、有明确条件的 `DELETE`、删除侧旧操作、注释/字符串和非迁移 SQL 文件。
- 误报边界：规则不判断回滚脚本、备份、双写或数据范围；策略层需要结合迁移上下文确认是否可逆及影响面。

### 4.5 已实现规则：`CR-REL-001`

`CR-REL-001`（external request without timeout）是面向新增 Go patch 行的确定性可靠性线索规则。

- 命中：`http.Get`、`http.Head`、`http.Post`、`http.PostForm`、无可见 context 的 `http.DefaultClient.Do`，或未设置 `Timeout` 且无可见 context 的内联 `http.Client{...}.Do`。
- 证据：定位到请求调用的新增 `side=right` 和正数行号；每个调用生成一条 signal。
- 输出：`category=reliability`、`source=deterministic`、`confidence=0.8`、默认 `weight=20`；事实表述为需要确认超时、取消和故障恢复策略。
- 不命中：调用参数中可见 `WithContext`、`NewRequestWithContext`、`WithTimeout` 或 `WithDeadline`，内联 `http.Client` 的可见 `Timeout` 字段，普通命名 client、注释、字符串、删除侧、测试文件、二进制或无 patch 文件。
- 误报边界：这是单行词法线索，不判断命名 client 的真实配置，不跟踪跨行 request/context 传播，也不证明请求处在服务入口或高并发路径；策略层需要结合上下文决定严重程度。

### 4.6 已实现规则：`CR-CON-001`

`CR-CON-001`（new goroutine lifecycle signal）是面向新增 Go patch 行的确定性并发线索规则。

- 命中：新增 `go worker()`、`go receiver.Run()` 或匿名 `go func`，且调用行或新增匿名闭包中未见 `ctx`、停止通道、`cancel` 或 `WaitGroup.Done` 生命周期线索。
- 证据：定位到启动 goroutine 的新增 `side=right` 和正数行号；每个启动点生成一条 signal。
- 输出：`category=concurrency`、`source=deterministic`、`confidence=0.65`、默认 `weight=20`；事实表述为需要确认退出条件。
- 不命中：调用参数或匿名闭包中可见 `ctx`、`done`、`stop`、`quit`、`cancel`、`wg`/`waitGroup`，以及注释、字符串、删除侧、测试文件、二进制或无 patch 文件。
- 误报边界：这是有限范围词法线索，不证明 goroutine 必然泄漏，也不分析被调用函数、跨函数取消传播、channel 生产/消费、真实 WaitGroup 配对或超过短闭包范围的逻辑；策略层需要结合上下文确认影响。

### 4.7 已实现规则：`CR-SC-001`

`CR-SC-001`（floating action/dependency reference）是面向新增 patch 行的确定性供应链线索规则，输入为 C2 规范化后的 Workflow YAML、其他 `.yml`/`.yaml` 配置、`Dockerfile*` 以及 shell 脚本与 Makefile 类文件。

- 命中：
  - GitHub Action 引用使用分支/浮动标签（`@main`、`@master`、`@latest` 等）或可移动大版本标签（`@v1`、`@v1.2`）；
  - 容器镜像使用 `latest` 标签或未指定标签（隐式 latest）；Dockerfile 中无标签的裸单词名可能是构建阶段引用，跳过以避免误报；
  - `go get` / `go install` 命令引用 `@latest` 或 `@master`/`@main` 分支版本。
- 固定判定：完整 40/64 位 commit SHA、`@sha256:`/`@sha512:` digest 摘要、三段式精确版本（如 `v1.2.3`）、显式非 latest 标签。
- 证据：定位到新增行的 `side=right` 和正数行号；同一文件合并为一条 signal。
- 输出：`category=supply_chain`、`source=deterministic`、`confidence=0.8`、默认 `weight=20`；事实表述为需要确认是否应钉死到精确版本。
- 不命中：上述固定引用形态、本地路径 action（无 `@ref`）、纯注释行与行内注释、删除侧旧内容、二进制/无 patch 文件和无关路径（含路径穿越输入）。
- 误报边界：这是词法线索，不识别 `releases/*` 等未列举的浮动形态，不判断依赖的真实可达性或是否已被替换，也不分析跨文件的引用复现；YAML 中无标签裸单词镜像按真实镜像处理，若为部署工具的自建别名会产生候选误报；策略层需要结合上下文确认严重程度。

### 4.8 已实现规则：`CR-SEC-002`

`CR-SEC-002`（secret-like literal）是面向所有带 patch 的非二进制文件新增行的确定性疑似密钥线索规则，扫描前执行路径规范化并拒绝路径穿越输入。

- 命中：
  - 已知令牌格式：GitHub Token（`ghp_`/`gho_`/`ghu_`/`ghs_`/`ghr_` 前缀）、AWS Access Key ID（`AKIA`/`ASIA` 开头）、Slack Token（`xoxb-` 等）、Google API Key（`AIza` 开头）；
  - 私钥块起始标记 `-----BEGIN ... PRIVATE KEY-----`；
  - 密钥类键名（`password`、`passwd`、`secret`、`token`、`api_key`、`access_key`、`access_token`、`private_key`、`auth_token`，允许作为更长变量名尾部）后紧跟非空字面量赋值。
- 脱敏不变量：Evidence 不设置 Excerpt；Fact 只包含类型、键名与行号描述，任何字段都不得出现完整原始密钥值（有专门测试断言）。
- 豁免：各家公开文档示例假值（如 `AKIAIOSFODNN7EXAMPLE`）、占位符形态（`<...>`、`${}`、`your_*`、`changeme`、`xxx*`、`placeholder` 等）、环境引用（`os.Getenv`、`process.env`）、短于 6 字符的值、字符种类不超过 2 的值、纯数字与纯字母值、整行与行内注释、删除侧旧内容、二进制和无 patch 文件。
- 取舍说明：不引入熵计算以控制噪声；要求赋值字面量至少含一个数字或符号——纯字母口令会漏报，这是刻意的低误报取舍。
- 证据：定位到新增行的 `side=right` 和正数行号；同一文件合并为一条 signal，行级按行号去重排序。
- 输出：`category=security`、`source=deterministic`、`confidence=0.85`、默认 `weight=30`；不直接产生 Finding、总分或门禁结果。confidence 高于 `CR-EXEC-001`（0.75）是因为令牌格式证据结构性强，低于 `CR-SEC-001`（1.0）是因为词法启发式无法验证凭据真实有效。
- 误报边界：词法规则不验证密钥是否真实有效，也不识别未列举的自定义令牌格式；含空格的口令只截取首段，可能整体漏报；候选需要策略层与人工确认后再处理，发现真实泄露时应旋转凭据。

### 4.9 已实现规则：`CR-SEC-003`

`CR-SEC-003`（authorization boundary change）是面向 Go 源文件（排除 `_test.go` 与 `vendor/`）patch 的确定性安全候选规则，同时检查删除侧与新增侧行。

- 命中：
  - 删除侧（`side=left`）：被删除行包含授权校验或中间件关键词（authorize/authenticate/check_auth/require_auth/require_permission/verify_token/is_admin/has_role/enforce_policy 等，大小写不敏感），或形如 `.Use(...auth...)` 的中间件注册；事实措辞为「删除了疑似授权校验或中间件代码，需确认是否有替代防护」。
  - 新增侧（`side=right`）：出现显式跳过或放宽鉴权的独立标识符——skip_auth/disable_auth/allow_all/permit_all/no_auth/without_auth/anonymous_access/public_route 等（允许 `-`/`_` 变体）；事实措辞为「疑似显式跳过或放宽鉴权，需确认意图」。
- 不命中：新增的鉴权强化代码（新加中间件注册、新加权限判断）；注释与字符串字面量中的命中（复用 `goCodeLine` 脱敏处理）；测试文件、`vendor/`、二进制、无 patch 文件与路径穿越输入；非独立单词的子串（如 `SetAllowAll` 中的 allowall 不命中）。
- 证据：删除侧定位到 `side=left` 正数行号，新增侧为 `side=right`；同一文件合并为一条 signal，按行号与侧别稳定排序去重。
- 输出：`category=security`、`source=deterministic`、`confidence=0.7`、默认 `weight=25`；不直接产生 Finding、总分或门禁结果。
- 误报边界：词法规则不构建调用图，无法判断被删代码是否有等价替代防护，也不识别自定义命名风格的守卫函数；重构改名场景可能同时产生删除与新增两类候选；仅覆盖 Go 语言文件；策略层需要结合上下文确认是否真的放宽了授权。

### 4.10 已实现规则：`CR-TEST-001`

`CR-TEST-001`（risk-bearing change without test evidence）是面向所有非二进制文件变更的弱线索规则，提醒「敏感路径的变更未观察到配套测试类变更」。

- 敏感路径定义：任一路径段包含 `migration`/`auth`/`payment`/`billing`/`security`/`admin`（大小写不敏感子串匹配；目录名与文件名都参与，因此 `author.go` 这类含 auth 子串的名字也会命中，属于已文档化的保守取舍）。
- 测试证据判定：同一 ChangeSet 中存在 `_test.go` 文件或位于 `tests/`/`test`/`testdata` 目录的变更，且与敏感文件共享同一段顶级目录（粗粒度前缀匹配，宁可漏报不误报），即视为已覆盖。
- 输出：每个未覆盖的敏感文件一条 `side=file` Evidence（Fact 为「未观察到 X 的配套测试变更，建议补充验证」）；按顶级目录合并为一条 signal，组间按目录名稳定排序；删除的敏感文件（无 patch）同样纳入。
- 措辞红线：只允许观察式表述（「未观察到」），不得断言「没有测试」。
- 不命中：已有配套测试证据的敏感变更；非敏感路径；二进制文件；测试类文件自身的变化；路径穿越输入。
- 输出：`category=testability`、`source=deterministic`、`confidence=0.6`、默认 `weight=15`；不直接产生 Finding、总分或门禁结果。
- 误报边界：关键词子串匹配会把非敏感文件误标为敏感；顶级目录粗匹配可能把无关测试变更当作覆盖证据造成漏报；规则不理解测试是否真的覆盖被改行为；策略层与人工复核决定最终处理。

## 5. 规则组合

单个弱线索不应直接制造 `high`：

```text
弱线索 + 公共入口 + 无测试证据 → medium/high candidate
权限扩大 + 不可信输入执行路径 → high/critical candidate
删除迁移 + 无回滚/兼容信号 → high candidate
```

组合规则必须显式记录原因和参与的 `rule_ids`，不能在 Prompt 中隐式完成。

## 6. 规则顺序和去重

- 先提取事实，再运行维度规则，再运行组合规则。
- 相同规则、路径和行范围的 signal 合并。
- findings 根据稳定 ID 去重。
- 规则输出按 `category`、路径、行号和 rule ID 排序。
- 不把 linter 的所有输出复制到报告；只把与变更风险有关系的信号纳入。

### 6.1 已实现：信号运行器

`server/internal/signals/runner.go` 提供统一运行器：按注册顺序执行全部分析器，聚合、去重并稳定排序输出。

- 默认注册顺序与第 4 节规则顺序一致：CR-SEC-001 → CR-API-001 → CR-EXEC-001 → CR-DATA-001 → CR-REL-001 → CR-CON-001 → CR-SC-001 → CR-SEC-002 → CR-SEC-003 → CR-TEST-001。
- 去重键：RuleID + Fact + 全部 Evidence 的（文件、起止行、侧别、事实）签名，完全相同的信号只保留一条。
- 全局排序键：类别固定维度顺序 → 首个证据文件路径 → 起始行 → 侧别 → 规则 ID → 事实。
- 错误传播：任一分析器失败或上下文取消时立即返回携带其规则 ID 的错误，后续分析器不再执行；不静默吞错。
- fixture：`server/internal/signals/testdata/golden_runner.json` 固化多规则聚合输出的 JSON 快照（覆盖全部十条规则的代表场景），由 `GOLDEN_UPDATE=1` 显式刷新；`spec/fixtures/cases.json` 保持面向未来端到端报告级评测的既有定位，不在信号层耦合。

## 7. 外部静态工具边界

后续可以消费用户显式提供的静态工具 Artifact，例如 `go vet`、`staticcheck`、`gosec` 或 CodeQL 结果，但首版不运行 PR 代码。

外部结果必须：

- 明确来源和版本。
- 绑定 commit SHA。
- 通过统一 finding 适配器转换。
- 不覆盖确定性策略。
- 对路径和行号再次校验。

## 8. 规则验收

每条规则至少提供：

- 一个命中正例。
- 一个不应命中的反例。
- 一个边界或不确定例。
- 期望 evidence。
- 期望 severity 上限。
- 误报原因和抑制方式。

规则宁可少而可靠，不要用大量关键词制造噪声。
