package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestAuthorizationBoundaryAnalyzerDetectsRemovalAndWeakening(t *testing.T) {
	patch := `--- a/server/router.go
+++ b/server/router.go
@@ -1,3 +1,4 @@
 package server
-router.Use(AuthMiddleware())
+router.Use(LoggerMiddleware())
+cfg.SkipAuth = true
`
	changeSet := workflowChangeSet("server/router.go", patch, 2, 1)
	signals, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), changeSet)
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
	if signal.RuleID != "CR-SEC-003" || signal.Category != domain.CategorySecurity || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 0.7 || signal.Weight != 25 {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	if len(signal.Evidence) != 2 {
		t.Fatalf("expected two evidence items, got %+v", signal.Evidence)
	}
	first, second := signal.Evidence[0], signal.Evidence[1]
	if first.Side != domain.SideLeft || first.StartLine != 2 || !strings.Contains(first.Fact, "删除了疑似授权") {
		t.Errorf("unexpected removal evidence: %+v", first)
	}
	if second.Side != domain.SideRight || second.StartLine != 3 || !strings.Contains(second.Fact, "放宽") {
		t.Errorf("unexpected weakening evidence: %+v", second)
	}
}

func TestAuthorizationBoundaryAnalyzerAddedHardeningIsNotReported(t *testing.T) {
	patch := `--- a/server/router.go
+++ b/server/router.go
@@ -1,1 +1,4 @@
 package server
+router.Use(AuthMiddleware())
+if !user.IsAdmin() {
+    deny(w)
`
	changeSet := workflowChangeSet("server/router.go", patch, 3, 0)
	signals, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("added hardening produced signals: %+v", signals)
	}
}

func TestAuthorizationBoundaryAnalyzerIgnoresNoiseAndUnavailableInputs(t *testing.T) {
	commentPatch := `--- a/server/router.go
+++ b/server/router.go
@@ -1,1 +1,3 @@
 package server
+// router.Use(AuthMiddleware()) was considered
+msg := "please skip auth for this route"
`
	changeSet := workflowChangeSet("server/router.go", commentPatch, 2, 0)
	signals, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("comments or strings produced signals: signals=%+v err=%v", signals, err)
	}

	testFilePatch := "--- a/server/router_test.go\n+++ b/server/router_test.go\n@@ -1,1 +1,2 @@\n package server\n-user.IsAdmin()\n"
	testChangeSet := workflowChangeSet("server/router_test.go", testFilePatch, 0, 1)
	signals, err = (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), testChangeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("test file produced signals: signals=%+v err=%v", signals, err)
	}

	vendorPatch := "--- a/vendor/golib/auth.go\n+++ b/vendor/golib/auth.go\n@@ -1,1 +1,2 @@\n package auth\n-func CheckAuth(u User) bool {\n"
	vendorChangeSet := workflowChangeSet("vendor/golib/auth.go", vendorPatch, 0, 1)
	signals, err = (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), vendorChangeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("vendor path produced signals: signals=%+v err=%v", signals, err)
	}

	noPatch := workflowChangeSet("server/router.go", "", 0, 0)
	noPatch.Files[0].Patch = nil
	signals, err = (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), noPatch)
	if err != nil || len(signals) != 0 {
		t.Fatalf("missing patch produced signals: signals=%+v err=%v", signals, err)
	}

	binary := workflowChangeSet("server/router.go", commentPatch, 2, 0)
	binary.Files[0].IsBinary = true
	signals, err = (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), binary)
	if err != nil || len(signals) != 0 {
		t.Fatalf("binary file produced signals: signals=%+v err=%v", signals, err)
	}

	traversal := workflowChangeSet("../outside/router.go", commentPatch, 2, 0)
	signals, err = (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), traversal)
	if err != nil || len(signals) != 0 {
		t.Fatalf("traversal path produced signals: signals=%+v err=%v", signals, err)
	}
}

func TestAuthorizationBoundaryAnalyzerStableOrderingAndFailures(t *testing.T) {
	zPatch := "--- a/api/z/routes.go\n+++ a/api/z/routes.go\n@@ -1,1 +1,2 @@\n z: 1\n-z.CheckPermission(u)\n"
	aPatch := "--- a/api/a/routes.go\n+++ a/api/a/routes.go\n@@ -1,1 +1,2 @@\n a: 1\n+cfg.PermitAll = true\n"
	combined := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: "api/z/routes.go", Status: domain.FileModified, Deletions: 1, Changes: 1, Patch: &zPatch},
			{NewPath: "api/a/routes.go", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &aPatch},
		},
		TotalFiles:     2,
		TotalDeletions: 1,
		TotalAdditions: 1,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}
	if first[0].Evidence[0].File != "api/a/routes.go" {
		t.Fatalf("signals are not stably sorted: %+v", first)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (AuthorizationBoundaryAnalyzer{}).Analyze(canceled, combined); err == nil {
		t.Fatal("canceled context was ignored")
	}

	invalid := combined
	invalid.HeadSHA = "not-a-sha"
	if _, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), invalid); err == nil {
		t.Fatal("invalid change set was accepted")
	}

	malformed := workflowChangeSet("api/bad/routes.go",
		"--- a/api/bad/routes.go\n+++ b/api/bad/routes.go\n@@ -1,1 +1,2 @@\nx bad line\n", 1, 0)
	if _, err := (AuthorizationBoundaryAnalyzer{}).Analyze(context.Background(), malformed); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
