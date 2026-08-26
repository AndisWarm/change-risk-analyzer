package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestEvidenceAnalyzerDetectsUncoveredSensitiveChanges(t *testing.T) {
	migrationPatch := "--- a/db/migrations/002_add_index.sql\n+++ b/db/migrations/002_add_index.sql\n@@ -1,1 +1,2 @@\n SELECT 1;\n+CREATE INDEX idx_users_email ON users(email);\n"
	authPatch := "--- a/internal/auth/middleware.go\n+++ b/internal/auth/middleware.go\n@@ -1,1 +1,2 @@\n package auth\n+func Wrap(h Handler) Handler { return h }\n"
	readmePatch := "--- a/README.md\n+++ b/README.md\n@@ -1,1 +1,2 @@\n # Demo\n+more docs\n"
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: "db/migrations/002_add_index.sql", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &migrationPatch},
			{NewPath: "internal/auth/middleware.go", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &authPatch},
			{NewPath: "README.md", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &readmePatch},
		},
		TotalFiles:     3,
		TotalAdditions: 3,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	signals, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2: %+v", len(signals), signals)
	}
	for _, signal := range signals {
		if err := signal.Validate(); err != nil {
			t.Fatalf("signal violates domain invariants: %v", err)
		}
		if signal.RuleID != "CR-TEST-001" || signal.Category != domain.CategoryTestability || signal.Source != domain.SourceDeterministic {
			t.Fatalf("unexpected signal identity: %+v", signal)
		}
		if signal.Confidence != 0.6 || signal.Weight != 15 {
			t.Fatalf("unexpected signal metadata: %+v", signal)
		}
	}
	if signals[0].Evidence[0].File != "db/migrations/002_add_index.sql" {
		t.Errorf("expected db group first, got %+v", signals[0].Evidence[0])
	}
	if signals[1].Evidence[0].File != "internal/auth/middleware.go" {
		t.Errorf("expected internal group second, got %+v", signals[1].Evidence[0])
	}
	for _, signal := range signals {
		if !strings.Contains(signal.Evidence[0].Fact, "未观察到") || strings.Contains(signal.Evidence[0].Fact, "没有测试") {
			t.Errorf("fact wording must stay observational: %+v", signal.Evidence[0])
		}
	}
}

func TestEvidenceAnalyzerCoveredSensitiveChangesAreNotReported(t *testing.T) {
	migrationPatch := "--- a/db/migrations/003.sql\n+++ b/db/migrations/003.sql\n@@ -1,1 +1,2 @@\n SELECT 1;\n+ALTER TABLE users ADD COLUMN bio TEXT;\n"
	migrationTestPatch := "--- a/db/migrations_test.go\n+++ b/db/migrations_test.go\n@@ -1,1 +1,2 @@\n package db\n+func TestMigration003(t *testing.T) {}\n"
	authPatch := "--- a/internal/auth/helper.go\n+++ b/internal/auth/helper.go\n@@ -1,1 +1,2 @@\n package auth\n+func Hash(p string) string { return p }\n"
	authTestPatch := "--- a/internal/auth/helper_test.go\n+++ b/internal/auth/helper_test.go\n@@ -1,1 +1,2 @@\n package auth\n+func TestHash(t *testing.T) {}\n"
	testsOnlyPatch := "--- a/tests/auth_utils.go\n+++ b/tests/auth_utils.go\n@@ -1,1 +1,2 @@\n package tests\n+var x = 1\n"
	readmePatch := "--- a/README.md\n+++ b/README.md\n@@ -1,1 +1,2 @@\n # Demo\n+docs\n"
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: "db/migrations/003.sql", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &migrationPatch},
			{NewPath: "db/migrations_test.go", Status: domain.FileAdded, Additions: 2, Changes: 2, Patch: &migrationTestPatch},
			{NewPath: "internal/auth/helper.go", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &authPatch},
			{NewPath: "internal/auth/helper_test.go", Status: domain.FileAdded, Additions: 2, Changes: 2, Patch: &authTestPatch},
			{NewPath: "tests/auth_utils.go", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &testsOnlyPatch},
			{NewPath: "README.md", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &readmePatch},
		},
		TotalFiles:     6,
		TotalAdditions: 8,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	signals, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("covered or non-sensitive changes produced signals: %+v", signals)
	}
}

func TestEvidenceAnalyzerBoundariesAndFailures(t *testing.T) {
	deletedMigration := domain.ChangedFile{
		NewPath:   "db/migrations/001_init.sql",
		Status:    domain.FileDeleted,
		Deletions: 5,
		Changes:   5,
	}
	binarySecret := domain.ChangedFile{
		NewPath:   "security/keys.bin",
		Status:    domain.FileModified,
		Additions: 1,
		Changes:   1,
		Patch:     stringPointerForTest("fake"),
		IsBinary:  true,
	}
	traversal := domain.ChangedFile{
		NewPath:   "../outside/payment/charge.go",
		Status:    domain.FileModified,
		Additions: 1,
		Changes:   1,
		Patch:     stringPointerForTest("--- a/x\n+++ b/x\n@@ -1,1 +1,2 @@\n a\n+b\n"),
	}
	changeSet := domain.ChangeSet{
		Files:          []domain.ChangedFile{deletedMigration, binarySecret, traversal},
		TotalFiles:     3,
		TotalDeletions: 5,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	signals, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 1 || signals[0].Evidence[0].File != "db/migrations/001_init.sql" {
		t.Fatalf("expected only the deleted migration to be flagged, got %+v", signals)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (TestEvidenceAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}

	invalid := changeSet
	invalid.HeadSHA = "not-a-sha"
	if _, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), invalid); err == nil {
		t.Fatal("invalid change set was accepted")
	}

	first, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("repeat analysis failed: %v", err)
	}
	second, err := (TestEvidenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("repeat analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("analysis is not idempotent")
	}
}
