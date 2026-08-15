package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func validReviewRequest() ReviewRequest {
	return ReviewRequest{
		Repository: RepositoryRef{
			Owner:    "example-org",
			Name:     "risk-analyzer",
			FullName: "example-org/risk-analyzer",
		},
		PullRequestNumber: 42,
		EventAction:       ActionSynchronize,
		BaseSHA:           "0123456789abcdef0123456789abcdef01234567",
		HeadSHA:           "abcdef0123456789abcdef0123456789abcdef01",
		SourceKind:        SourceSameRepo,
		WorkflowRunID:     "run-123",
	}
}

func TestReviewRequestValidate(t *testing.T) {
	t.Run("accepts a valid request", func(t *testing.T) {
		if err := validReviewRequest().Validate(); err != nil {
			t.Fatalf("valid request rejected: %v", err)
		}
	})

	t.Run("rejects malformed head SHA", func(t *testing.T) {
		req := validReviewRequest()
		req.HeadSHA = "not-a-commit"
		if err := req.Validate(); err == nil {
			t.Fatal("malformed head SHA was accepted")
		} else if !strings.Contains(err.Error(), "head_sha") {
			t.Fatalf("error does not identify head_sha: %v", err)
		}
	})

	t.Run("rejects inconsistent repository full name", func(t *testing.T) {
		req := validReviewRequest()
		req.Repository.FullName = "other/repository"
		if err := req.Validate(); err == nil {
			t.Fatal("inconsistent repository was accepted")
		}
	})
}

func TestReviewRequestJSONOmitsInternalRequestedAt(t *testing.T) {
	data, err := json.Marshal(validReviewRequest())
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if strings.Contains(string(data), "requested_at") {
		t.Fatalf("internal requested_at leaked into JSON: %s", data)
	}
}
