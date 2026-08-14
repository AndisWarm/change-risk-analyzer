# 10. Roadmap

## Phase 0：设计和协议冻结

交付：

- `agents.md`。
- `spec/` 文档树。
- `risk-report.schema.json`。
- fixture 清单和决策记录。

完成条件：领域对象、输出协议、安全边界和默认门禁没有未决冲突。

## Phase 1：离线确定性内核

实现：

- `ReviewRequest`、`ChangeSet`、`RiskSignal`、`Finding`、`RiskReport`。
- 本地事件和 patch 输入。
- diff 解析、文件分类、限制和稳定排序。
- 初始确定性规则。
- JSON/Markdown 报告。

不实现：GitHub 写操作、真实模型、PR 代码执行。

完成条件：fixture 和 golden tests 通过；报告满足 schema。

## Phase 2：GitHub Action 只读接入

实现：

- `pull_request` 触发。
- GitHub PR 元数据和 changed files API。
- 分页、超时、重试和错误映射。
- JSON/Markdown Artifact 和 Step Summary。

阶段策略：先不发布 PR 评论，先验证读取和报告正确性。

完成条件：fake GitHub server 和测试仓库验证通过。

## Phase 3：幂等评论和模型适配

实现：

- 总评论 marker、创建和更新。
- Fake Provider。
- 一个真实 Provider 适配器。
- JSON 校验、一次修复和降级。
- 上下文预算、脱敏和 Prompt 注入防护。

完成条件：模型失败不丢失确定性报告；评论重跑不重复。

## Phase 4：风险信号增强和可选门禁

实现：

- Go 并发、外部请求、资源释放和公共 API 规则。
- 依赖、Workflow、迁移和部署变更规则。
- 行级评论候选。
- `fail-on` 配置，但默认仍关闭。

完成条件：高/严重风险精确率达到评测门槛，且门禁行为可解释。

## Phase 5：Fork 安全和平台扩展

实现前提：完成单独的威胁模型。

- 非特权分析 + 特权发布双工作流。
- Artifact 来源、schema、PR number 和 head SHA 验证。
- GitHub App 或其他 Git 平台适配器。

禁止：直接把首版改成 `pull_request_target` 并 checkout 外部代码。

## Phase 6：评估是否需要常驻服务

只有出现以下需求时才考虑 App、数据库和队列：

- 需要跨仓库安装和组织级配置。
- 需要保存历史风险和趋势。
- 需要异步排队处理大规模 PR。
- 需要统一模型预算和租户计费。

在此之前保持 Action + Go CLI 的简单架构。