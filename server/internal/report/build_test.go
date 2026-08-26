package report

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"change-risk-analyzer/internal/domain"
	"change-risk-analyzer/internal/policy"
)

// 与其他包一致的固定测试 SHA，保证金样与断言可复现。
const (
	testBaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testHeadSHA = "abcdef0123456789abcdef0123456789abcdef01"
)

var testGeneratedAt = time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)

func sampleRequest() domain.ReviewRequest {
	return domain.ReviewRequest{
		Repository: domain.RepositoryRef{
			Owner:    "example-org",
			Name:     "risk-analyzer",
			FullName: "example-org/risk-analyzer",
		},
		PullRequestNumber: 42,
		EventAction:       domain.ActionOpened,
		BaseSHA:           testBaseSHA,
		HeadSHA:           testHeadSHA,
		SourceKind:        domain.SourceSameRepo,
	}
}

func sampleSummary() domain.ChangeSummary {
	return domain.ChangeSummary{
		FilesSeen:     6,
		FilesAnalyzed: 6,
		Additions:     12,
		Deletions:     2,
	}
}

func sampleRuntime() domain.RuntimeMetadata {
	return domain.RuntimeMetadata{
		DurationMs:     120,
		FilesSeen:      6,
		FilesAnalyzed:  6,
		PatchBytesSeen: 4096,
	}
}

// lineSignal 构造带右侧行级证据的合法确定性信号。
func lineSignal(ruleID string, category domain.RiskCategory, weight int, file string, line int) domain.RiskSignal {
	return domain.RiskSignal{
		RuleID:   ruleID,
		Category: category,
		Fact:     fmt.Sprintf("%s 在 %s 第 %d 行识别出候选线索", ruleID, file, line),
		Evidence: []domain.Evidence{{
			File:      file,
			StartLine: line,
			EndLine:   line,
			Side:      domain.SideRight,
			Fact:      fmt.Sprintf("第 %d 号新增行命中规则 %s", line, ruleID),
		}},
		Source:     domain.SourceDeterministic,
		Confidence: 0.8,
		Weight:     weight,
	}
}

// fileSignal 构造文件级证据的信号（evidence_factor=0.7）。
func fileSignal(ruleID string, category domain.RiskCategory, weight int, file string) domain.RiskSignal {
	return domain.RiskSignal{
		RuleID:   ruleID,
		Category: category,
		Fact:     fmt.Sprintf("%s 在 %s 识别出候选线索", ruleID, file),
		Evidence: []domain.Evidence{{
			File: file,
			Side: domain.SideFile,
			Fact: fmt.Sprintf("文件级变更命中规则 %s", ruleID),
		}},
		Source:     domain.SourceDeterministic,
		Confidence: 0.6,
		Weight:     weight,
	}
}

// sampleSignals 覆盖四个类别：贡献分合计 30+20+15+7×0.7=69.9，
// 总分取整为 70（high），维度分数分别为 30/20/15/5。
func sampleSignals() []domain.RiskSignal {
	return []domain.RiskSignal{
		lineSignal("CR-SEC-001", domain.CategorySecurity, 30, ".github/workflows/ci.yml", 3),
		lineSignal("CR-SC-001", domain.CategorySupplyChain, 20, ".github/workflows/release.yml", 4),
		lineSignal("CR-REL-001", domain.CategoryReliability, 15, "pkg/client.go", 7),
		fileSignal("CR-TEST-001", domain.CategoryTestability, 7, "src/payment/service.go"),
	}
}

// buildSampleReport 通过真实策略引擎求值后构建标准多类别报告，
// 供构建测试与 Markdown 金样共用。
func buildSampleReport(t *testing.T) (*domain.RiskReport, policy.Result) {
	t.Helper()
	result, err := policy.Evaluate(sampleSignals())
	if err != nil {
		t.Fatalf("policy evaluate failed: %v", err)
	}
	built, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        result.Findings,
		Dimensions:      result.Dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	return built, result
}

func TestBuildProducesValidReportFromPolicyResult(t *testing.T) {
	built, result := buildSampleReport(t)

	if built.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want completed", built.Status)
	}
	if built.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %q, want %q", built.SchemaVersion, SchemaVersion)
	}
	if built.OverallScore != result.OverallScore || built.OverallLevel != result.OverallLevel {
		t.Fatalf("overall %d/%s != policy %d/%s",
			built.OverallScore, built.OverallLevel, result.OverallScore, result.OverallLevel)
	}
	if built.OverallScore != 70 || built.OverallLevel != domain.SeverityHigh {
		t.Fatalf("unexpected overall %d/%s, want 70/high", built.OverallScore, built.OverallLevel)
	}
	if len(built.Findings) != 4 || len(built.Dimensions) != 4 {
		t.Fatalf("unexpected sizes: findings=%d dimensions=%d", len(built.Findings), len(built.Dimensions))
	}

	// 排序断言：medium 的 security 线索在前，low 按首个证据文件路径排序。
	wantOrder := []string{
		"CR-SEC-001",  // medium
		"CR-SC-001",   // low，.github/workflows/release.yml
		"CR-REL-001",  // low，pkg/client.go
		"CR-TEST-001", // low，src/payment/service.go
	}
	for i, f := range built.Findings {
		if f.RuleIDs[0] != wantOrder[i] {
			t.Fatalf("finding[%d] rule = %s, want %s", i, f.RuleIDs[0], wantOrder[i])
		}
	}
	wantDims := []domain.RiskCategory{
		domain.CategorySecurity, domain.CategoryReliability,
		domain.CategorySupplyChain, domain.CategoryTestability,
	}
	for i, d := range built.Dimensions {
		if d.Category != wantDims[i] {
			t.Fatalf("dimension[%d] = %s, want %s", i, d.Category, wantDims[i])
		}
	}

	// 构建结果必须再次通过包内双重校验。
	res, err := Validate(built)
	if err != nil || !res.Valid() {
		t.Fatalf("built report failed double validation: %+v err=%v", res, err)
	}
}

func TestBuildJSONRoundTripPreservesReport(t *testing.T) {
	built, _ := buildSampleReport(t)
	data, err := RenderJSON(built)
	if err != nil {
		t.Fatalf("render json failed: %v", err)
	}
	var restored domain.RiskReport
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if !reflect.DeepEqual(&restored, built) {
		t.Fatal("json round trip changed the report")
	}
}

func TestBuildEmptySignalsYieldsCompletedLowRiskReport(t *testing.T) {
	result, err := policy.Evaluate(nil)
	if err != nil {
		t.Fatalf("evaluate empty failed: %v", err)
	}
	built, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         domain.ChangeSummary{},
		Findings:        result.Findings,
		Dimensions:      result.Dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         domain.RuntimeMetadata{},
	})
	if err != nil {
		t.Fatalf("build empty report failed: %v", err)
	}
	if built.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want completed", built.Status)
	}
	if built.OverallScore != 0 || built.OverallLevel != domain.SeverityLow {
		t.Fatalf("overall = %d/%s, want 0/low", built.OverallScore, built.OverallLevel)
	}
	if built.Findings == nil || built.Dimensions == nil || built.TestGaps == nil || built.DegradationReasons == nil {
		t.Fatal("empty report arrays must be non-nil so they serialize as []")
	}
	res, err := Validate(built)
	if err != nil || !res.Valid() {
		t.Fatalf("empty report failed double validation: %+v err=%v", res, err)
	}
}

func TestBuildTruncatesFindingsBeyondLimitWithReason(t *testing.T) {
	signals := make([]domain.RiskSignal, 0, MaxFindingsPerReport+5)
	for i := 0; i < MaxFindingsPerReport+5; i++ {
		signals = append(signals,
			lineSignal(fmt.Sprintf("CR-STUB-%02d", i), domain.CategoryAPI, 40-i, fmt.Sprintf("pkg/file-%02d.go", i), i+1))
	}
	result, err := policy.Evaluate(signals)
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	if len(result.Findings) != MaxFindingsPerReport+5 {
		t.Fatalf("policy produced %d findings, want %d", len(result.Findings), MaxFindingsPerReport+5)
	}

	built, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        result.Findings,
		Dimensions:      result.Dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	})
	if err != nil {
		t.Fatalf("build truncated report failed: %v", err)
	}

	if len(built.Findings) != MaxFindingsPerReport {
		t.Fatalf("findings not truncated: got %d, want %d", len(built.Findings), MaxFindingsPerReport)
	}
	if built.Status != domain.StatusDegraded {
		t.Fatalf("status = %q, want degraded after truncation", built.Status)
	}

	var truncation *domain.DegradationReason
	for i := range built.DegradationReasons {
		if built.DegradationReasons[i].Code == DegradationFindingsTruncated {
			truncation = &built.DegradationReasons[i]
		}
	}
	if truncation == nil {
		t.Fatalf("missing %s reason: %+v", DegradationFindingsTruncated, built.DegradationReasons)
	}
	for _, want := range []string{"25", "20"} {
		if !strings.Contains(truncation.Message, want) {
			t.Fatalf("truncation message %q missing %q", truncation.Message, want)
		}
	}

	// 截断保留项必须是既有稳定排序的前缀（严重级别高者优先）。
	wantPrefix := append([]domain.Finding(nil), result.Findings...)
	domain.SortFindings(wantPrefix)
	for i := range built.Findings {
		if built.Findings[i].ID != wantPrefix[i].ID {
			t.Fatalf("kept finding[%d] = %s, want %s", i, built.Findings[i].ID, wantPrefix[i].ID)
		}
	}
	// 分数仍基于全部线索：总分不被裁剪影响。
	if built.OverallScore != result.OverallScore {
		t.Fatalf("overall score changed by truncation: %d != %d", built.OverallScore, result.OverallScore)
	}

	res, err := Validate(built)
	if err != nil || !res.Valid() {
		t.Fatalf("truncated report failed double validation: %+v err=%v", res, err)
	}
}

func TestBuildRejectsInvalidInputs(t *testing.T) {
	validResult, err := policy.Evaluate(sampleSignals())
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	base := BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        validResult.Findings,
		Dimensions:      validResult.Dimensions,
		OverallScore:    validResult.OverallScore,
		OverallLevel:    validResult.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	}

	cases := map[string]struct {
		mutate func(in BuilderInput) BuilderInput
	}{
		"missing request identity": {
			mutate: func(in BuilderInput) BuilderInput { in.Request = domain.ReviewRequest{}; return in },
		},
		"negative summary counter": {
			mutate: func(in BuilderInput) BuilderInput { in.Summary.FilesSeen = -1; return in },
		},
		"empty analyzer version": {
			mutate: func(in BuilderInput) BuilderInput { in.AnalyzerVersion = "  "; return in },
		},
		"zero generated_at": {
			mutate: func(in BuilderInput) BuilderInput { in.GeneratedAt = time.Time{}; return in },
		},
		"score out of range": {
			mutate: func(in BuilderInput) BuilderInput { in.OverallScore = 101; return in },
		},
		"level inconsistent with score": {
			mutate: func(in BuilderInput) BuilderInput { in.OverallLevel = domain.SeverityLow; return in },
		},
		"negative runtime duration": {
			mutate: func(in BuilderInput) BuilderInput { in.Runtime.DurationMs = -5; return in },
		},
		"finding violates inline eligibility": {
			mutate: func(in BuilderInput) BuilderInput {
				badFinding := domain.Finding{
					ID:             "CR-BAD:pkg-a.go:1",
					Category:       domain.CategorySecurity,
					Severity:       domain.SeverityLow,
					EvidenceStatus: domain.EvidenceConfirmed,
					Confidence:     0.5,
					Title:          "非法行级候选",
					Impact:         "inline_eligible 缺少右侧行号",
					Evidence: []domain.Evidence{{
						File: "pkg/a.go", Side: domain.SideFile, Fact: "文件级事实",
					}},
					Recommendation: "复核",
					RuleIDs:        []string{"CR-BAD"},
					Source:         domain.SourceDeterministic,
					InlineEligible: true,
				}
				in.Findings = []domain.Finding{badFinding}
				return in
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			built, err := Build(tc.mutate(base))
			if err == nil {
				t.Fatalf("expected error for %s, got report %+v", name, built)
			}
		})
	}
}

func TestBuildIsDeterministicAcrossRunsAndInputOrder(t *testing.T) {
	built, result := buildSampleReport(t)

	again, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        result.Findings,
		Dimensions:      result.Dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	})
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}
	if !reflect.DeepEqual(built, again) {
		t.Fatal("same inputs produced different reports")
	}

	// 打乱 findings 输入顺序后结果必须一致（构建器内部重新稳定排序）。
	shuffled := append([]domain.Finding(nil), result.Findings...)
	reversedInPlace(shuffled)
	shuffledDims := append([]domain.RiskDimension(nil), result.Dimensions...)
	reversedInPlace(shuffledDims)
	fromShuffled, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        shuffled,
		Dimensions:      shuffledDims,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	})
	if err != nil {
		t.Fatalf("build from shuffled input failed: %v", err)
	}
	if !reflect.DeepEqual(built, fromShuffled) {
		t.Fatal("input order changed the built report")
	}

	first, err := RenderJSON(built)
	if err != nil {
		t.Fatalf("render json failed: %v", err)
	}
	second, err := RenderJSON(fromShuffled)
	if err != nil {
		t.Fatalf("render json failed: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("json render is not byte-identical across equivalent builds")
	}
}

func reversedInPlace[T any](items []T) {
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
}
