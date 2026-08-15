package domain

import "testing"

func validEvidence() Evidence {
	return Evidence{
		File:      "internal/handler.go",
		StartLine: 12,
		EndLine:   14,
		Side:      SideRight,
		Fact:      "新增请求处理路径",
	}
}

func validFinding() Finding {
	return Finding{
		ID:             "CR-API-001:internal.handler.go:12",
		Category:       CategoryAPI,
		Severity:       SeverityMedium,
		EvidenceStatus: EvidenceConfirmed,
		Confidence:     0.9,
		Title:          "接口行为发生变化",
		Impact:         "现有调用方可能需要同步调整。",
		Evidence:       []Evidence{validEvidence()},
		Recommendation: "补充兼容性和消费者测试。",
		RuleIDs:        []string{"CR-API-001"},
		Source:         SourceDeterministic,
	}
}

func TestLevelFromScoreBoundaries(t *testing.T) {
	tests := []struct {
		score int
		want  Severity
	}{
		{0, SeverityLow}, {24, SeverityLow}, {25, SeverityMedium},
		{49, SeverityMedium}, {50, SeverityHigh}, {74, SeverityHigh},
		{75, SeverityCritical}, {100, SeverityCritical},
	}
	for _, tc := range tests {
		if got := LevelFromScore(tc.score); got != tc.want {
			t.Errorf("LevelFromScore(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestEvidenceValidate(t *testing.T) {
	if err := validEvidence().Validate(); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*Evidence)
	}{
		{name: "file side does not need line", edit: func(e *Evidence) { e.Side, e.StartLine, e.EndLine = SideFile, 0, 0 }},
		{name: "left side requires positive lines", edit: func(e *Evidence) { e.Side, e.StartLine = SideLeft, 0 }},
		{name: "end cannot precede start", edit: func(e *Evidence) { e.EndLine = e.StartLine - 1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			evidence := validEvidence()
			tc.edit(&evidence)
			if tc.name == "file side does not need line" {
				if err := evidence.Validate(); err != nil {
					t.Fatalf("file evidence rejected: %v", err)
				}
				return
			}
			if err := evidence.Validate(); err == nil {
				t.Fatal("invalid evidence was accepted")
			}
		})
	}
}

func TestFindingValidate(t *testing.T) {
	if err := validFinding().Validate(); err != nil {
		t.Fatalf("valid finding rejected: %v", err)
	}

	t.Run("high severity requires valid evidence", func(t *testing.T) {
		finding := validFinding()
		finding.Severity = SeverityHigh
		finding.Evidence = nil
		if err := finding.Validate(); err == nil {
			t.Fatal("high finding without evidence was accepted")
		}
	})

	t.Run("inline finding requires right line evidence", func(t *testing.T) {
		finding := validFinding()
		finding.InlineEligible = true
		finding.Evidence[0].Side = SideFile
		finding.Evidence[0].StartLine = 0
		finding.Evidence[0].EndLine = 0
		if err := finding.Validate(); err == nil {
			t.Fatal("inline finding without right line evidence was accepted")
		}
	})
}

func TestRiskSignalValidate(t *testing.T) {
	signal := RiskSignal{
		RuleID:     "CR-API-001",
		Category:   CategoryAPI,
		Fact:       "导出函数签名发生变化",
		Evidence:   []Evidence{validEvidence()},
		Source:     SourceDeterministic,
		Confidence: 1,
		Weight:     40,
	}
	if err := signal.Validate(); err != nil {
		t.Fatalf("valid signal rejected: %v", err)
	}

	signal.Confidence = 1.01
	if err := signal.Validate(); err == nil {
		t.Fatal("out-of-range confidence was accepted")
	}
}

func TestRiskDimensionValidate(t *testing.T) {
	dimension := RiskDimension{
		Category:    CategorySecurity,
		Score:       50,
		Level:       SeverityHigh,
		SignalCount: 1,
		Summary:     "存在需要复核的安全变更。",
	}
	if err := dimension.Validate(); err != nil {
		t.Fatalf("valid dimension rejected: %v", err)
	}
	dimension.Level = SeverityMedium
	if err := dimension.Validate(); err == nil {
		t.Fatal("dimension with mismatched level was accepted")
	}
}

func TestReportNestedValidation(t *testing.T) {
	if err := (DegradationReason{Code: "provider.timeout", Message: "模型调用超时"}).Validate(); err != nil {
		t.Fatalf("valid degradation reason rejected: %v", err)
	}
	if err := (DegradationReason{Code: "INVALID CODE", Message: "bad"}).Validate(); err == nil {
		t.Fatal("invalid degradation code was accepted")
	}

	token := -1
	if err := (RuntimeMetadata{TokenInput: &token}).Validate(); err == nil {
		t.Fatal("negative token count was accepted")
	}
}

func TestSortFindingsAndDimensions(t *testing.T) {
	findings := []Finding{
		func() Finding { f := validFinding(); f.ID = "medium-b"; f.Evidence[0].File = "z.go"; return f }(),
		func() Finding { f := validFinding(); f.ID = "critical"; f.Severity = SeverityCritical; return f }(),
		func() Finding { f := validFinding(); f.ID = "medium-a"; f.Evidence[0].File = "a.go"; return f }(),
	}
	SortFindings(findings)
	if findings[0].ID != "critical" || findings[1].ID != "medium-a" || findings[2].ID != "medium-b" {
		t.Fatalf("unexpected finding order: %q, %q, %q", findings[0].ID, findings[1].ID, findings[2].ID)
	}

	dimensions := []RiskDimension{
		{Category: CategoryTestability},
		{Category: CategorySecurity},
		{Category: CategoryAPI},
	}
	SortDimensions(dimensions)
	if dimensions[0].Category != CategorySecurity || dimensions[1].Category != CategoryAPI || dimensions[2].Category != CategoryTestability {
		t.Fatalf("unexpected dimension order: %q, %q, %q", dimensions[0].Category, dimensions[1].Category, dimensions[2].Category)
	}
}
