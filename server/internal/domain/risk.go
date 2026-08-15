package domain

import (
	"sort"
	"time"
)

// RiskCategory 是固定的风险维度枚举。
type RiskCategory string

const (
	CategorySecurity    RiskCategory = "security"
	CategoryData        RiskCategory = "data"
	CategoryAPI         RiskCategory = "api"
	CategoryReliability RiskCategory = "reliability"
	CategoryConcurrency RiskCategory = "concurrency"
	CategoryPerformance RiskCategory = "performance"
	CategoryDelivery    RiskCategory = "delivery"
	CategorySupplyChain RiskCategory = "supply_chain"
	CategoryTestability RiskCategory = "testability"
)

// Severity 描述风险级别。
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// EvidenceSide 描述证据定位到哪一侧。
type EvidenceSide string

const (
	SideLeft  EvidenceSide = "left"
	SideRight EvidenceSide = "right"
	SideFile  EvidenceSide = "file"
)

// SignalSource 描述信号或发现的来源。
type SignalSource string

const (
	SourceDeterministic SignalSource = "deterministic"
	SourceModel         SignalSource = "model"
	SourceCombined      SignalSource = "combined"
)

// ReportStatus 描述报告状态。
type ReportStatus string

const (
	StatusCompleted ReportStatus = "completed"
	StatusDegraded  ReportStatus = "degraded"
	StatusSkipped   ReportStatus = "skipped"
	StatusFailed    ReportStatus = "failed"
)

// EvidenceStatus 描述证据的可信状态。
type EvidenceStatus string

const (
	EvidenceConfirmed   EvidenceStatus = "confirmed"
	EvidenceNeedsReview EvidenceStatus = "needs_review"
)

// TestGapPriority 描述测试缺口优先级。
type TestGapPriority string

const (
	PriorityLow    TestGapPriority = "low"
	PriorityMedium TestGapPriority = "medium"
	PriorityHigh   TestGapPriority = "high"
)

// Evidence 是定位到文件、行或变更片段的支持信息。
// 行号不可伪造：无法从 patch 确定行号时使用 side=file，不能填 0 作为假行号。
type Evidence struct {
	File      string       `json:"file"`
	StartLine int          `json:"start_line,omitempty"`
	EndLine   int          `json:"end_line,omitempty"`
	Side      EvidenceSide `json:"side"`
	Excerpt   string       `json:"excerpt,omitempty"`
	Fact      string       `json:"fact"`
}

// RiskSignal 是从变更中提取的事实或风险线索，不等于最终结论。
type RiskSignal struct {
	RuleID        string       `json:"rule_id"`
	Category      RiskCategory `json:"category"`
	Fact          string       `json:"fact"`
	Evidence      []Evidence   `json:"evidence"`
	Source        SignalSource `json:"source"`
	Confidence    float64      `json:"confidence"`
	Weight        int          `json:"weight"`
	MitigationIDs []string     `json:"mitigation_ids,omitempty"`
}

// Finding 是经过证据校验并可供开发者复核的风险发现。
// ID 由规则、路径、行范围和稳定事实摘要生成，不能使用随机 UUID。
type Finding struct {
	ID             string         `json:"id"`
	Category       RiskCategory   `json:"category"`
	Severity       Severity       `json:"severity"`
	EvidenceStatus EvidenceStatus `json:"evidence_status"`
	Confidence     float64        `json:"confidence"`
	Title          string         `json:"title"`
	Impact         string         `json:"impact"`
	Evidence       []Evidence     `json:"evidence"`
	Recommendation string         `json:"recommendation"`
	RuleIDs        []string       `json:"rule_ids"`
	Source         SignalSource   `json:"source"`
	InlineEligible bool           `json:"inline_eligible"`
}

// RiskDimension 是单个风险维度的汇总。
type RiskDimension struct {
	Category    RiskCategory `json:"category"`
	Score       int          `json:"score"`
	Level       Severity     `json:"level"`
	SignalCount int          `json:"signal_count"`
	Summary     string       `json:"summary"`
}

// TestGap 是测试缺口建议，不等于断言测试一定缺失。
type TestGap struct {
	Area            string          `json:"area"`
	Reason          string          `json:"reason"`
	RecommendedTest string          `json:"recommended_test"`
	Evidence        []Evidence      `json:"evidence,omitempty"`
	Priority        TestGapPriority `json:"priority"`
}

// DegradationReason 描述部分能力失败的原因。
type DegradationReason struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

// RuntimeMetadata 记录运行信息，禁止包含 API Key、完整 Prompt、完整代码或 Token。
type RuntimeMetadata struct {
	DurationMs       int    `json:"duration_ms"`
	FilesSeen        int    `json:"files_seen"`
	FilesAnalyzed    int    `json:"files_analyzed"`
	PatchBytesSeen   int    `json:"patch_bytes_seen"`
	ContextTruncated bool   `json:"context_truncated"`
	ModelProvider    string `json:"model_provider,omitempty"`
	ModelName        string `json:"model_name,omitempty"`
	PromptVersion    string `json:"prompt_version,omitempty"`
	TokenInput       *int   `json:"token_input,omitempty"`
	TokenOutput      *int   `json:"token_output,omitempty"`
}

// RiskReport 是发布前冻结的不可变报告。
type RiskReport struct {
	SchemaVersion      string              `json:"schema_version"`
	Status             ReportStatus        `json:"status"`
	GeneratedAt        time.Time           `json:"generated_at"`
	AnalyzerVersion    string              `json:"analyzer_version"`
	Request            ReviewRequest       `json:"request"`
	ChangeSummary      ChangeSummary       `json:"change_summary"`
	OverallScore       int                 `json:"overall_score"`
	OverallLevel       Severity            `json:"overall_level"`
	Dimensions         []RiskDimension     `json:"dimensions"`
	Findings           []Finding           `json:"findings"`
	TestGaps           []TestGap           `json:"test_gaps"`
	DegradationReasons []DegradationReason `json:"degradation_reasons"`
	Runtime            RuntimeMetadata     `json:"runtime"`
}

// LevelFromScore 根据初始阈值把分数映射为级别。
// 阈值为 spec/02-risk-model.md 的初始配置，策略阶段可再调整。
func LevelFromScore(score int) Severity {
	switch {
	case score >= 75:
		return SeverityCritical
	case score >= 50:
		return SeverityHigh
	case score >= 25:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func validCategory(c RiskCategory) bool {
	switch c {
	case CategorySecurity, CategoryData, CategoryAPI, CategoryReliability,
		CategoryConcurrency, CategoryPerformance, CategoryDelivery,
		CategorySupplyChain, CategoryTestability:
		return true
	}
	return false
}

func validSeverity(s Severity) bool {
	switch s {
	case SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	}
	return false
}

func validSide(s EvidenceSide) bool {
	switch s {
	case SideLeft, SideRight, SideFile:
		return true
	}
	return false
}

func validSignalSource(s SignalSource) bool {
	switch s {
	case SourceDeterministic, SourceModel, SourceCombined:
		return true
	}
	return false
}

func validReportStatus(s ReportStatus) bool {
	switch s {
	case StatusCompleted, StatusDegraded, StatusSkipped, StatusFailed:
		return true
	}
	return false
}

func validEvidenceStatus(s EvidenceStatus) bool {
	switch s {
	case EvidenceConfirmed, EvidenceNeedsReview:
		return true
	}
	return false
}

func validTestGapPriority(p TestGapPriority) bool {
	switch p {
	case PriorityLow, PriorityMedium, PriorityHigh:
		return true
	}
	return false
}

// Validate 校验证据：必填字段、side 枚举，以及 left/right 侧的行号范围。
func (e Evidence) Validate() error {
	v := &validator{}
	v.check(e.File != "", "file 不能为空")
	v.check(e.Fact != "", "fact 不能为空")
	v.check(validSide(e.Side), "side %q 非法", e.Side)
	if e.Side == SideLeft || e.Side == SideRight {
		v.check(e.StartLine >= 1, "side=%s 时 start_line 必须 >= 1，实际为 %d", e.Side, e.StartLine)
		v.check(e.EndLine >= 1, "side=%s 时 end_line 必须 >= 1，实际为 %d", e.Side, e.EndLine)
		v.check(e.EndLine >= e.StartLine, "end_line %d 小于 start_line %d", e.EndLine, e.StartLine)
	}
	return v.err()
}

// Validate 校验信号字段：规则、事实、来源、置信度和权重。
func (s RiskSignal) Validate() error {
	v := &validator{}
	v.check(s.RuleID != "", "rule_id 不能为空")
	v.check(validCategory(s.Category), "category %q 非法", s.Category)
	v.check(s.Fact != "", "fact 不能为空")
	v.check(validSignalSource(s.Source), "source %q 非法", s.Source)
	v.check(s.Confidence >= 0 && s.Confidence <= 1, "confidence %v 必须在 0 到 1 之间", s.Confidence)
	v.check(s.Weight >= 1, "weight %d 必须 >= 1（spec 02 的 signal_weight 范围为 1 到 40）", s.Weight)
	for i := range s.Evidence {
		if err := s.Evidence[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	return v.err()
}

// Validate 校验发现：基础字段、证据合法性，以及
// high/critical 必须包含有效证据、inline_eligible 必须定位到右侧行。
func (f Finding) Validate() error {
	v := &validator{}
	v.check(findingIDPattern.MatchString(f.ID), "id %q 不符合 ^[A-Za-z0-9_.:-]{1,160}$", f.ID)
	v.check(validCategory(f.Category), "category %q 非法", f.Category)
	v.check(validSeverity(f.Severity), "severity %q 非法", f.Severity)
	v.check(validEvidenceStatus(f.EvidenceStatus), "evidence_status %q 非法", f.EvidenceStatus)
	v.check(f.Confidence >= 0 && f.Confidence <= 1, "confidence %v 必须在 0 到 1 之间", f.Confidence)
	v.check(f.Title != "", "title 不能为空")
	v.check(f.Impact != "", "impact 不能为空")
	v.check(f.Recommendation != "", "recommendation 不能为空")
	v.check(validSignalSource(f.Source), "source %q 非法", f.Source)
	v.check(len(f.Evidence) > 0, "evidence 不能为空")
	v.check(len(f.RuleIDs) > 0, "rule_ids 不能为空")

	validEvidence := 0
	rightLine := false
	for i := range f.Evidence {
		if err := f.Evidence[i].Validate(); err == nil {
			validEvidence++
		} else {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
		if f.Evidence[i].Side == SideRight && f.Evidence[i].StartLine >= 1 && f.Evidence[i].EndLine >= 1 {
			rightLine = true
		}
	}
	if f.Severity == SeverityHigh || f.Severity == SeverityCritical {
		v.check(validEvidence >= 1, "severity=%s 的 finding 必须包含至少一个有效 Evidence", f.Severity)
	}
	v.check(!f.InlineEligible || rightLine, "inline_eligible=true 时必须存在 side=right 且行号为正的证据")
	return v.err()
}

// Validate 校验维度：类别、分数范围、级别与分数一致。
func (d RiskDimension) Validate() error {
	v := &validator{}
	v.check(validCategory(d.Category), "category %q 非法", d.Category)
	v.check(d.Score >= 0 && d.Score <= 100, "score %d 必须在 0 到 100 之间", d.Score)
	v.check(validSeverity(d.Level), "level %q 非法", d.Level)
	v.check(d.Level == LevelFromScore(d.Score), "level %q 与 score %d 不匹配（应为 %s）", d.Level, d.Score, LevelFromScore(d.Score))
	v.check(d.SignalCount >= 0, "signal_count 必须 >= 0，实际为 %d", d.SignalCount)
	v.check(d.Summary != "", "summary 不能为空")
	return v.err()
}

// Validate 校验测试缺口建议。
func (g TestGap) Validate() error {
	v := &validator{}
	v.check(g.Area != "", "area 不能为空")
	v.check(g.Reason != "", "reason 不能为空")
	v.check(g.RecommendedTest != "", "recommended_test 不能为空")
	v.check(validTestGapPriority(g.Priority), "priority %q 非法", g.Priority)
	for i := range g.Evidence {
		if err := g.Evidence[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	return v.err()
}

// Validate 校验降级原因：code 格式和必填 message。
func (r DegradationReason) Validate() error {
	v := &validator{}
	v.check(degradationRe.MatchString(r.Code), "code %q 不符合 ^[a-z0-9_.-]{1,80}$", r.Code)
	v.check(r.Message != "", "message 不能为空")
	return v.err()
}

// Validate 校验运行元数据非负。
func (r RuntimeMetadata) Validate() error {
	v := &validator{}
	v.check(r.DurationMs >= 0, "duration_ms 必须 >= 0，实际为 %d", r.DurationMs)
	v.check(r.FilesSeen >= 0, "files_seen 必须 >= 0，实际为 %d", r.FilesSeen)
	v.check(r.FilesAnalyzed >= 0, "files_analyzed 必须 >= 0，实际为 %d", r.FilesAnalyzed)
	v.check(r.PatchBytesSeen >= 0, "patch_bytes_seen 必须 >= 0，实际为 %d", r.PatchBytesSeen)
	if r.TokenInput != nil {
		v.check(*r.TokenInput >= 0, "token_input 必须 >= 0，实际为 %d", *r.TokenInput)
	}
	if r.TokenOutput != nil {
		v.check(*r.TokenOutput >= 0, "token_output 必须 >= 0，实际为 %d", *r.TokenOutput)
	}
	return v.err()
}

// Validate 校验完整报告：schema 版本、状态、请求身份、分数级别一致性和嵌套对象。
func (r RiskReport) Validate() error {
	v := &validator{}
	v.check(r.SchemaVersion == "risk-report/v1", "schema_version %q 必须是 risk-report/v1", r.SchemaVersion)
	v.check(validReportStatus(r.Status), "status %q 非法", r.Status)
	v.check(r.AnalyzerVersion != "", "analyzer_version 不能为空")
	v.check(r.OverallScore >= 0 && r.OverallScore <= 100, "overall_score %d 必须在 0 到 100 之间", r.OverallScore)
	v.check(r.OverallLevel == LevelFromScore(r.OverallScore), "overall_level %q 与 overall_score %d 不匹配（应为 %s）", r.OverallLevel, r.OverallScore, LevelFromScore(r.OverallScore))
	if r.Status == StatusDegraded || r.Status == StatusSkipped {
		v.check(len(r.DegradationReasons) > 0, "status=%s 时必须至少有一个 degradation_reasons", r.Status)
	}
	if err := r.Request.Validate(); err != nil {
		v.problems = append(v.problems, err.(*ValidationError).Problems...)
	}
	if err := r.ChangeSummary.Validate(); err != nil {
		v.problems = append(v.problems, err.(*ValidationError).Problems...)
	}
	for i := range r.Dimensions {
		if err := r.Dimensions[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	for i := range r.Findings {
		if err := r.Findings[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	for i := range r.TestGaps {
		if err := r.TestGaps[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	for i := range r.DegradationReasons {
		if err := r.DegradationReasons[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	if err := r.Runtime.Validate(); err != nil {
		v.problems = append(v.problems, err.(*ValidationError).Problems...)
	}
	return v.err()
}

var severityWeight = map[Severity]int{
	SeverityLow:      0,
	SeverityMedium:   1,
	SeverityHigh:     2,
	SeverityCritical: 3,
}

// SortFindings 按严重级别降序、首个证据的文件路径、行号和 ID 升序稳定排序。
// 构建报告时必须调用，保证同一输入产生稳定输出。
func SortFindings(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		wi, wj := severityWeight[findings[i].Severity], severityWeight[findings[j].Severity]
		if wi != wj {
			return wi > wj
		}
		fi, fj := findingFirstFileLine(findings[i]), findingFirstFileLine(findings[j])
		if fi.file != fj.file {
			return fi.file < fj.file
		}
		if fi.line != fj.line {
			return fi.line < fj.line
		}
		return findings[i].ID < findings[j].ID
	})
}

type fileLine struct {
	file string
	line int
}

func findingFirstFileLine(f Finding) fileLine {
	if len(f.Evidence) == 0 {
		return fileLine{}
	}
	line := f.Evidence[0].StartLine
	if line < 1 {
		line = 0
	}
	return fileLine{file: f.Evidence[0].File, line: line}
}

// 维度稳定顺序，与 spec/02-risk-model.md 的表格顺序一致。
var categoryOrder = []RiskCategory{
	CategorySecurity, CategoryData, CategoryAPI, CategoryReliability,
	CategoryConcurrency, CategoryPerformance, CategoryDelivery,
	CategorySupplyChain, CategoryTestability,
}

var categoryRank = func() map[RiskCategory]int {
	m := make(map[RiskCategory]int, len(categoryOrder))
	for i, c := range categoryOrder {
		m[c] = i
	}
	return m
}()

// SortDimensions 按固定维度顺序稳定排序，便于 golden test。
func SortDimensions(dims []RiskDimension) {
	sort.SliceStable(dims, func(i, j int) bool {
		return categoryRank[dims[i].Category] < categoryRank[dims[j].Category]
	})
}
