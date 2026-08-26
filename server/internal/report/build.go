// build.go 实现报告构建器：把显式传入的请求身份、变更摘要、策略引擎
// 结果与运行元数据组装为通过双重校验（领域 + risk-report/v1 JSON Schema）
// 的不可变 RiskReport。
//
// 构建器是纯函数：不读取全局状态、环境变量或文件系统；所有输入显式传参，
// 输出在发布前冻结，渲染器只读不写。
//
// 与规格的对应关系：
//   - findings 数量上限（spec/03-architecture.md 第 6 节「最大 findings：
//     20」+ risk-report.schema.json maxItems:20）：超过上限时按
//     domain.SortFindings 既有稳定排序保留前 20 条（严重级别高者优先），
//     并追加一条 code=findings-truncated 的显式降级原因；spec/03 同节要求
//     「超过上限必须输出显式降级原因，不得静默丢弃关键事实」。总分与维度
//     统计仍基于全部线索（由策略引擎计算），裁剪只影响报告列出的清单。
//   - 状态推导（spec/01-product-requirements.md 第 6 节状态模型）：调用方
//     不直接传 status；当降级原因列表（含截断原因）非空时报告记为
//     degraded——部分能力受限但确定性结果仍可用，符合该节对 degraded 的
//     定义；列表为空时记为 completed。该推导保证不会出现「completed 却带
//     降级原因」「degraded 却无原因」的矛盾形态。这是已记录的实现层口径。
package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"change-risk-analyzer/internal/domain"
)

// MaxFindingsPerReport 是单份报告允许列出的 findings 数量上限。
// 来源为明文规格而非实现层默认值：spec/03-architecture.md 第 6 节
// 「最大 findings：20」，与 spec/schemas/risk-report.schema.json 中
// findings 的 maxItems:20 一致。
const MaxFindingsPerReport = 20

// DegradationFindingsTruncated 是 findings 超限截断的降级原因码，
// 采用 schema 规定的小写连字符风格（^[a-z0-9_.-]{1,80}$）。
const DegradationFindingsTruncated = "findings-truncated"

// BuilderInput 是构建一份报告所需的全部显式输入。
// 所有字段必填（TestGaps 当前没有生产者，恒为空列表，不设输入项）。
type BuilderInput struct {
	// Request 是经过事件解析与领域校验的分析请求身份。
	Request domain.ReviewRequest
	// Summary 是变更统计摘要。
	Summary domain.ChangeSummary
	// Findings 来自策略引擎 Result.Findings；顺序不限，构建时会深拷贝并
	// 按 domain.SortFindings 固定排序。
	Findings []domain.Finding
	// Dimensions 来自策略引擎 Result.Dimensions；构建时深拷贝并按固定维度
	// 顺序排序。
	Dimensions []domain.RiskDimension
	// OverallScore 与 OverallLevel 必须来自同一次策略求值；
	// OverallLevel 必须等于 domain.LevelFromScore(OverallScore)。
	OverallScore int
	OverallLevel domain.Severity
	// AnalyzerVersion 是当前分析程序版本字符串，进入报告供追溯。
	AnalyzerVersion string
	// GeneratedAt 是报告生成时间。显式传参保证同一输入可复现逐字节相同的
	// 渲染输出。
	GeneratedAt time.Time
	// DegradationReasons 是上游已发生的降级原因（如模型不可用）；findings
	// 截断原因会在其基础上追加，不会覆盖。
	DegradationReasons []domain.DegradationReason
	// Runtime 是运行元数据。risk-report/v1 协议强制要求 runtime 对象且其
	// 字段（耗时、patch 字节数等）无法由其他输入推导，因此作为显式入参。
	Runtime domain.RuntimeMetadata
}

// Build 组装并校验 RiskReport。任何输入非法都返回错误，绝不静默修复或丢弃；
// 组装结果必须同时通过领域校验与内置 JSON Schema 校验后才返回。
func Build(in BuilderInput) (*domain.RiskReport, error) {
	if err := in.Request.Validate(); err != nil {
		return nil, fmt.Errorf("report: request 非法: %w", err)
	}
	if err := in.Summary.Validate(); err != nil {
		return nil, fmt.Errorf("report: change_summary 非法: %w", err)
	}
	if strings.TrimSpace(in.AnalyzerVersion) == "" {
		return nil, fmt.Errorf("report: analyzer_version 不能为空")
	}
	if in.GeneratedAt.IsZero() {
		return nil, fmt.Errorf("report: generated_at 必须是显式传入的非零时间")
	}
	if in.OverallScore < 0 || in.OverallScore > 100 {
		return nil, fmt.Errorf("report: overall_score %d 必须在 0 到 100 之间", in.OverallScore)
	}
	if want := domain.LevelFromScore(in.OverallScore); in.OverallLevel != want {
		return nil, fmt.Errorf("report: overall_level %q 与 overall_score %d 不一致（应为 %s）",
			in.OverallLevel, in.OverallScore, want)
	}
	if err := in.Runtime.Validate(); err != nil {
		return nil, fmt.Errorf("report: runtime 元数据非法: %w", err)
	}

	findings := copySortedFindings(in.Findings)
	reasons := copyDegradationReasons(in.DegradationReasons)
	if len(findings) > MaxFindingsPerReport {
		total := len(findings)
		findings = findings[:MaxFindingsPerReport]
		reasons = append(reasons, domain.DegradationReason{
			Code: DegradationFindingsTruncated,
			Message: fmt.Sprintf("共发现 %d 条 findings，超过单份报告上限 %d 条"+
				"（spec/03 第 6 节），已按稳定排序保留优先级最高的 %d 条，其余 %d 条未列出；"+
				"总分与维度统计仍基于全部线索计算。",
				total, MaxFindingsPerReport, MaxFindingsPerReport, total-MaxFindingsPerReport),
		})
	}

	status := domain.StatusCompleted
	if len(reasons) > 0 {
		status = domain.StatusDegraded
	}

	built := &domain.RiskReport{
		SchemaVersion:      SchemaVersion,
		Status:             status,
		GeneratedAt:        in.GeneratedAt.UTC(),
		AnalyzerVersion:    in.AnalyzerVersion,
		Request:            in.Request,
		ChangeSummary:      in.Summary,
		OverallScore:       in.OverallScore,
		OverallLevel:       in.OverallLevel,
		Dimensions:         copySortedDimensions(in.Dimensions),
		Findings:           findings,
		TestGaps:           []domain.TestGap{},
		DegradationReasons: reasons,
		Runtime:            in.Runtime,
	}

	result, err := Validate(built)
	if err != nil {
		return nil, fmt.Errorf("report: 构建后校验执行失败: %w", err)
	}
	if !result.Valid() {
		return nil, fmt.Errorf("report: 构建的报告未通过双重校验: 领域问题 %v；schema 问题 %v",
			result.DomainErrors, result.SchemaErrors)
	}
	return built, nil
}

// RenderJSON 输出确定性缩进 JSON（末尾带换行）。相同报告实例序列化结果
// 逐字节相同；GeneratedAt 由构建器显式冻结为 UTC。
func RenderJSON(rep *domain.RiskReport) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("report: 报告为 nil，无法渲染 JSON")
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("report: JSON 序列化失败: %w", err)
	}
	data = append(data, '\n')
	return data, nil
}

// copySortedFindings 深拷贝 findings（含 Evidence 与 RuleIDs 切片），并按
// 领域稳定排序函数排序，使构建结果与调用方传入顺序无关且不被外部修改污染。
func copySortedFindings(src []domain.Finding) []domain.Finding {
	copied := make([]domain.Finding, 0, len(src))
	for _, f := range src {
		f.Evidence = append([]domain.Evidence(nil), f.Evidence...)
		f.RuleIDs = append([]string(nil), f.RuleIDs...)
		copied = append(copied, f)
	}
	domain.SortFindings(copied)
	return copied
}

// copySortedDimensions 深拷贝维度并按固定维度顺序排序；空输入返回非 nil
// 空切片以满足协议数组类型要求。
func copySortedDimensions(src []domain.RiskDimension) []domain.RiskDimension {
	copied := make([]domain.RiskDimension, 0, len(src))
	copied = append(copied, src...)
	domain.SortDimensions(copied)
	return copied
}

// copyDegradationReasons 返回降级原因的副本，保持调用方顺序。
func copyDegradationReasons(src []domain.DegradationReason) []domain.DegradationReason {
	copied := make([]domain.DegradationReason, 0, len(src))
	return append(copied, src...)
}
