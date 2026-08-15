package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestExternalRequestWithoutTimeoutAnalyzerDetectsDefaultHTTPHelpers(t *testing.T) {
	patch := `--- a/service/client.go
+++ b/service/client.go
@@ -1,1 +1,5 @@
 package service
+response, err := http.Get(url)
+_, err = http.Head(endpoint)
+response, err = http.Post(url, "application/json", body)
+response, err = http.PostForm(url, values)
`
	signals, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/client.go", patch, 4, 0))
	if err != nil || len(signals) != 4 {
		t.Fatalf("expected four signals, got signals=%+v err=%v", signals, err)
	}
	for index, signal := range signals {
		if err := signal.Validate(); err != nil {
			t.Fatalf("signal[%d] violates domain invariants: %v", index, err)
		}
		if signal.RuleID != ExternalRequestWithoutTimeoutRuleID || signal.Category != domain.CategoryReliability || signal.Source != domain.SourceDeterministic {
			t.Errorf("unexpected signal[%d] identity: %+v", index, signal)
		}
		if signal.Confidence != 0.8 || signal.Weight != 20 || len(signal.Evidence) != 1 {
			t.Errorf("unexpected signal[%d] metadata: %+v", index, signal)
		}
		if signal.Evidence[0].File != "service/client.go" || signal.Evidence[0].Side != domain.SideRight || signal.Evidence[0].StartLine != index+2 {
			t.Errorf("unexpected signal[%d] evidence: %+v", index, signal.Evidence)
		}
	}
}

func TestExternalRequestWithoutTimeoutAnalyzerDetectsVisibleClientCalls(t *testing.T) {
	patch := `--- a/service/client.go
+++ b/service/client.go
@@ -1,1 +1,4 @@
 package service
+response, err := http.DefaultClient.Do(req)
+response, err = (&http.Client{}).Do(req)
+response, err = http.Client{Transport: transport}.Do(req)
`
	signals, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/client.go", patch, 3, 0))
	if err != nil || len(signals) != 3 {
		t.Fatalf("expected three signals, got signals=%+v err=%v", signals, err)
	}
	for _, signal := range signals {
		if !strings.Contains(signal.Fact, "HTTP") || signal.Evidence[0].Side != domain.SideRight {
			t.Errorf("unexpected reliability signal: %+v", signal)
		}
	}
}

func TestExternalRequestWithoutTimeoutAnalyzerAvoidsVisibleProtectionsAndAmbiguousCalls(t *testing.T) {
	patch := `--- a/service/client.go
+++ b/service/client.go
@@ -1,1 +1,8 @@
 package service
+response, err := http.DefaultClient.Do(req.WithContext(ctx))
+response, err = (&http.Client{}).Do(req.WithContext(ctx))
+response, err = (&http.Client{Timeout: time.Second}).Do(req)
+response, err = client.Do(req)
+req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
+// http.Get(url)
+message := "http.Get(url)"
`
	signals, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/client.go", patch, 7, 0))
	if err != nil || len(signals) != 0 {
		t.Fatalf("protected or ambiguous calls produced signals=%+v err=%v", signals, err)
	}
}

func TestExternalRequestWithoutTimeoutAnalyzerIgnoresBoundaryInputs(t *testing.T) {
	patch := `--- a/service/client.go
+++ b/service/client.go
@@ -1,2 +1,1 @@
 package service
-response, err := http.Get(url)
`
	file := workflowChangeSet("service/client.go", patch, 0, 1).Files[0]
	for _, candidate := range []domain.ChangedFile{
		file,
		func() domain.ChangedFile {
			copy := file
			copy.NewPath = "service/client_test.go"
			return copy
		}(),
		func() domain.ChangedFile {
			copy := file
			copy.NewPath = "service/client.txt"
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
		signals, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), workflowWithFile(candidate))
		if err != nil || len(signals) != 0 {
			t.Errorf("boundary input produced signals: file=%+v signals=%+v err=%v", candidate, signals, err)
		}
	}
}

func TestExternalRequestWithoutTimeoutAnalyzerStableAndContextAware(t *testing.T) {
	patchA := `--- a/service/a.go
+++ b/service/a.go
@@ -1,1 +1,2 @@
 package service
+http.Get(url)
`
	patchZ := `--- a/service/z.go
+++ b/service/z.go
@@ -1,1 +1,2 @@
 package service
+http.DefaultClient.Do(req)
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
	first, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 || first[0].Evidence[0].File != "service/a.go" {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestExternalRequestWithoutTimeoutAnalyzerRejectsMalformedPatch(t *testing.T) {
	patch := `--- a/service/client.go
+++ b/service/client.go
@@ -1 +1 @@
not a hunk line
`
	if _, err := (ExternalRequestWithoutTimeoutAnalyzer{}).Analyze(context.Background(), workflowChangeSet("service/client.go", patch, 0, 0)); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
