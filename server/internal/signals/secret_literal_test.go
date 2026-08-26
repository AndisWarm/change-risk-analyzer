package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

const (
	testFakeGitHubToken = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	testFakePassword    = "Tr0ub4dor&3XyZ99"
)

func TestSecretLiteralAnalyzerDetectsKnownFormatsAndPrivateKey(t *testing.T) {
	patch := `--- a/app/config.yaml
+++ b/app/config.yaml
@@ -1,1 +1,6 @@
 app: prod
+github_pat: ` + testFakeGitHubToken + `
+aws_access_key_id: AKIAABCDEFGHIJKLMNOP
+slack: xoxb-123456789012-abcdefgh
+google_api_key = AIzaSyD1234567890abcdefghijklmnopqrstuvwx
+    -----BEGIN RSA PRIVATE KEY-----
`
	changeSet := workflowChangeSet("app/config.yaml", patch, 5, 0)
	signals, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), changeSet)
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
	if signal.RuleID != "CR-SEC-002" || signal.Category != domain.CategorySecurity || signal.Source != domain.SourceDeterministic {
		t.Fatalf("unexpected signal identity: %+v", signal)
	}
	if signal.Confidence != 0.85 || signal.Weight != 30 {
		t.Fatalf("unexpected signal metadata: %+v", signal)
	}
	wantLines := []int{2, 3, 4, 5, 6}
	for i, evidence := range signal.Evidence {
		if evidence.File != "app/config.yaml" || evidence.Side != domain.SideRight ||
			evidence.StartLine != wantLines[i] || evidence.EndLine != wantLines[i] {
			t.Errorf("unexpected evidence[%d]: %+v", i, evidence)
		}
	}
	wantKinds := []string{"GitHub Token", "AWS Access Key ID", "Slack Token", "Google API Key", "私钥块"}
	for i, kind := range wantKinds {
		if !strings.Contains(signal.Evidence[i].Fact, kind) {
			t.Errorf("evidence[%d].Fact=%q, want kind %q", i, signal.Evidence[i].Fact, kind)
		}
	}
}

func TestSecretLiteralAnalyzerNeverEmitsRawSecretValues(t *testing.T) {
	patch := `--- a/scripts/bootstrap.sh
+++ b/scripts/bootstrap.sh
@@ -1,1 +1,4 @@
 set -eu
+deploy_token="` + testFakeGitHubToken + `"
+password='` + testFakePassword + `'
+api_key=AKIAABCDEFGHIJKLMNOP
`
	changeSet := workflowChangeSet("scripts/bootstrap.sh", patch, 3, 0)
	signals, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 1 || len(signals[0].Evidence) != 3 {
		t.Fatalf("expected one signal with three evidence items, got %+v", signals)
	}
	for _, evidence := range signals[0].Evidence {
		if evidence.Excerpt != "" {
			t.Errorf("excerpt must stay empty for secret rules, got %q", evidence.Excerpt)
		}
		for _, banned := range []string{testFakeGitHubToken, testFakePassword, "AKIAABCDEFGHIJKLMNOP"} {
			if strings.Contains(evidence.Fact, banned) {
				t.Errorf("fact leaks raw secret %q: %q", banned, evidence.Fact)
			}
		}
	}
}

func TestSecretLiteralAnalyzerPlaceholderExamplesAreNotReported(t *testing.T) {
	patch := `--- a/.env.example
+++ b/.env.example
@@ -1,1 +1,11 @@
 # config template
+password = "AKIAIOSFODNN7EXAMPLE"
+secret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
+api_key = "<your-api-key-here>"
+token = ${GITHUB_TOKEN}
+db_password = os.Getenv("DB_PASSWORD")
+admin_password: changeme
+auth_token = "xxxxxxxxxxxxxxxx"
+api_key: PLACEHOLDER_VALUE_42
+app_secret = correcthorsebatterystaple
+legacy_token: 12345678
+# token = supersecretvalue99
`
	changeSet := workflowChangeSet(".env.example", patch, 11, 0)
	signals, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("analyzer failed: %v", err)
	}
	if len(signals) != 0 {
		t.Fatalf("placeholder examples produced signals: %+v", signals)
	}
}

func TestSecretLiteralAnalyzerIgnoresUnavailableAndUnsafeInputs(t *testing.T) {
	patch := `--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,3 +1,3 @@
 name: CI
-  deploy_token: ` + testFakeGitHubToken + `
+  deploy_token: "${GITHUB_TOKEN}" # rotated to env reference
`
	changeSet := workflowChangeSet(".github/workflows/ci.yml", patch, 1, 1)
	signals, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("deleted side or env reference produced signals: signals=%+v err=%v", signals, err)
	}

	goCommentPatch := "--- a/main.go\n+++ b/main.go\n@@ -1,1 +1,2 @@\n package main\n+// " + testFakeGitHubToken + "\n"
	goChangeSet := workflowChangeSet("main.go", goCommentPatch, 1, 0)
	signals, err = (SecretLiteralAnalyzer{}).Analyze(context.Background(), goChangeSet)
	if err != nil || len(signals) != 0 {
		t.Fatalf("commented token produced signals: signals=%+v err=%v", signals, err)
	}

	noPatch := workflowChangeSet("app/config.yaml", "", 0, 0)
	noPatch.Files[0].Patch = nil
	signals, err = (SecretLiteralAnalyzer{}).Analyze(context.Background(), noPatch)
	if err != nil || len(signals) != 0 {
		t.Fatalf("missing patch produced signals: signals=%+v err=%v", signals, err)
	}

	binary := workflowChangeSet("app/config.yaml", patch, 1, 1)
	binary.Files[0].IsBinary = true
	signals, err = (SecretLiteralAnalyzer{}).Analyze(context.Background(), binary)
	if err != nil || len(signals) != 0 {
		t.Fatalf("binary file produced signals: signals=%+v err=%v", signals, err)
	}

	traversal := workflowChangeSet("../outside/secrets.env", patch, 1, 1)
	signals, err = (SecretLiteralAnalyzer{}).Analyze(context.Background(), traversal)
	if err != nil || len(signals) != 0 {
		t.Fatalf("traversal path produced signals: signals=%+v err=%v", signals, err)
	}
}

func TestSecretLiteralAnalyzerStableOrderingAndFailures(t *testing.T) {
	zPatch := "--- a/config/z.settings\n+++ b/config/z.settings\n@@ -1,1 +1,2 @@\n z: 1\n+z_secret: Zz9pQw4rTt\n"
	aPatch := "--- a/config/a.settings\n+++ b/config/a.settings\n@@ -1,1 +1,2 @@\n a: 1\n+a_token: " + testFakeGitHubToken + "\n"
	combined := domain.ChangeSet{
		Files: []domain.ChangedFile{
			{NewPath: "config/z.settings", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &zPatch},
			{NewPath: "config/a.settings", Status: domain.FileModified, Additions: 1, Changes: 1, Patch: &aPatch},
		},
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), combined)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}
	if first[0].Evidence[0].File != "config/a.settings" {
		t.Fatalf("signals are not stably sorted: %+v", first)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SecretLiteralAnalyzer{}).Analyze(canceled, combined); err == nil {
		t.Fatal("canceled context was ignored")
	}

	invalid := combined
	invalid.HeadSHA = "not-a-sha"
	if _, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), invalid); err == nil {
		t.Fatal("invalid change set was accepted")
	}

	malformed := workflowChangeSet("config/bad.settings",
		"--- a/config/bad.settings\n+++ b/config/bad.settings\n@@ -1,1 +1,2 @@\nx bad line\n", 1, 0)
	if _, err := (SecretLiteralAnalyzer{}).Analyze(context.Background(), malformed); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
