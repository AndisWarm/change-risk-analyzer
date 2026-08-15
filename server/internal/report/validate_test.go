package report

import (
	"change-risk-analyzer/internal/domain"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func validReport() domain.RiskReport {
	return domain.RiskReport{
		SchemaVersion:   SchemaVersion,
		Status:          domain.StatusCompleted,
		GeneratedAt:     time.Date(2026, time.August, 15, 8, 0, 0, 0, time.UTC),
		AnalyzerVersion: "test",
		Request: domain.ReviewRequest{
			Repository: domain.RepositoryRef{
				Owner:    "example-org",
				Name:     "risk-analyzer",
				FullName: "example-org/risk-analyzer",
			},
			PullRequestNumber: 42,
			EventAction:       domain.ActionOpened,
			BaseSHA:           "0123456789abcdef0123456789abcdef01234567",
			HeadSHA:           "abcdef0123456789abcdef0123456789abcdef01",
			SourceKind:        domain.SourceSameRepo,
		},
		ChangeSummary: domain.ChangeSummary{
			FilesSeen: 2, FilesAnalyzed: 2, Additions: 4, Deletions: 1,
		},
		OverallScore:       10,
		OverallLevel:       domain.SeverityLow,
		Dimensions:         []domain.RiskDimension{},
		Findings:           []domain.Finding{},
		TestGaps:           []domain.TestGap{},
		DegradationReasons: []domain.DegradationReason{},
		Runtime: domain.RuntimeMetadata{
			DurationMs: 10, FilesSeen: 2, FilesAnalyzed: 2, PatchBytesSeen: 128,
		},
	}
}

func TestValidateAcceptsSchemaCompliantReport(t *testing.T) {
	report := validReport()
	result, err := Validate(&report)
	if err != nil {
		t.Fatalf("unexpected validation execution error: %v", err)
	}
	if !result.Valid() || !result.DomainOK || !result.SchemaOK {
		t.Fatalf("valid report rejected: %+v", result)
	}
}

func TestValidateSeparatesDomainAndSchemaErrors(t *testing.T) {
	t.Run("domain error", func(t *testing.T) {
		report := validReport()
		report.Request.HeadSHA = "invalid"
		result, err := Validate(&report)
		if err != nil {
			t.Fatalf("unexpected validation execution error: %v", err)
		}
		if result.DomainOK || len(result.DomainErrors) == 0 {
			t.Fatalf("expected domain errors: %+v", result)
		}
	})

	t.Run("schema error", func(t *testing.T) {
		report := validReport()
		report.Findings = []domain.Finding{{
			ID:             "CR-API-001",
			Category:       domain.CategoryAPI,
			Severity:       domain.SeverityLow,
			EvidenceStatus: domain.EvidenceNeedsReview,
			Confidence:     0.2,
			Title:          strings.Repeat("x", 241),
			Impact:         "需要复核",
			Evidence:       []domain.Evidence{{File: "api.go", Side: domain.SideFile, Fact: "变更"}},
			Recommendation: "补充测试",
			RuleIDs:        []string{"CR-API-001"},
			Source:         domain.SourceDeterministic,
		}}
		result, err := Validate(&report)
		if err != nil {
			t.Fatalf("unexpected validation execution error: %v", err)
		}
		if !result.DomainOK || result.SchemaOK || len(result.SchemaErrors) == 0 {
			t.Fatalf("expected schema-only errors: %+v", result)
		}
	})
}

func TestValidateRequiresDegradationReason(t *testing.T) {
	report := validReport()
	report.Status = domain.StatusDegraded
	result, err := Validate(&report)
	if err != nil {
		t.Fatalf("unexpected validation execution error: %v", err)
	}
	if result.DomainOK || result.Valid() {
		t.Fatalf("degraded report without reason was accepted: %+v", result)
	}
}

func TestValidateAgainstSchemaRejectsInvalidJSON(t *testing.T) {
	valid, schemaErrors, err := ValidateAgainstSchema([]byte("not-json"))
	if err == nil || valid || schemaErrors != nil {
		t.Fatalf("unexpected invalid JSON result: valid=%v errors=%v err=%v", valid, schemaErrors, err)
	}
}

func TestValidateAgainstSchemaUsesSerializedReport(t *testing.T) {
	data, err := json.Marshal(validReport())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	valid, schemaErrors, err := ValidateAgainstSchema(data)
	if err != nil || !valid || len(schemaErrors) != 0 {
		t.Fatalf("valid serialized report rejected: valid=%v errors=%v err=%v", valid, schemaErrors, err)
	}
}
