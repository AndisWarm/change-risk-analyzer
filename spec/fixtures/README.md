# Evaluation fixtures

本目录保存不依赖真实 GitHub 和真实模型的固定评测输入。实现阶段可将每个 case 展开为事件 JSON、changed files JSON、patch 和期望报告。

## 推荐目录布局

```text
fixtures/
  cases.json
  low-doc-change/
    event.json
    files.json
    patch.diff
    expected.json
  high-auth-change/
    event.json
    files.json
    patch.diff
    expected.json
  prompt-injection/
    event.json
    files.json
    patch.diff
    expected.json
```

## Fixture 约束

- 输入不包含真实仓库 Token、API Key、私有源码或个人信息。
- `head_sha` 使用固定的十六进制测试值。
- expected report 忽略 `generated_at`、耗时和 Provider request ID。
- expected report 必须校验状态、规则 ID、证据路径/行号、风险级别上限、禁止规则和降级原因。
- 每个新增规则必须至少增加一个正例和一个反例。
- Prompt 注入样例只验证“被当作不可信数据”，不验证模型是否拥有真实工具权限。