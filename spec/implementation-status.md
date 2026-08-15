# Implementation Status

## 当前状态

- 项目阶段：Phase 1 - 离线确定性内核
- 当前检查点：C1
- 当前功能：领域对象和报告协议
- 总体状态：in_progress（C1 completed，等待进入 C2）
- 最后更新：2026-08-15

## 检查点列表

| 检查点 | 功能                                   | 状态        |
| ------ | -------------------------------------- | ----------- |
| C0     | 设计文档、agents.md、Schema 和 Fixture | completed   |
| C1     | 领域对象和报告协议                     | completed   |
| C2     | unified diff 解析                      | pending     |
| C3     | 确定性风险规则                         | pending     |
| C4     | 风险策略和报告构建                     | pending     |
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

## 已知限制

- 当前没有接入真实 GitHub API。
- 当前没有接入真实模型。
- 当前没有实现 PR 评论发布。
- 当前风险规则仍然是设计阶段。

## 阻塞事项

无。

## 下一步计划

### C2：unified diff 解析

目标：

- 解析 added、modified、deleted、renamed 文件。
- 获取增删行数。
- 处理无 patch 和二进制文件。
- 建立新增行号映射。
- 对超大 patch 产生 `patch_truncated` 状态。

前置条件：

- C1 领域对象完成。
- `ChangedFile` 和 `ChangeSet` 的字段冻结。

验收标准：

- 通过低风险、重命名、删除、二进制和超大 patch Fixture。
- 行号映射测试通过。
- 同一 patch 重复解析产生稳定结果。
