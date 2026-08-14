# 09. Evaluation

## 1. 评测目标

评测重点不是“模型回答像不像人”，而是：

- 风险判断是否有证据。
- 高严重度结果是否可信。
- 是否漏掉关键变更风险。
- 是否产生过多噪声。
- 行号、路径、head SHA 是否正确。
- 失败、超限、Fork 和重复运行是否安全。

## 2. Fixture 组织

固定样例定义在 `spec/fixtures/cases.json`。每个 case 至少包含：

- `id`：稳定名称。
- `scenario`：场景说明。
- `input`：事件、文件和 patch 的引用。
- `expected_status`。
- `expected_level_max`：允许的最高风险级别，避免低风险样例被误报为高危。
- `expected_rules`：必须命中或允许命中的规则。
- `forbidden_rules`：不应命中的规则。
- `security_notes`：该样例的安全关注点。

建议覆盖：

- 文档或注释修改。
- 普通业务逻辑修改。
- 公共 API 变化。
- 认证和授权变化。
- 数据库迁移和删除。
- Workflow 权限扩大。
- `go.mod`、Action 或镜像依赖升级。
- goroutine、channel、超时和资源释放。
- 文件重命名、删除、二进制、无 patch。
- 超大 patch 和上下文截断。
- PR 标题、正文和代码注释 Prompt 注入。
- Fork PR 无模型 Secret。

## 3. 测试层级

### 3.1 单元测试

覆盖：

- event parser。
- diff parser 和行号映射。
- 文件状态和语言识别。
- 各确定性规则。
- 信号去重和稳定排序。
- 评分、缓解项和级别阈值。
- JSON Schema 校验。
- Markdown 转义和评论 marker。

### 3.2 Golden tests

同一 fixture 输入应生成稳定的确定性报告。以下字段需要归一化或排除时间差异：

- `generated_at`。
- 运行耗时。
- Provider request ID。

以下字段必须稳定：

- `head_sha`。
- 变更统计。
- 规则 ID。
- evidence 路径和行号。
- score、level 和 finding ID。
- degradation reason。

### 3.3 API 集成测试

使用 fake GitHub server，覆盖：

- 多页文件列表。
- 多页评论列表。
- 429 和 `Retry-After`。
- 502/503/504。
- 401/403/404/422。
- 空 patch 和二进制文件。
- 评论创建、更新和重复 marker。
- head SHA 变化导致旧结果拒绝发布。

### 3.4 Provider 集成测试

使用 fake Provider，覆盖：

- 合法 JSON。
- 缺字段 JSON。
- 行号越界。
- 不存在文件路径。
- 超过 finding 数量。
- Prompt 注入文本。
- 超时、429、无效 JSON 和一次修复失败。

不在自动化测试中调用真实模型 API。

## 4. 质量指标

### 高风险精确率

```text
precision_high = confirmed_high_findings / all_high_findings
```

### 风险召回率

```text
recall = detected_expected_findings / all_expected_findings
```

### 证据有效率

```text
evidence_validity = findings_with_valid_location / findings_with_evidence
```

### 行级命中率

```text
inline_accuracy = correctly_located_lines / all_inline_candidates
```

### 噪声率

```text
noise_rate = rejected_or_false_positive_findings / all_findings
```

### 运行指标

- p50/p95 总耗时。
- GitHub API 请求数。
- 模型输入和输出 token。
- 单 PR 成本估算。
- 降级率。
- 评论发布成功率。

## 5. 初始门槛

在没有足够真实数据前，只把它们当作起始目标：

- JSON Schema 合法率：100%。
- 幂等评论重复率：0%。
- evidence 路径有效率：100%。
- 高/严重 finding 的固定评测集精确率：至少 0.80 后，才允许尝试门禁。
- 模型不可用时确定性报告成功率：100%。
- 恶意输入不会触发命令执行或 Secret 输出。

门槛必须按语言、风险维度和规则版本分层统计，不能用总体平均数隐藏某个规则的高误报。

## 6. 人工复核流程

每个版本抽取一批真实但已脱敏的 PR：

1. 两名开发者独立标记风险事实、严重程度和证据。
2. 记录分歧和规则误报原因。
3. 更新 golden fixture 或规则抑制说明。
4. 重新运行回归测试。
5. 在决策记录中说明是否调整阈值或门禁。

## 7. 验收矩阵

| 能力 | 必须验证 |
| --- | --- |
| 变更采集 | SHA、分页、状态、patch、重命名 |
| 风险判断 | 规则、证据、分数、稳定排序 |
| AI 边界 | schema、行号、注入、无效 JSON、降级 |
| GitHub 发布 | marker、更新、权限失败、SHA 保护 |
| 安全 | 无执行、无 Secret 泄露、最小权限、Action 固定 SHA |
| 资源限制 | 文件、patch、上下文、评论和重试上限 |
| 可维护性 | 文档同步、fixture、决策记录、静态检查 |