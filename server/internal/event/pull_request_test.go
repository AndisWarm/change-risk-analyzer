package event

import (
	"change-risk-analyzer/internal/domain"
	"errors"
	"strings"
	"testing"
)

func TestParsePullRequestEvent(t *testing.T) {
	t.Run("parses same repository pull request", func(t *testing.T) {
		request, err := ParsePullRequestEvent([]byte(validEventJSON("opened", "acme/risk", "acme/risk")))
		if err != nil {
			t.Fatalf("parse event: %v", err)
		}
		if request.EventAction != domain.ActionOpened {
			t.Fatalf("event action = %q, want %q", request.EventAction, domain.ActionOpened)
		}
		if request.SourceKind != domain.SourceSameRepo {
			t.Fatalf("source kind = %q, want %q", request.SourceKind, domain.SourceSameRepo)
		}
		if request.Repository.FullName != "acme/risk" || request.PullRequestNumber != 42 {
			t.Fatalf("unexpected request identity: %+v", request)
		}
		if err := request.Validate(); err != nil {
			t.Fatalf("parsed request is invalid: %v", err)
		}
	})

	t.Run("parses fork pull request", func(t *testing.T) {
		request, err := ParsePullRequestEvent([]byte(validEventJSON("synchronize", "acme/risk", "contributor/risk")))
		if err != nil {
			t.Fatalf("parse event: %v", err)
		}
		if request.EventAction != domain.ActionSynchronize || request.SourceKind != domain.SourceFork {
			t.Fatalf("unexpected fork request: %+v", request)
		}
	})

	t.Run("keeps unknown action valid", func(t *testing.T) {
		request, err := ParsePullRequestEvent([]byte(validEventJSON("labeled", "acme/risk", "ACME/RISK")))
		if err != nil {
			t.Fatalf("parse event: %v", err)
		}
		if request.EventAction != domain.ActionUnknown || request.SourceKind != domain.SourceSameRepo {
			t.Fatalf("unexpected unknown event request: %+v", request)
		}
	})

	t.Run("uses unknown source when head repository is unavailable", func(t *testing.T) {
		input := strings.Replace(validEventJSON("reopened", "acme/risk", "acme/risk"), `"repo":{"full_name":"acme/risk"}`, `"repo":null`, 1)
		request, err := ParsePullRequestEvent([]byte(input))
		if err != nil {
			t.Fatalf("parse event: %v", err)
		}
		if request.SourceKind != domain.SourceUnknown {
			t.Fatalf("source kind = %q, want %q", request.SourceKind, domain.SourceUnknown)
		}
	})
}

func TestParsePullRequestEventRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		field   string
		message string
	}{
		{name: "empty", input: " \n\t", message: "empty event payload"},
		{name: "invalid JSON", input: `{`, message: "invalid JSON"},
		{name: "missing pull request", input: `{"action":"opened"}`, field: "pull_request", message: "missing pull request payload"},
		{name: "workflow dispatch", input: validEventJSON("workflow_dispatch", "acme/risk", "acme/risk"), field: "action", message: "workflow_dispatch requires GitHub pull request lookup"},
		{name: "invalid SHA", input: strings.Replace(validEventJSON("opened", "acme/risk", "acme/risk"), "0123456789abcdef0123456789abcdef01234567", "not-a-sha", 1), message: "base_sha"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParsePullRequestEvent([]byte(test.input))
			if err == nil {
				t.Fatal("expected parse error")
			}
			var parseError *ParseError
			if test.field != "" && (!errors.As(err, &parseError) || parseError.Field != test.field) {
				t.Fatalf("parse error field = %v, want %q; err = %v", parseError, test.field, err)
			}
			if !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %q, want substring %q", err, test.message)
			}
		})
	}
}

func validEventJSON(action, repository, headRepository string) string {
	return `{
		"action":"` + action + `",
		"number":42,
		"repository":{"owner":{"login":"acme"},"name":"risk","full_name":"` + repository + `"},
		"pull_request":{
			"base":{"sha":"0123456789abcdef0123456789abcdef01234567"},
			"head":{"sha":"abcdef0123456789abcdef0123456789abcdef01","repo":{"full_name":"` + headRepository + `"}}
		}
	}`
}
