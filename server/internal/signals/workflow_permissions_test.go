package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

const (
	testBaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testHeadSHA = "abcdef0123456789abcdef0123456789abcdef01"
)

func workflowChangeSet(path, patch string, additions, deletions int) domain.ChangeSet {
	return domain.ChangeSet{
		Files: []domain.ChangedFile{{
			NewPath:   path,
			Status:    domain.FileModified,
			Additions: additions,
			Deletions: deletions,
			Changes:   additions + deletions,
			Patch:     &patch,
		}},
		TotalFiles:        1,
		TotalAdditions:    additions,
		TotalDeletions:    deletions,
		BaseSHA:           testBaseSHA,
		HeadSHA:           testHeadSHA,
		Truncated:         false,
		TruncationReasons: nil,
	}
}

func TestWorkflowPermissionAnalyzerDetectsWritePermissions(t *testing.T) {
	patch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,2 +1,5 @@
 name: CI
+permissions:
+  contents: write
+  pull-requests: "write"
 jobs: {}
`
	changeSet := workflowChangeSet(".github/workflows/ci.yml", patch, 3, 0)
	signals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1: %+v", len(signals), signals)
	}
	signal := signals[0]
	if err := signal.Validate(); err != nil {
		t.Fatalf("signal violates domain invariants: %v", err)
	}
	if signal.RuleID != "CR-SEC-001" || signal.Category != domain.CategorySecurity || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 1 || signal.Weight != 30 || len(signal.Evidence) != 2 {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	wantLines := []int{3, 4}
	for i, evidence := range signal.Evidence {
		if evidence.File != ".github/workflows/ci.yml" || evidence.Side != domain.SideRight || evidence.StartLine != wantLines[i] || evidence.EndLine != wantLines[i] {
			t.Errorf("unexpected evidence[%d]: %+v", i, evidence)
		}
	}
}

func TestWorkflowPermissionAnalyzerDetectsInlineAndWriteAll(t *testing.T) {
	patch := `--- a/.github/workflows/release.yaml
+++ b/.github/workflows/release.yaml
@@ -1,1 +1,4 @@
 name: Release
+permissions: {contents: "write", packages: read}
+permissions: write-all
`
	changeSet := workflowChangeSet(".github/workflows/release.yaml", patch, 2, 0)
	signals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil || len(signals) != 1 {
		t.Fatalf("expected one inline/write-all signal, got signals=%+v err=%v", signals, err)
	}
	if len(signals[0].Evidence) != 2 {
		t.Fatalf("expected two evidence items, got %+v", signals[0].Evidence)
	}
	if !strings.Contains(signals[0].Evidence[0].Fact, "contents: write") || !strings.Contains(signals[0].Evidence[1].Fact, "write-all") {
		t.Fatalf("unexpected evidence facts: %+v", signals[0].Evidence)
	}
}

func TestWorkflowPermissionAnalyzerDoesNotReportReadOrUnrelatedChanges(t *testing.T) {
	patch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,3 +1,6 @@
 name: CI
+permissions: read-all
+  contents: read
+  mode: write
 jobs: {}
`
	for _, filePath := range []string{".github/workflows/ci.yml", "config/ci.yml", "src/workflow.yml"} {
		changeSet := workflowChangeSet(filePath, patch, 3, 0)
		signals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), changeSet)
		if err != nil {
			t.Fatalf("analyzer failed for %s: %v", filePath, err)
		}
		if len(signals) != 0 {
			t.Errorf("got false positive for %s: %+v", filePath, signals)
		}
	}
}

func TestWorkflowPermissionAnalyzerIgnoresCommentsAndUnavailablePatch(t *testing.T) {
	commentPatch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,1 +1,3 @@
 name: CI
+# permissions: contents: write
+  # contents: write
+message: "permissions: {contents: write}"
`
	changeSet := workflowChangeSet(".github/workflows/ci.yml", commentPatch, 3, 0)
	signals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("comment-only permission text produced a signal: signals=%+v err=%v", signals, err)
	}

	changeSet.Files[0].Patch = nil
	signals, err = (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), workflowWithFile(changeSet.Files[0]))
	if err != nil || len(signals) != 0 {
		t.Fatalf("missing patch produced a signal: signals=%+v err=%v", signals, err)
	}

	changeSet.Files[0].Patch = stringPointerForTest(commentPatch)
	changeSet.Files[0].IsBinary = true
	signals, err = (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), workflowWithFile(changeSet.Files[0]))
	if err != nil || len(signals) != 0 {
		t.Fatalf("binary file produced a signal: signals=%+v err=%v", signals, err)
	}
}

func TestWorkflowPermissionAnalyzerStableAndContextAware(t *testing.T) {
	patch := `--- a/.github/workflows/z.yml
+++ b/.github/workflows/z.yml
@@ -1,1 +1,2 @@
 name: Z
+contents: write
diff --git a/.github/workflows/a.yml b/.github/workflows/a.yml
--- a/.github/workflows/a.yml
+++ b/.github/workflows/a.yml
@@ -1,1 +1,2 @@
 name: A
+actions: write
`
	first := workflowChangeSet(".github/workflows/z.yml", patch[:strings.Index(patch, "diff --git")], 1, 0)
	second := workflowChangeSet(".github/workflows/a.yml", patch[strings.Index(patch, "diff --git"):], 1, 0)
	combined := domain.ChangeSet{
		Files:          append(first.Files, second.Files...),
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	firstSignals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	secondSignals, err := (WorkflowPermissionAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(firstSignals, secondSignals) || len(firstSignals) != 2 {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", firstSignals, secondSignals)
	}
	if firstSignals[0].Evidence[0].File != ".github/workflows/a.yml" {
		t.Fatalf("signals are not stably sorted: %+v", firstSignals)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (WorkflowPermissionAnalyzer{}).Analyze(canceled, combined); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func workflowWithFile(file domain.ChangedFile) domain.ChangeSet {
	return domain.ChangeSet{
		Files:          []domain.ChangedFile{file},
		TotalFiles:     1,
		TotalAdditions: file.Additions,
		TotalDeletions: file.Deletions,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
}

func stringPointerForTest(value string) *string { return &value }
