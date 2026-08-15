# 04. Domain Model

## 1. 领域对象

### 1.1 `ReviewRequest`

表示一次可复现的分析请求：

```text
ReviewRequest {
  repository: RepositoryRef
  pull_request_number: int
  event_action: string
  base_sha: string
  head_sha: string
  source_kind: same_repository | fork | unknown
  workflow_run_id: string
  requested_at: timestamp
}
```

约束：

- `head_sha` 是幂等和报告身份的一部分。
- 缺少 `base_sha` 或 `head_sha` 时不能进入发布阶段。
- `source_kind` 只影响权限和模型策略，不改变代码事实解析。

### 1.2 `ChangedFile`

```text
ChangedFile {
  old_path: string?
  new_path: string
  status: added | modified | deleted | renamed | copied | unknown
  language: string?
  additions: int
  deletions: int
  changes: int
  patch: string?
  is_binary: bool
  patch_truncated: bool
}
```

离线 diff 解析器将 `patch` 规范化为单文件 unified patch，通常从 `---`/`+++` 文件头开始；二进制文件和没有 hunk 的文件使用 `null` 表示无 patch。超过单文件或总 patch 预算时保留确定性增删统计，只裁剪 patch 内容并设置 `patch_truncated`/`ChangeSet.truncated` 及原因。

### 1.3 `ChangeSet`

```text
ChangeSet {
  files: []ChangedFile
  total_files: int
  total_additions: int
  total_deletions: int
  base_sha: string
  head_sha: string
  truncated: bool
  truncation_reasons: []string
}
```

### 1.4 `Evidence`

```text
Evidence {
  file: string
  start_line: int?
  end_line: int?
  side: left | right | file
  excerpt: string?
  fact: string
}
```

`excerpt` 可选且必须经过长度限制和脱敏。行号不可伪造：如果无法从 patch 确定行号，使用 `side=file`，不能填 0 作为假行号。

### 1.5 `RiskSignal`

```text
RiskSignal {
  rule_id: string
  category: RiskCategory
  fact: string
  evidence: []Evidence
  source: deterministic | model
  confidence: float
  weight: int
  mitigation_ids: []string
}
```

`confidence` 范围为 0 到 1。模型产生的 signal 必须先经过候选校验，不能绕过 `Evidence` 验证。

### 1.6 `Finding`

```text
Finding {
  id: string
  category: RiskCategory
  severity: low | medium | high | critical
  evidence_status: confirmed | needs_review
  confidence: float
  title: string
  impact: string
  evidence: []Evidence
  recommendation: string
  rule_ids: []string
  source: deterministic | model | combined
  inline_eligible: bool
}
```

`id` 由规则、路径、行范围和稳定事实摘要生成，不能使用随机 UUID，否则重跑无法去重。

### 1.7 `RiskReport`

报告是发布前冻结的不可变对象：

```text
RiskReport {
  schema_version: string
  status: completed | degraded | skipped | failed
  generated_at: timestamp
  analyzer_version: string
  request: ReviewRequest
  change_summary: ChangeSummary
  overall_score: int
  overall_level: low | medium | high | critical
  dimensions: []RiskDimension
  findings: []Finding
  test_gaps: []TestGap
  degradation_reasons: []DegradationReason
  runtime: RuntimeMetadata
}
```

## 2. `RiskCategory`

固定枚举：

```text
security | data | api | reliability | concurrency | performance |
delivery | supply_chain | testability
```

## 3. `RiskDimension`

```text
RiskDimension {
  category: RiskCategory
  score: int
  level: low | medium | high | critical
  signal_count: int
  summary: string
}
```

没有信号的维度也可以省略；报告必须保持维度排序稳定，便于 golden test。

## 4. `TestGap`

```text
TestGap {
  area: string
  reason: string
  recommended_test: string
  evidence: []Evidence
  priority: low | medium | high
}
```

测试缺口是建议，不等于断言测试一定缺失。没有读取测试上下文时应使用谨慎措辞。

## 5. `RuntimeMetadata`

```text
RuntimeMetadata {
  duration_ms: int
  files_seen: int
  files_analyzed: int
  patch_bytes_seen: int
  context_truncated: bool
  model_provider: string?
  model_name: string?
  prompt_version: string?
  token_input: int?
  token_output: int?
}
```

禁止包含 API Key、完整 Prompt、完整代码、GitHub Token 或原始 Secret。

## 6. 不变式

- `overall_score` 在 0 到 100 之间。
- `overall_level` 必须由 `overall_score` 和当前策略计算，不能由 Provider 直接指定。
- `high` 和 `critical` finding 必须有至少一个有效 Evidence。
- `inline_eligible=true` 时必须有右侧文件和正数行范围。
- `findings` 按严重级别、路径、行号和 ID 稳定排序。
- `status=degraded` 或 `skipped` 时必须至少有一个 `degradation_reasons`。
- `head_sha` 必须存在于报告中，评论 marker 和 Artifact 名称必须使用它。
