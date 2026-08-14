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