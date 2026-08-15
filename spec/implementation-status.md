# Implementation Status

## 当前状态

- 项目阶段：Phase 1 - 离线确定性内核
- 当前检查点：C3
- 当前功能：`CR-API-001` exported API change
- 总体状态：in_progress（`CR-API-001` completed，C3 remaining）
- 最后更新：2026-08-15

## 检查点列表

| 检查点 | 功能                                   | 状态        |
| ------ | -------------------------------------- | ----------- |
| C0     | 设计文档、agents.md、Schema 和 Fixture | completed   |
| C1     | 领域对象和报告协议                     | completed   |
| C2     | unified diff 解析                      | completed   |
| C3     | 确定性风险规则                         | in_progress |
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

## 已知限制

- 当前没有接入真实 GitHub API。
- 当前没有接入真实模型。
- 当前没有实现 PR 评论发布。
- 当前已实现 `CR-SEC-001` 和 `CR-API-001`，其余风险规则仍在设计/实现阶段。

## 阻塞事项

无。

## 下一步计划

### C3 下一功能：`CR-EXEC-001` untrusted command execution signal

目标：

- 在 `internal/signals` 增加外部输入流向命令执行入口的确定性线索。
- 只处理 C2 提供的新增/删除 patch 行，输出稳定 rule ID 和 Evidence。
- 保持模型、策略评分和报告构建不变。

前置条件：

- C1 领域对象、C2 diff parser、`CR-SEC-001` 和 `CR-API-001` 完成。
- 命令执行线索的 Evidence 语义明确且不需要扩展 RiskReport schema。

验收标准：

- 提供命令执行线索的正例、反例、边界例和误报说明。
- signal 通过领域校验，路径和行号来自 C2 解析结果。
- 同一 ChangeSet 重复分析产生稳定、去重后的 signal。
