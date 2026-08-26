package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestFloatingReferenceAnalyzerDetectsFloatingActionsAndGoModules(t *testing.T) {
	patch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,2 +1,6 @@
 name: CI
+- uses: actions/checkout@v4
+- uses: actions/setup-go@main
+- uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
+  run: go get example.com/pkg@latest
 jobs: {}
`
	changeSet := workflowChangeSet(".github/workflows/ci.yml", patch, 4, 0)
	signals, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), changeSet)
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
	if signal.RuleID != "CR-SC-001" || signal.Category != domain.CategorySupplyChain || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 0.8 || signal.Weight != 20 {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	wantLines := []int{2, 3, 5}
	for i, evidence := range signal.Evidence {
		if evidence.File != ".github/workflows/ci.yml" || evidence.Side != domain.SideRight ||
			evidence.StartLine != wantLines[i] || evidence.EndLine != wantLines[i] {
			t.Errorf("unexpected evidence[%d]: %+v", i, evidence)
		}
	}
	if !strings.Contains(signal.Evidence[0].Fact, "@v4") {
		t.Errorf("expected major tag fact, got %+v", signal.Evidence[0])
	}
	if !strings.Contains(signal.Evidence[1].Fact, "@main") {
		t.Errorf("expected branch fact, got %+v", signal.Evidence[1])
	}
	if !strings.Contains(signal.Evidence[2].Fact, "@latest") || !strings.Contains(signal.Evidence[2].Fact, "Go 依赖安装命令") {
		t.Errorf("expected go module fact, got %+v", signal.Evidence[2])
	}
}

func TestFloatingReferenceAnalyzerPinnedReferencesAreNotReported(t *testing.T) {
	patch := `--- a/.github/workflows/release.yaml
+++ b/.github/workflows/release.yaml
@@ -1,1 +1,7 @@
 name: Release
+- uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
+- uses: actions/setup-go@v5.0.0
+- uses: ./local-action
+  image: registry.example.com/team/app:1.2.3
+  image: "app@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
+  image: app:v20240115
`
	changeSet := workflowChangeSet(".github/workflows/release.yaml", patch, 6, 0)
	signals, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("pinned references produced signals: %+v", signals)
	}
}

func TestFloatingReferenceAnalyzerDockerfileCases(t *testing.T) {
	patch := `--- a/build/Dockerfile
+++ b/build/Dockerfile
@@ -1,2 +1,8 @@
 ARG BASE=alpine
+FROM nginx:latest
+FROM myreg.example.com/team/app
+FROM golang:1.22 AS build
+FROM build AS release
+FROM scratch
+RUN go get example.com/cli@master
+CMD ["/bin/sh"]
`
	changeSet := workflowChangeSet("build/Dockerfile", patch, 6, 0)
	signals, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1: %+v", len(signals), signals)
	}
	if len(signals[0].Evidence) != 3 {
		t.Fatalf("expected three evidence items, got %+v", signals[0].Evidence)
	}
	wantLines := []int{2, 3, 7}
	for i, evidence := range signals[0].Evidence {
		if evidence.StartLine != wantLines[i] {
			t.Errorf("evidence[%d].StartLine=%d, want %d", i, evidence.StartLine, wantLines[i])
		}
	}
	if !strings.Contains(signals[0].Evidence[0].Fact, "latest") {
		t.Errorf("expected latest-tag fact, got %+v", signals[0].Evidence[0])
	}
	if !strings.Contains(signals[0].Evidence[1].Fact, "未指定标签") {
		t.Errorf("expected implicit-latest fact, got %+v", signals[0].Evidence[1])
	}
}

func TestFloatingReferenceAnalyzerYamlImagesAndScripts(t *testing.T) {
	composePatch := `--- a/deploy/docker-compose.yml
+++ b/deploy/docker-compose.yml
@@ -1,2 +1,4 @@
 services:
+  web:
+    image: redis
`
	scriptPatch := `--- a/scripts/install.sh
+++ b/scripts/install.sh
@@ -1,1 +1,2 @@
 set -eu
+go install example.com/tools/tool@main
`
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: "deploy/docker-compose.yml", Status: domain.FileModified, Additions: 2, Changes: 2, Patch: &composePatch},
			{NewPath: "scripts/install.sh", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &scriptPatch},
		},
		TotalFiles:     2,
		TotalAdditions: 3,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	signals, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 2 {
		t.Fatalf("got %d signals, want 2: %+v", len(signals), signals)
	}
	if signals[0].Evidence[0].File != "deploy/docker-compose.yml" || signals[0].Evidence[0].StartLine != 3 {
		t.Errorf("unexpected compose evidence: %+v", signals[0].Evidence[0])
	}
	if signals[1].Evidence[0].File != "scripts/install.sh" || signals[1].Evidence[0].StartLine != 2 {
		t.Errorf("unexpected script evidence: %+v", signals[1].Evidence[0])
	}
	if !strings.Contains(signals[1].Evidence[0].Fact, "@main") {
		t.Errorf("expected branch-ref fact, got %+v", signals[1].Evidence[0])
	}
}

func TestFloatingReferenceAnalyzerIgnoresCommentsDeletedSideAndUnavailablePatches(t *testing.T) {
	patch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,3 +1,3 @@
 name: CI
-  image: redis:latest
+# - uses: actions/checkout@v4
`
	changeSet := workflowChangeSet(".github/workflows/ci.yml", patch, 1, 1)
	signals, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("comments or deletions produced signals: signals=%+v err=%v", signals, err)
	}

	noPatch := workflowChangeSet(".github/workflows/ci.yml", "", 0, 0)
	noPatch.Files[0].Patch = nil
	signals, err = (FloatingReferenceAnalyzer{}).Analyze(context.Background(), noPatch)
	if err != nil || len(signals) != 0 {
		t.Fatalf("missing patch produced signals: signals=%+v err=%v", signals, err)
	}

	binary := workflowChangeSet("build/Dockerfile", patch, 1, 1)
	binary.Files[0].IsBinary = true
	signals, err = (FloatingReferenceAnalyzer{}).Analyze(context.Background(), binary)
	if err != nil || len(signals) != 0 {
		t.Fatalf("binary file produced signals: signals=%+v err=%v", signals, err)
	}

	unrelated := workflowChangeSet("src/main.go", patch, 1, 1)
	signals, err = (FloatingReferenceAnalyzer{}).Analyze(context.Background(), unrelated)
	if err != nil || len(signals) != 0 {
		t.Fatalf("unrelated path produced signals: signals=%+v err=%v", signals, err)
	}

	traversal := workflowChangeSet("../outside/Dockerfile", patch, 1, 1)
	signals, err = (FloatingReferenceAnalyzer{}).Analyze(context.Background(), traversal)
	if err != nil || len(signals) != 0 {
		t.Fatalf("traversal path produced signals: signals=%+v err=%v", signals, err)
	}
}

func TestFloatingReferenceAnalyzerStableOrderingAndFailures(t *testing.T) {
	zPatch := `--- a/.github/workflows/z.yml
+++ b/.github/workflows/z.yml
@@ -1,1 +1,2 @@
 name: Z
+- uses: actions/cache@v4
`
	aPatch := `--- a/.github/workflows/a.yml
+++ b/.github/workflows/a.yml
@@ -1,1 +1,2 @@
 name: A
+- uses: actions/upload-artifact@master
`
	combined := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: ".github/workflows/z.yml", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &zPatch},
			{NewPath: ".github/workflows/a.yml", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &aPatch},
		},
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}
	if first[0].Evidence[0].File != ".github/workflows/a.yml" {
		t.Fatalf("signals are not stably sorted: %+v", first)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (FloatingReferenceAnalyzer{}).Analyze(canceled, combined); err == nil {
		t.Fatal("canceled context was ignored")
	}

	invalid := combined
	invalid.HeadSHA = "not-a-sha"
	if _, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), invalid); err == nil {
		t.Fatal("invalid change set was accepted")
	}

	malformed := workflowChangeSet(".github/workflows/bad.yml",
		"--- a/.github/workflows/bad.yml\n+++ b/.github/workflows/bad.yml\n@@ -1,1 +1,2 @@\nx bad line\n", 1, 0)
	if _, err := (FloatingReferenceAnalyzer{}).Analyze(context.Background(), malformed); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
