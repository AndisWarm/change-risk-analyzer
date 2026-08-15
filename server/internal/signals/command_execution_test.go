package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestUntrustedCommandExecutionAnalyzerDetectsDynamicShellInput(t *testing.T) {
	patch := `--- a/internal/runner.go
+++ b/internal/runner.go
@@ -1,1 +1,2 @@
 package runner
+func Run(input string) { exec.Command("sh", "-c", input) }
`
	signals, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), workflowChangeSet("internal/runner.go", patch, 1, 0))
	if err != nil || len(signals) != 1 {
		t.Fatalf("expected one command execution signal, got signals=%+v err=%v", signals, err)
	}
	signal := signals[0]
	if err := signal.Validate(); err != nil {
		t.Fatalf("signal violates domain invariants: %v", err)
	}
	if signal.RuleID != UntrustedCommandExecutionRuleID || signal.Category != domain.CategorySecurity || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 0.75 || signal.Weight != 30 || !strings.Contains(signal.Fact, "动态参数") {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	if len(signal.Evidence) != 1 || signal.Evidence[0].Side != domain.SideRight || signal.Evidence[0].StartLine != 2 {
		t.Fatalf("unexpected command evidence: %+v", signal.Evidence)
	}
}

func TestUntrustedCommandExecutionAnalyzerDetectsContextAndFormattedCommands(t *testing.T) {
	patch := `--- a/internal/runner.go
+++ b/internal/runner.go
@@ -1,1 +1,3 @@
 package runner
+exec.CommandContext(ctx, "bash", "-c", fmt.Sprintf("echo %s", input))
+exec.Command(commandName, "--version")
`
	signals, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), workflowChangeSet("internal/runner.go", patch, 2, 0))
	if err != nil || len(signals) != 2 {
		t.Fatalf("expected two dynamic command signals, got signals=%+v err=%v", signals, err)
	}
	if signals[0].Evidence[0].StartLine != 2 || signals[1].Evidence[0].StartLine != 3 {
		t.Fatalf("signals are not line ordered: %+v", signals)
	}
}

func TestUntrustedCommandExecutionAnalyzerIgnoresStaticAndTextOnlyCommands(t *testing.T) {
	patch := `--- a/internal/runner.go
+++ b/internal/runner.go
@@ -1,1 +1,5 @@
 package runner
+exec.Command("git", "status")
+exec.CommandContext(ctx, "git", "status")
+// exec.Command("sh", "-c", input)
+message := "exec.Command(\"sh\", \"-c\", input)"
`
	signals, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), workflowChangeSet("internal/runner.go", patch, 4, 0))
	if err != nil || len(signals) != 0 {
		t.Fatalf("static or text-only command produced signal: signals=%+v err=%v", signals, err)
	}
}

func TestUntrustedCommandExecutionAnalyzerIgnoresUnsupportedFilesAndDeletedLines(t *testing.T) {
	patch := `--- a/scripts/run.sh
+++ b/scripts/run.sh
@@ -1,1 +1,2 @@
 #!/bin/sh
+sh -c "$INPUT"
`
	for _, file := range []domain.ChangedFile{
		workflowChangeSet("scripts/run.sh", patch, 1, 0).Files[0],
		workflowChangeSet("internal/runner_test.go", patch, 1, 0).Files[0],
		func() domain.ChangedFile {
			file := workflowChangeSet("internal/runner.go", patch, 1, 0).Files[0]
			file.IsBinary = true
			return file
		}(),
		func() domain.ChangedFile {
			file := workflowChangeSet("internal/runner.go", patch, 1, 0).Files[0]
			file.Patch = nil
			return file
		}(),
	} {
		changeSet := workflowWithFile(file)
		signals, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), changeSet)
		if err != nil || len(signals) != 0 {
			t.Errorf("unsupported file produced signal: file=%+v signals=%+v err=%v", file, signals, err)
		}
	}
}

func TestUntrustedCommandExecutionAnalyzerStableAndContextAware(t *testing.T) {
	firstPatch := `--- a/pkg/z.go
+++ b/pkg/z.go
@@ -1,1 +1,2 @@
 package z
+exec.Command(command, "run")
`
	secondPatch := `--- a/pkg/a.go
+++ b/pkg/a.go
@@ -1,1 +1,2 @@
 package a
+exec.Command(command, "run")
`
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			workflowChangeSet("pkg/z.go", firstPatch, 1, 0).Files[0],
			workflowChangeSet("pkg/a.go", secondPatch, 1, 0).Files[0],
		},
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), changeSet)
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
	if _, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestUntrustedCommandExecutionAnalyzerRejectsMalformedPatch(t *testing.T) {
	patch := `--- a/internal/runner.go
+++ b/internal/runner.go
@@ -1 +1 @@
not a hunk line
`
	if _, err := (UntrustedCommandExecutionAnalyzer{}).Analyze(context.Background(), workflowChangeSet("internal/runner.go", patch, 0, 0)); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
