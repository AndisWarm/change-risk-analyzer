package report

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"change-risk-analyzer/internal/domain"
	"change-risk-analyzer/internal/policy"
)

func TestRenderMarkdownGoldenFixture(t *testing.T) {
	built, _ := buildSampleReport(t)
	rendered, err := RenderMarkdown(built)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	goldenPath := filepath.Join("testdata", "golden_report.md")
	if os.Getenv("GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create testdata dir failed: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(rendered), 0o644); err != nil {
			t.Fatalf("update golden failed: %v", err)
		}
		t.Logf("golden updated at %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("golden fixture missing; run with GOLDEN_UPDATE=1 to create %s", goldenPath)
	}
	if err != nil {
		t.Fatalf("read golden failed: %v", err)
	}
	if string(want) != rendered {
		t.Fatalf("golden mismatch; rerun with GOLDEN_UPDATE=1 after verifying the change is intended")
	}
}

func TestRenderMarkdownIsByteIdenticalAcrossRenders(t *testing.T) {
	built, _ := buildSampleReport(t)
	first, err := RenderMarkdown(built)
	if err != nil {
		t.Fatalf("first render failed: %v", err)
	}
	second, err := RenderMarkdown(built)
	if err != nil {
		t.Fatalf("second render failed: %v", err)
	}
	if first != second {
		t.Fatal("markdown render is not byte-identical for the same report")
	}
}

func TestRenderMarkdownContainsRequiredSections(t *testing.T) {
	built, _ := buildSampleReport(t)
	rendered, err := RenderMarkdown(built)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, want := range []string{
		"# 变更风险分析报告",
		"总体风险：**high**（70 / 100 分）",
		"`example-org/risk-analyzer` · PR #42",
		"`" + testHeadSHA + "`",
		"2026-08-26T12:00:00Z",
		"## 变更概览",
		"| 文件数（已见 / 已分析） | 6 / 6 |",
		"## 风险维度",
		"| security | 30 | medium | 1 |",
		"| supply_chain | 20 | low | 1 |",
		"## 需要优先确认的发现",
		"CR-SEC-001",
		"`.github/workflows/ci.yml`:3（右侧）",
		"- 建议：",
		"## 建议补充的测试",
		"## 分析范围与降级原因",
		"本次分析完整执行，没有降级事项。",
		"## 输出信息",
		"`risk-report/v1`",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered markdown missing %q\n---\n%s", want, rendered)
		}
	}
}

func TestRenderMarkdownEscapesPipesInTableCells(t *testing.T) {
	result, err := policyEvaluateForEscapeCase()
	if err != nil {
		t.Fatalf("evaluate failed: %v", err)
	}
	dimensions := []domain.RiskDimension{{
		Category:    domain.CategorySecurity,
		Score:       result.OverallScore,
		Level:       domain.LevelFromScore(result.OverallScore),
		SignalCount: 1,
		Summary:     "含竖线的说明：权限 | 扩大 | 风险",
	}}
	findings := []domain.Finding{{
		ID:             "CR-SEC-001:.github-workflows-ci.yml:3",
		Category:       domain.CategorySecurity,
		Severity:       domain.SeverityMedium,
		EvidenceStatus: domain.EvidenceConfirmed,
		Confidence:     0.8,
		Title:          "workflow 权限|write 泄漏候选",
		Impact:         "待复核",
		Evidence: []domain.Evidence{{
			File:      ".github/workflows/ci.yml",
			StartLine: 3,
			EndLine:   4,
			Side:      domain.SideRight,
			Fact:      "新增 contents: write | 行级事实",
		}},
		Recommendation: "复核第 3-4 行",
		RuleIDs:        []string{"CR-SEC-001"},
		Source:         domain.SourceDeterministic,
		InlineEligible: true,
	}}
	built, err := Build(BuilderInput{
		Request:         sampleRequest(),
		Summary:         sampleSummary(),
		Findings:        findings,
		Dimensions:      dimensions,
		OverallScore:    result.OverallScore,
		OverallLevel:    result.OverallLevel,
		AnalyzerVersion: "test-analyzer/0.1.0",
		GeneratedAt:     testGeneratedAt,
		Runtime:         sampleRuntime(),
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	rendered, err := RenderMarkdown(built)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	for _, want := range []string{
		`权限 \| 扩大 \| 风险`,
		`contents: write \| 行级事实`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("table cell pipe not escaped, missing %q\n---\n%s", want, rendered)
		}
	}
	// 标题不在表格内，竖线按原样保留（Markdown 标题中合法），但换行必须被折叠。
	if !strings.Contains(rendered, "workflow 权限|write 泄漏候选") {
		t.Fatalf("heading text altered unexpectedly:\n%s", rendered)
	}
}

// policyEvaluateForEscapeCase 为转义用例提供一个合法的分数/级别组合。
func policyEvaluateForEscapeCase() (policy.Result, error) {
	return policy.Evaluate([]domain.RiskSignal{
		lineSignal("CR-SEC-001", domain.CategorySecurity, 30, ".github/workflows/ci.yml", 3),
	})
}

func TestRenderMarkdownRejectsNilReport(t *testing.T) {
	if rendered, err := RenderMarkdown(nil); err == nil {
		t.Fatalf("expected error for nil report, got %q", rendered)
	}
	if _, err := RenderJSON(nil); err == nil {
		t.Fatal("expected error for nil report JSON render")
	}
}
