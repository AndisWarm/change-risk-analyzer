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
