// policy_test.go 覆盖策略引擎的转换、评分、门禁与稳定性契约：
// 多类别正例、空输入、非法证据拒绝（含规则 ID 与路径）、阈值边界、
// 重复与乱序输入的幂等稳定，以及默认门禁恒不阻塞。
package policy

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"change-risk-analyzer/internal/domain"
)

// buildSignal 构造一条合法的确定性信号；side=start=end=0 表示文件级证据。
func buildSignal(ruleID string, category domain.RiskCategory, weight int, confidence float64,
	file string, side domain.EvidenceSide, start, end int) domain.RiskSignal {
	evidence := domain.Evidence{File: file, Side: side, Fact: fmt.Sprintf("%s 命中证据：%s", ruleID, file)}
	if side == domain.SideLeft || side == domain.SideRight {
		evidence.StartLine = start
		evidence.EndLine = end
	}
	return domain.RiskSignal{
		RuleID:     ruleID,
		Category:   category,
		Fact:       fmt.Sprintf("%s 在 %s 中识别出待确认线索，需要结合上下文确认。", ruleID, file),
		Evidence:   []domain.Evidence{evidence},
		Source:     domain.SourceDeterministic,
		Confidence: confidence,
		Weight:     weight,
	}
}

// buildMultiCategorySignals 覆盖七类确定性规则的代表信号，
// 其中 CR-API-001 同时带删除侧与新增侧证据（签名替换形态）。
func buildMultiCategorySignals() []domain.RiskSignal {
	apiReplacement := buildSignal("CR-API-001", domain.CategoryAPI, 25, 0.9, "pkg/api/user.go", domain.SideLeft, 12, 12)
	apiReplacement.Evidence = append(apiReplacement.Evidence, domain.Evidence{
		File: "pkg/api/user.go", StartLine: 12, EndLine: 12, Side: domain.SideRight,
		Fact: "CR-API-001 替换后的导出声明",
	})
	testEvidence := buildSignal("CR-TEST-001", domain.CategoryTestability, 15, 0.6,
		"db/migrations/004_drop.sql", domain.SideFile, 0, 0)
	testEvidence.Evidence[0].Fact = "未观察到 db/migrations/004_drop.sql 的配套测试变更，建议补充验证"

	return []domain.RiskSignal{
		buildSignal("CR-SEC-001", domain.CategorySecurity, 30, 1.0, ".github/workflows/ci.yml", domain.SideRight, 3, 3),
		buildSignal("CR-DATA-001", domain.CategoryData, 35, 0.85, "db/migrations/004_drop.sql", domain.SideRight, 2, 2),
		apiReplacement,
		buildSignal("CR-REL-001", domain.CategoryReliability, 20, 0.8, "client/http.go", domain.SideRight, 5, 7),
		buildSignal("CR-CON-001", domain.CategoryConcurrency, 20, 0.65, "worker/pool.go", domain.SideRight, 9, 9),
		buildSignal("CR-SC-001", domain.CategorySupplyChain, 20, 0.8, "deploy/compose.yaml", domain.SideRight, 4, 4),
		testEvidence,
	}
}

func TestEvaluateMultiCategorySignalsDeterministically(t *testing.T) {
	result, err := Evaluate(buildMultiCategorySignals())
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}

	if len(result.Findings) != 7 {
		t.Fatalf("expected 7 findings, got %d", len(result.Findings))
	}
	wantIDs := []string{
		"CR-SEC-001:.github_workflows_ci.yml:3",
		"CR-DATA-001:db_migrations_004_drop.sql:2",
		"CR-API-001:pkg_api_user.go:12",
		"CR-REL-001:client_http.go:5",
		"CR-CON-001:worker_pool.go:9",
		"CR-SC-001:deploy_compose.yaml:4",
		"CR-TEST-001:db_migrations_004_drop.sql:0",
	}
	gotIDs := make(map[string]bool)
	for _, finding := range result.Findings {
		if err := finding.Validate(); err != nil {
			t.Fatalf("finding %s violates domain invariants: %v", finding.ID, err)
		}
		if gotIDs[finding.ID] {
			t.Fatalf("duplicate finding id %s", finding.ID)
		}
		gotIDs[finding.ID] = true
	}
	for _, want := range wantIDs {
		if !gotIDs[want] {
			t.Errorf("finding id %q missing; got %v", want, gotIDs)
		}
	}

	// 维度分组：七个类别各一条维度，分数为该类别贡献分之和。
	wantDimensionScores := map[domain.RiskCategory]int{
		domain.CategorySecurity:    30,
		domain.CategoryData:        35,
		domain.CategoryAPI:         25,
		domain.CategoryReliability: 20,
		domain.CategoryConcurrency: 20,
		domain.CategorySupplyChain: 20,
		domain.CategoryTestability: 11, // round(15 × 0.7 文件级因子) = round(10.5)
	}
	if len(result.Dimensions) != len(wantDimensionScores) {
		t.Fatalf("expected %d dimensions, got %d: %+v", len(wantDimensionScores), len(result.Dimensions), result.Dimensions)
	}
	for _, dim := range result.Dimensions {
		if err := dim.Validate(); err != nil {
			t.Fatalf("dimension %s violates domain invariants: %v", dim.Category, err)
		}
		wantScore, ok := wantDimensionScores[dim.Category]
		if !ok {
			t.Fatalf("unexpected dimension %s", dim.Category)
		}
		if dim.Score != wantScore {
			t.Errorf("dimension %s score = %d, want %d", dim.Category, dim.Score, wantScore)
		}
		if dim.SignalCount != 1 {
			t.Errorf("dimension %s signal_count = %d, want 1", dim.Category, dim.SignalCount)
		}
	}

	// 总分：raw = 160.5，round 后 161，封顶 min(100, ·)；级别由 LevelFromScore 得出。
	if result.OverallScore != 100 {
		t.Errorf("overall score = %d, want 100 (capped)", result.OverallScore)
	}
	if result.OverallLevel != domain.SeverityCritical {
		t.Errorf("overall level = %s, want critical", result.OverallLevel)
	}

	severityByID := map[string]domain.Severity{
		"CR-SEC-001:.github_workflows_ci.yml:3":    domain.SeverityMedium,
		"CR-DATA-001:db_migrations_004_drop.sql:2": domain.SeverityMedium,
		"CR-API-001:pkg_api_user.go:12":            domain.SeverityMedium,
		"CR-REL-001:client_http.go:5":              domain.SeverityLow,
		"CR-CON-001:worker_pool.go:9":              domain.SeverityLow,
		"CR-SC-001:deploy_compose.yaml:4":          domain.SeverityLow,
		"CR-TEST-001:db_migrations_004_drop.sql:0": domain.SeverityLow,
	}
	for _, finding := range result.Findings {
		wantSeverity := severityByID[finding.ID]
		if finding.Severity != wantSeverity {
			t.Errorf("finding %s severity = %s, want %s", finding.ID, finding.Severity, wantSeverity)
		}
		if finding.EvidenceStatus != domain.EvidenceConfirmed {
			t.Errorf("finding %s evidence_status = %s, want confirmed", finding.ID, finding.EvidenceStatus)
		}
		if finding.Source != domain.SourceDeterministic {
			t.Errorf("finding %s source = %s, want deterministic", finding.ID, finding.Source)
		}
		if len(finding.RuleIDs) != 1 || finding.RuleIDs[0] != strings.SplitN(finding.ID, ":", 2)[0] {
			t.Errorf("finding %s has unexpected rule_ids %v", finding.ID, finding.RuleIDs)
		}
		wantInline := finding.ID != "CR-TEST-001:db_migrations_004_drop.sql:0"
		if finding.InlineEligible != wantInline {
			t.Errorf("finding %s inline_eligible = %v, want %v", finding.ID, finding.InlineEligible, wantInline)
		}
		if finding.Title == "" || finding.Impact == "" || finding.Recommendation == "" {
			t.Errorf("finding %s has empty title/impact/recommendation", finding.ID)
		}
	}

	// 排序稳定性：结果已按 domain.SortFindings 排序（再次排序应完全一致）。
	sortedCopy := append([]domain.Finding(nil), result.Findings...)
	domain.SortFindings(sortedCopy)
	if !reflect.DeepEqual(sortedCopy, result.Findings) {
		t.Fatal("findings are not in domain.SortFindings order")
	}
	sortedDims := append([]domain.RiskDimension(nil), result.Dimensions...)
	domain.SortDimensions(sortedDims)
	if !reflect.DeepEqual(sortedDims, result.Dimensions) {
		t.Fatal("dimensions are not in domain.SortDimensions order")
	}
}

func TestEvaluateEmptyInput(t *testing.T) {
	for name, signals := range map[string][]domain.RiskSignal{
		"nil slice":   nil,
		"empty slice": {},
	} {
		result, err := Evaluate(signals)
		if err != nil {
			t.Fatalf("%s: evaluate failed: %v", name, err)
		}
		if result.OverallScore != 0 || result.OverallLevel != domain.SeverityLow {
			t.Errorf("%s: got score=%d level=%s, want 0/low", name, result.OverallScore, result.OverallLevel)
		}
		if len(result.Findings) != 0 {
			t.Errorf("%s: expected no findings, got %+v", name, result.Findings)
		}
		if len(result.Dimensions) != 0 {
			t.Errorf("%s: expected no dimensions, got %+v", name, result.Dimensions)
		}
		if result.ShouldBlock {
			t.Errorf("%s: empty input must not block merge", name)
		}
		if result.GateReason == "" {
			t.Errorf("%s: gate reason must not be empty", name)
		}
	}
}

func TestEvaluateRejectsInvalidEvidenceWithRuleIDAndPath(t *testing.T) {
	cases := []struct {
		name         string
		signal       domain.RiskSignal
		wantContains []string
	}{
		{
			name: "empty evidence file",
			signal: func() domain.RiskSignal {
				s := buildSignal("CR-SEC-002", domain.CategorySecurity, 30, 0.85, "", domain.SideRight, 2, 2)
				return s
			}(),
			wantContains: []string{"CR-SEC-002", `file ""`},
		},
		{
			name:         "zero line on right side",
			signal:       buildSignal("CR-EXEC-001", domain.CategorySecurity, 30, 0.75, "src/cmd.go", domain.SideRight, 0, 0),
			wantContains: []string{"CR-EXEC-001", "src/cmd.go"},
		},
		{
			name: "no evidence at all",
			signal: func() domain.RiskSignal {
				s := buildSignal("CR-REL-001", domain.CategoryReliability, 20, 0.8, "client/http.go", domain.SideRight, 5, 5)
				s.Evidence = nil
				return s
			}(),
			wantContains: []string{"CR-REL-001"},
		},
		{
			name:         "weight above spec maximum",
			signal:       buildSignal("CR-DATA-001", domain.CategoryData, 41, 0.85, "db/mig.sql", domain.SideRight, 1, 1),
			wantContains: []string{"CR-DATA-001", "1-40"},
		},
		{
			name:         "weight below spec minimum",
			signal:       buildSignal("CR-CON-001", domain.CategoryConcurrency, 0, 0.65, "w/pool.go", domain.SideRight, 1, 1),
			wantContains: []string{"CR-CON-001"},
		},
	}
	for _, tc := range cases {
		_, err := Evaluate([]domain.RiskSignal{tc.signal})
		if err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
			continue
		}
		for _, want := range tc.wantContains {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: error %q does not contain %q", tc.name, err.Error(), want)
			}
		}
	}
}

func TestEvaluateThresholdBoundariesMatchLevelFromScore(t *testing.T) {
	cases := []struct {
		name      string
		weights   map[domain.RiskCategory]int
		wantScore int
	}{
		{name: "24 stays low", weights: map[domain.RiskCategory]int{domain.CategoryReliability: 12, domain.CategoryConcurrency: 12}, wantScore: 24},
		{name: "25 becomes medium", weights: map[domain.RiskCategory]int{domain.CategoryAPI: 25}, wantScore: 25},
		{name: "49 stays medium", weights: map[domain.RiskCategory]int{domain.CategoryReliability: 24, domain.CategoryAPI: 25}, wantScore: 49},
		{name: "50 becomes high", weights: map[domain.RiskCategory]int{domain.CategoryData: 25, domain.CategoryAPI: 25}, wantScore: 50},
		{name: "74 stays high", weights: map[domain.RiskCategory]int{domain.CategoryData: 37, domain.CategoryAPI: 37}, wantScore: 74},
		{name: "75 becomes critical", weights: map[domain.RiskCategory]int{domain.CategorySecurity: 40, domain.CategoryData: 35}, wantScore: 75},
	}
	for _, tc := range cases {
		signals := make([]domain.RiskSignal, 0, len(tc.weights))
		files := map[domain.RiskCategory]string{
			domain.CategorySecurity:    "sec/a.yml",
			domain.CategoryData:        "data/drop.sql",
			domain.CategoryAPI:         "api/user.go",
			domain.CategoryReliability: "rel/http.go",
			domain.CategoryConcurrency: "con/pool.go",
		}
		for category, weight := range tc.weights {
			signals = append(signals, buildSignal("CR-BOUNDARY", category, weight, 1.0, files[category], domain.SideRight, 1, 1))
		}
		result, err := Evaluate(signals)
		if err != nil {
			t.Fatalf("%s: evaluate failed: %v", tc.name, err)
		}
		if result.OverallScore != tc.wantScore {
			t.Errorf("%s: overall score = %d, want %d", tc.name, result.OverallScore, tc.wantScore)
		}
		if want := domain.LevelFromScore(tc.wantScore); result.OverallLevel != want {
			t.Errorf("%s: overall level = %s, want %s (LevelFromScore(%d))", tc.name, result.OverallLevel, want, tc.wantScore)
		}
		for _, dim := range result.Dimensions {
			if want := domain.LevelFromScore(dim.Score); dim.Level != want {
				t.Errorf("%s: dimension %s level = %s, want %s", tc.name, dim.Category, dim.Level, want)
			}
		}
	}
}

func TestEvaluateStableUnderRepetitionAndPermutation(t *testing.T) {
	signals := buildMultiCategorySignals()
	first, err := Evaluate(signals)
	if err != nil {
		t.Fatalf("first evaluate failed: %v", err)
	}
	second, err := Evaluate(signals)
	if err != nil {
		t.Fatalf("second evaluate failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repeated evaluation produced different results")
	}

	reversed := append([]domain.RiskSignal(nil), signals...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	fromReversed, err := Evaluate(reversed)
	if err != nil {
		t.Fatalf("reversed evaluate failed: %v", err)
	}
	if !reflect.DeepEqual(first, fromReversed) {
		t.Fatal("input order changed evaluation output")
	}
}

func TestEvaluateDefaultGateNeverBlocksAndMitigationNeutral(t *testing.T) {
	inputs := [][]domain.RiskSignal{
		nil,
		{buildSignal("CR-SEC-001", domain.CategorySecurity, 30, 1.0, "a.yml", domain.SideRight, 1, 1)},
		buildMultiCategorySignals(),
	}
	var reasons []string
	for i, signals := range inputs {
		result, err := Evaluate(signals)
		if err != nil {
			t.Fatalf("case %d: evaluate failed: %v", i, err)
		}
		if result.ShouldBlock {
			t.Errorf("case %d: default gate blocked merge", i)
		}
		if result.GateReason == "" {
			t.Errorf("case %d: gate reason empty", i)
		}
		reasons = append(reasons, result.GateReason)
	}
	for i := 1; i < len(reasons); i++ {
		if reasons[i] != reasons[0] {
			t.Errorf("gate reason differs between runs: %q vs %q", reasons[0], reasons[i])
		}
	}

	// mitigation_credit 数值目录未定义：MitigationIDs 不改变当前分数。
	withMitigation := buildSignal("CR-SEC-001", domain.CategorySecurity, 30, 1.0, "a.yml", domain.SideRight, 1, 1)
	withMitigation.MitigationIDs = []string{"mit-test-added"}
	base, err := Evaluate([]domain.RiskSignal{buildSignal("CR-SEC-001", domain.CategorySecurity, 30, 1.0, "a.yml", domain.SideRight, 1, 1)})
	if err != nil {
		t.Fatalf("base evaluate failed: %v", err)
	}
	mitigated, err := Evaluate([]domain.RiskSignal{withMitigation})
	if err != nil {
		t.Fatalf("mitigated evaluate failed: %v", err)
	}
	if base.OverallScore != mitigated.OverallScore || base.OverallLevel != mitigated.OverallLevel {
		t.Fatalf("mitigation ids unexpectedly changed score: %d/%s vs %d/%s",
			base.OverallScore, base.OverallLevel, mitigated.OverallScore, mitigated.OverallLevel)
	}
}
