package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"testing"
)

func TestGoroutineLifecycleAnalyzerDetectsGoroutinesWithoutVisibleLifecycle(t *testing.T) {
	patch := `--- a/service/worker.go
+++ b/service/worker.go
@@ -1,1 +1,4 @@
 package service
+go worker()
+go runner.Process()
+go func() { process() }()
`
	signals, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/worker.go", patch, 3, 0))
	if err != nil || len(signals) != 3 {
		t.Fatalf("expected three signals, got signals=%+v err=%v", signals, err)
	}
	for index, signal := range signals {
		if err := signal.Validate(); err != nil {
			t.Fatalf("signal[%d] violates domain invariants: %v", index, err)
		}
		if signal.RuleID != GoroutineLifecycleRuleID || signal.Category != domain.CategoryConcurrency || signal.Source != domain.SourceDeterministic {
			t.Errorf("unexpected signal[%d] identity: %+v", index, signal)
		}
		if signal.Confidence != 0.65 || signal.Weight != 20 || len(signal.Evidence) != 1 {
			t.Errorf("unexpected signal[%d] metadata: %+v", index, signal)
		}
		if signal.Evidence[0].File != "service/worker.go" || signal.Evidence[0].Side != domain.SideRight || signal.Evidence[0].StartLine != index+2 {
			t.Errorf("unexpected signal[%d] evidence: %+v", index, signal.Evidence)
		}
	}
}

func TestGoroutineLifecycleAnalyzerAvoidsVisibleLifecycleSignals(t *testing.T) {
	patch := `--- a/service/worker.go
+++ b/service/worker.go
@@ -1,1 +1,10 @@
 package service
+go worker(ctx)
+go worker(done)
+go func() { defer wg.Done(); <-ctx.Done() }()
+go func() {
+    defer waitGroup.Done()
+    select {
+    case <-stop:
+        return
+    }
+}()
`
	signals, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/worker.go", patch, 9, 0))
	if err != nil || len(signals) != 0 {
		t.Fatalf("visible lifecycle signals produced signals=%+v err=%v", signals, err)
	}
}

func TestGoroutineLifecycleAnalyzerIgnoresBoundaryInputs(t *testing.T) {
	patch := `--- a/service/worker.go
+++ b/service/worker.go
@@ -1,2 +1,1 @@
 package service
-go worker()
`
	file := workflowChangeSet("service/worker.go", patch, 0, 1).Files[0]
	for _, candidate := range []domain.ChangedFile{
		file,
		func() domain.ChangedFile {
			copy := file
			copy.NewPath = "service/worker_test.go"
			return copy
		}(),
		func() domain.ChangedFile {
			copy := file
			copy.NewPath = "service/worker.txt"
			return copy
		}(),
		func() domain.ChangedFile {
			copy := file
			copy.IsBinary = true
			return copy
		}(),
		func() domain.ChangedFile {
			copy := file
			copy.Patch = nil
			return copy
		}(),
	} {
		signals, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), workflowWithFile(candidate))
		if err != nil || len(signals) != 0 {
			t.Errorf("boundary input produced signals: file=%+v signals=%+v err=%v", candidate, signals, err)
		}
	}
}

func TestGoroutineLifecycleAnalyzerIgnoresCommentsAndStrings(t *testing.T) {
	patch := `--- a/service/worker.go
+++ b/service/worker.go
@@ -1,1 +1,4 @@
 package service
+// go worker()
+message := "go worker()"
+prefix := ` + "`" + `go func() { process() }()` + "`" + `
`
	signals, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/worker.go", patch, 3, 0))
	if err != nil || len(signals) != 0 {
		t.Fatalf("comments or strings produced signals=%+v err=%v", signals, err)
	}
}

func TestGoroutineLifecycleAnalyzerStableAndContextAware(t *testing.T) {
	patchA := `--- a/service/a.go
+++ b/service/a.go
@@ -1,1 +1,2 @@
 package service
+go worker()
`
	patchZ := `--- a/service/z.go
+++ b/service/z.go
@@ -1,1 +1,2 @@
 package service
+go func() { process() }()
`
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			workflowChangeSet("service/z.go", patchZ, 1, 0).Files[0],
			workflowChangeSet("service/a.go", patchA, 1, 0).Files[0],
		},
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 || first[0].Evidence[0].File != "service/a.go" {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (GoroutineLifecycleAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestGoroutineLifecycleAnalyzerRejectsMalformedPatch(t *testing.T) {
	patch := `--- a/service/worker.go
+++ b/service/worker.go
@@ -1 +1 @@
not a hunk line
`
	if _, err := (GoroutineLifecycleAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/worker.go", patch, 0, 0)); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
