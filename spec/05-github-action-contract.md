# 05. GitHub Action Contract

## 1. 触发事件

默认工作流：

```yaml
name: Change Risk Analysis

on:
  pull_request:
    types: [opened, synchronize, reopened]
  workflow_dispatch:
    inputs:
      pull_request_number:
        required: true
        type: number

permissions:
  contents: read
  pull-requests: write

jobs:
  analyze:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - name: Run change risk analyzer
        uses: example/change-risk-analyzer@<full-commit-sha>
        with:
          model-provider: ${{ vars.RISK_MODEL_PROVIDER }}
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          MODEL_API_KEY: ${{ secrets.MODEL_API_KEY }}
```

上面的 `uses` 地址只是契约示例，正式发布前必须替换为真实 Action，并固定到完整 commit SHA。

## 2. 事件处理

- `opened`、`reopened` 和 `synchronize` 进入同一分析流程。
- 以事件中的 Pull Request number、base SHA 和 head SHA 建立请求身份。
- 不以 `GITHUB_SHA` 单独代表 PR head；它可能代表合并引用。
- `workflow_dispatch` 必须显式传入 PR number，并重新从 GitHub API 验证当前 head SHA。
- 已关闭或已经合并的 PR 默认跳过写评论，但可以生成离线报告。
- 事件重复送达或人工重跑不能导致重复评论。

## 3. Token 权限

MVP 使用：

```yaml
permissions:
  contents: read
  pull-requests: write
```

用途：

- `contents: read`：读取仓库文件或规则上下文时使用，能不用则不用。
- `pull-requests: write`：创建或更新总评论。

明确不申请：

```yaml
contents: write
actions: write
checks: write
issues: write
administration: write
```

如果未来改为只发布 Artifact，可将 `pull-requests: write` 降为 `pull-requests: read`。新增权限必须通过决策记录说明。

## 4. Fork PR 策略

首版使用 `pull_request`，不使用 `pull_request_target`。

- Fork PR 使用只读且受 GitHub 保护的运行环境。
- 外部模型 Secret 不可用时，只执行确定性分析，或把模型阶段标记为 `skipped_external_provider`。
- 不允许为了获得 Secret 而 checkout 外部代码到特权工作流。
- 不允许执行 PR 中的任何脚本，即使该脚本只是为了获取语言或依赖信息。
- 公开仓库的双工作流发布方案属于后续阶段，必须单独设计并验证 artifact 信任边界。

## 5. 输入配置

建议支持的 Action inputs：

- `config`: 配置文件路径，默认 `.risk-analyzer.yml`。
- `model-provider`: `disabled`、`openai-compatible`、`anthropic-compatible` 或未来 Provider。
- `model-name`: Provider 使用的模型名。
- `max-files`：最大分析文件数。
- `max-patch-bytes`：最大 patch 字节数。
- `publish-comment`: 是否发布总评论，默认 `true`。
- `upload-report`: 是否上传 JSON/Markdown Artifact，默认 `true`。
- `fail-on`: `never`、`high`、`critical`，默认 `never`。
- `debug`: 是否输出非敏感调试元数据，默认 `false`。

Secret 只能通过环境变量传入：

- `GITHUB_TOKEN`。
- `MODEL_API_KEY`。

不允许把 Secret 作为命令行参数、配置文件内容或报告字段。

## 6. Artifact

每次分析产生：

- `risk-report.json`：机器可读完整报告。
- `risk-report.md`：人类可读报告。
- `run-metadata.json`：版本、耗时、输入计数和降级原因，不含原始代码和 Secret。

Artifact 名称：

```text
change-risk-report-pr-<number>-<head-sha-short>
```

保留期由仓库设置决定。默认不上传原始完整 patch、完整 Prompt 或完整模型上下文。

## 7. 评论幂等

Marker：

```html
<!-- change-risk-analyzer:v1 repo=<owner>/<repo> pr=<number> -->
```

发布算法：

1. 列出当前 PR 的 issue comments，分页读取。
2. 只匹配 marker 和机器人作者，不能按正文模糊搜索。
3. 找到一条则更新；找到多条则保留最新一条并记录重复状态，是否删除旧评论由后续策略决定。
4. 找不到则创建一条。
5. 更新失败时保留 Artifact，并将状态设为 `degraded`。

正文必须包含当前 head SHA，避免用户误把旧报告当成最新结论。

## 8. API 失败和限流

可重试：网络暂时错误、429、502、503、504。最多三次，采用指数退避并尊重 `Retry-After`。

不可重试：401、403、404、422，除非是明确的分页或请求参数错误。

错误策略：

- 无法读取 PR 身份：`failed`，不发布不完整结论。
- 无法读取部分文件：`degraded`，报告未读取的范围。
- 无法写评论：Artifact 成功，状态 `degraded`。
- 模型失败：保留确定性报告，状态 `degraded` 或 `skipped`。

## 9. 输出和门禁

MVP 始终以零退出码生成报告，除非输入或程序本身无法安全运行。`fail-on` 默认 `never`，后续门禁必须依赖完整报告和合法证据。

Action 输出建议：

- `report-path`
- `report-status`
- `risk-level`
- `risk-score`
- `finding-count`
- `degradation-count`
- `comment-url`

这些输出不能取代 Artifact 中的完整报告。