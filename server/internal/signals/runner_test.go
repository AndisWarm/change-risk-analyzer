package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type stubAnalyzer struct {
	signals []domain.RiskSignal
	err     error
	calls   int
}

func (s *stubAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.signals, nil
}

func buildRunnerFixtureChangeSet() domain.ChangeSet {
	workflowPatch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,1 +1,4 @@
 name: CI
+permissions:
+  contents: write
+- uses: actions/checkout@v4
`
	userPatch := `--- a/pkg/api/user.go
+++ b/pkg/api/user.go
@@ -1,3 +1,5 @@
 package api
-func FetchUser(id string) (User, error) {
+func fetchUser(id string) (User, error) {
+	resp, err := http.Get("https://example.com")
+	go save(resp)
`
	toolPatch := `--- a/cmd/tool/main.go
+++ b/cmd/tool/main.go
@@ -1,1 +1,3 @@
 package main
+name := readInput()
+exec.Command(name)
`
	migrationPatch := `--- a/db/migrations/004_drop.sql
+++ b/db/migrations/004_drop.sql
@@ -1,1 +1,2 @@
 -- migration
+DROP TABLE users;
`
	routerPatch := `--- a/server/router.go
+++ b/server/router.go
@@ -1,3 +1,4 @@
 package server
-router.Use(AuthMiddleware())
+router.Use(LoggerMiddleware())
+cfg.SkipAuth = true
`
	bootstrapPatch := `--- a/scripts/bootstrap.sh
+++ b/scripts/bootstrap.sh
@@ -1,1 +1,2 @@
 set -eu
+deploy_token="ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
`
	newFile := func(path, patch string, additions, deletions int) domain.ChangedFile {
		return domain.ChangedFile{
			NewPath:   path,
			Status:    domain.FileModified,
			Additions: additions,
			Deletions: deletions,
			Changes:   additions + deletions,
			Patch:     &patch,
		}
	}
	return domain.ChangeSet{
		Files: []domain.ChangedFile{
			newFile(".github/workflows/ci.yml", workflowPatch, 3, 0),
			newFile("pkg/api/user.go", userPatch, 3, 1),
			newFile("cmd/tool/main.go", toolPatch, 2, 0),
			newFile("db/migrations/004_drop.sql", migrationPatch, 1, 0),
			newFile("server/router.go", routerPatch, 2, 1),
			newFile("scripts/bootstrap.sh", bootstrapPatch, 1, 0),
		},
		TotalFiles:     6,
		TotalAdditions: 12,
		TotalDeletions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
}

var wantRunnerRuleIDs = []string{
	WorkflowPermissionRuleID,
	ExportedAPIChangeRuleID,
	UntrustedCommandExecutionRuleID,
	DestructiveMigrationRuleID,
	ExternalRequestWithoutTimeoutRuleID,
	GoroutineLifecycleRuleID,
	FloatingReferenceRuleID,
	SecretLiteralRuleID,
	AuthorizationBoundaryRuleID,
	TestEvidenceRuleID,
}

func TestRunnerDefaultAggregatesAllRulesStably(t *testing.T) {
	changeSet := buildRunnerFixtureChangeSet()
	first, err := DefaultRunner().Run(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	second, err := DefaultRunner().Run(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("runner output is not stable across runs")
	}
	seen := make(map[string]int)
	for _, signal := range first {
		if err := signal.Validate(); err != nil {
			t.Fatalf("signal violates domain invariants: %v", err)
		}
		seen[signal.RuleID]++
	}
	for _, id := range wantRunnerRuleIDs {
		if seen[id] == 0 {
			t.Errorf("rule %s missing from aggregated output", id)
		}
	}
	if len(first) != len(wantRunnerRuleIDs) {
		t.Fatalf("expected %d aggregated signals, got %d", len(wantRunnerRuleIDs), len(first))
	}
	for i := 1; i < len(first); i++ {
		if runnerSortLess(runnerSortKey(first[i]), runnerSortKey(first[i-1])) {
			t.Fatalf("signals not stably sorted at index %d: %+v before %+v", i, first[i-1], first[i])
		}
	}
}

func runnerSortLess(left, right runnerSortKeyStruct) bool {
	if left.category != right.category {
		return left.category < right.category
	}
	if left.file != right.file {
		return left.file < right.file
	}
	if left.line != right.line {
		return left.line < right.line
	}
	if left.side != right.side {
		return left.side < right.side
	}
	if left.ruleID != right.ruleID {
		return left.ruleID < right.ruleID
	}
	return left.fact < right.fact
}

func TestRunnerDeduplicatesIdenticalSignals(t *testing.T) {
	duplicated := []domain.RiskSignal{{
		RuleID:     "CR-TEST-RUNNER",
		Category:   domain.CategoryReliability,
		Fact:       "重复信号样例",
		Evidence:   []domain.Evidence{{File: "a.go", StartLine: 1, EndLine: 1, Side: domain.SideRight, Fact: "样例"}},
		Source:     domain.SourceDeterministic,
		Confidence: 0.5,
		Weight:     10,
	}}
	stub := &stubAnalyzer{signals: []domain.RiskSignal{duplicated[0], duplicated[0]}}
	runner, err := NewRunner(NamedAnalyzer{ID: "STUB-DUP", Analyzer: stub})
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	signals, err := runner.Run(context.Background(), domain.ChangeSet{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("dedup failed: got %d signals, want 1: %+v", len(signals), signals)
	}
}

func TestRunnerPropagatesAnalyzerErrorWithRuleID(t *testing.T) {
	counting := &stubAnalyzer{}
	runner, err := NewRunner(
		NamedAnalyzer{ID: "FAKE-BROKEN", Analyzer: &stubAnalyzer{err: errors.New("boom")}},
		NamedAnalyzer{ID: "COUNTING", Analyzer: counting},
	)
	if err != nil {
		t.Fatalf("NewRunner failed: %v", err)
	}
	signals, runErr := runner.Run(context.Background(), domain.ChangeSet{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA})
	if runErr == nil {
		t.Fatal("analyzer error was swallowed")
	}
	if !strings.Contains(runErr.Error(), "FAKE-BROKEN") {
		t.Fatalf("error does not identify failing analyzer: %v", runErr)
	}
	if len(signals) != 0 {
		t.Fatalf("expected no partial signals on error, got %+v", signals)
	}
	if counting.calls != 0 {
		t.Fatalf("analyzers after failure still ran: %d calls", counting.calls)
	}
}

func TestRunnerCanceledContextAndEmptyChangeSet(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DefaultRunner().Run(canceled, buildRunnerFixtureChangeSet()); err == nil {
		t.Fatal("canceled context was ignored")
	}

	empty := domain.ChangeSet{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA}
	signals, err := DefaultRunner().Run(context.Background(), empty)
	if err != nil {
		t.Fatalf("empty change set run failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("empty change set produced signals: %+v", signals)
	}
}

func TestRunnerGoldenFixtureAgainstTestdata(t *testing.T) {
	signals, err := DefaultRunner().Run(context.Background(), buildRunnerFixtureChangeSet())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	data, err := json.MarshalIndent(signals, "", "  ")
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	data = append(data, '\n')

	goldenPath := filepath.Join("testdata", "golden_runner.json")
	if os.Getenv("GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(goldenPath, data, 0o644); err != nil {
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
	if !reflect.DeepEqual(strings.TrimSpace(string(want)), strings.TrimSpace(string(data))) {
		t.Fatalf("golden mismatch; rerun with GOLDEN_UPDATE=1 after verifying the change is intended")
	}
}
