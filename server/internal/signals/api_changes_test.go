package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestExportedAPIAnalyzerDetectsDeletedFunction(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1,2 +1,1 @@
 package api
-func Public(input string) error { return nil }
`
	signals, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet("pkg/api.go", patch, 0, 1))
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
	if signal.RuleID != ExportedAPIChangeRuleID || signal.Category != domain.CategoryAPI || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 0.9 || signal.Weight != 25 || !strings.Contains(signal.Fact, "删除") {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	if len(signal.Evidence) != 1 || signal.Evidence[0].Side != domain.SideLeft || signal.Evidence[0].StartLine != 2 {
		t.Fatalf("unexpected deletion evidence: %+v", signal.Evidence)
	}
}

func TestExportedAPIAnalyzerDetectsSignatureChange(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1,2 +1,2 @@
 package api
-func (s *Server) Public(input string) error { return nil }
+func (s *Server) Public(input int) error { return nil }
`
	signals, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet("pkg/api.go", patch, 1, 1))
	if err != nil || len(signals) != 1 {
		t.Fatalf("expected one signature signal, got signals=%+v err=%v", signals, err)
	}
	signal := signals[0]
	if !strings.Contains(signal.Fact, "签名") || len(signal.Evidence) != 2 {
		t.Fatalf("unexpected signature signal: %+v", signal)
	}
	if signal.Evidence[0].Side != domain.SideLeft || signal.Evidence[1].Side != domain.SideRight {
		t.Fatalf("signature evidence sides are not stable: %+v", signal.Evidence)
	}
	for _, evidence := range signal.Evidence {
		if evidence.StartLine != 2 || evidence.EndLine != 2 {
			t.Errorf("unexpected signature evidence line: %+v", evidence)
		}
	}
}

func TestExportedAPIAnalyzerDetectsTypeVarAndConstRemoval(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1,4 +1,1 @@
 package api
-type Public struct{}
-var PublicValue = 1
-const PublicConst = 2
`
	signals, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet("pkg/api.go", patch, 0, 3))
	if err != nil || len(signals) != 3 {
		t.Fatalf("expected three declaration signals, got signals=%+v err=%v", signals, err)
	}
	want := []string{"type", "var", "const"}
	for i, signal := range signals {
		if !strings.Contains(signal.Fact, want[i]) {
			t.Errorf("signal[%d] fact=%q, want %q", i, signal.Fact, want[i])
		}
	}
}

func TestExportedAPIAnalyzerIgnoresAdditiveAndNonPublicChanges(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1,3 +1,5 @@
 package api
 // comment mentioning func Public
-func private(input string) error { return nil }
+func private(input int) error { return nil }
+func Added(input string) error { return nil }
+const message = "func Public"
`
	for _, filePath := range []string{"pkg/api.go", "internal/api.go", "pkg/api_test.go", "vendor/api.go", "../pkg/api.go"} {
		signals, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet(filePath, patch, 3, 1))
		if err != nil {
			t.Fatalf("analyzer failed for %s: %v", filePath, err)
		}
		if len(signals) != 0 {
			t.Errorf("got false positive for %s: %+v", filePath, signals)
		}
	}
}

func TestExportedAPIAnalyzerDetectsGenericFunctionSignatureChange(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1,2 +1,2 @@
 package api
-func Public[T any](input T) T { return input }
+func Public[T any](input string) string { return input }
`
	signals, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet("pkg/api.go", patch, 1, 1))
	if err != nil || len(signals) != 1 {
		t.Fatalf("expected generic signature signal, got signals=%+v err=%v", signals, err)
	}
}

func TestExportedAPIAnalyzerStableAndContextAware(t *testing.T) {
	firstPatch := `--- a/pkg/z.go
+++ b/pkg/z.go
@@ -1,2 +1,1 @@
 package z
-func Zed() {}
`
	secondPatch := `--- a/pkg/a.go
+++ b/pkg/a.go
@@ -1,2 +1,1 @@
 package a
-func Alpha() {}
`
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			workflowChangeSet("pkg/z.go", firstPatch, 0, 1).Files[0],
			workflowChangeSet("pkg/a.go", secondPatch, 0, 1).Files[0],
		},
		TotalFiles:     2,
		TotalDeletions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}
	if first[0].Evidence[0].File != "pkg/a.go" {
		t.Fatalf("signals are not stably sorted: %+v", first)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ExportedAPIAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestExportedAPIAnalyzerRejectsMalformedPatch(t *testing.T) {
	patch := `--- a/pkg/api.go
+++ b/pkg/api.go
@@ -1 +1 @@
not a hunk line
`
	if _, err := (ExportedAPIAnalyzer{}).Analyze(context.Background(), workflowChangeSet("pkg/api.go", patch, 0, 0)); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
