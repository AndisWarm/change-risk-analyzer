// Package event parses GitHub Action event payloads into domain requests.
// It only decodes JSON and validates fields; it does not call GitHub or execute input content.
package event

import (
	"bytes"
	"change-risk-analyzer/internal/domain"
	"encoding/json"
	"fmt"
	"strings"
)

// ParseError identifies a structural Action event error without including the raw event payload.
type ParseError struct {
	Field  string
	Reason string
}

func (e *ParseError) Error() string {
	if e.Field == "" {
		return "event parse error: " + e.Reason
	}
	return fmt.Sprintf("event parse error for %s: %s", e.Field, e.Reason)
}

// ParsePullRequestEvent converts a pull_request-style Action event JSON document into a ReviewRequest.
// workflow_dispatch events are deliberately rejected because their real GitHub payloads do not contain
// verified base/head SHAs; a later GitHub adapter must look up the requested PR before analysis.
func ParsePullRequestEvent(input []byte) (domain.ReviewRequest, error) {
	if len(bytes.TrimSpace(input)) == 0 {
		return domain.ReviewRequest{}, &ParseError{Reason: "empty event payload"}
	}

	var payload pullRequestEvent
	if err := json.Unmarshal(input, &payload); err != nil {
		return domain.ReviewRequest{}, &ParseError{Reason: "invalid JSON"}
	}
	if strings.TrimSpace(payload.Action) == string(domain.ActionWorkflowDispatch) {
		return domain.ReviewRequest{}, &ParseError{
			Field:  "action",
			Reason: "workflow_dispatch requires GitHub pull request lookup",
		}
	}
	if payload.PullRequest == nil {
		return domain.ReviewRequest{}, &ParseError{Field: "pull_request", Reason: "missing pull request payload"}
	}

	request := domain.ReviewRequest{
		Repository: domain.RepositoryRef{
			Owner:    payload.Repository.Owner.Login,
			Name:     payload.Repository.Name,
			FullName: payload.Repository.FullName,
		},
		PullRequestNumber: payload.Number,
		EventAction:       actionFor(payload.Action),
		BaseSHA:           payload.PullRequest.Base.SHA,
		HeadSHA:           payload.PullRequest.Head.SHA,
		SourceKind:        sourceKindFor(payload.Repository.FullName, payload.PullRequest.Head.Repository),
	}
	if err := request.Validate(); err != nil {
		return domain.ReviewRequest{}, fmt.Errorf("event request validation: %w", err)
	}
	return request, nil
}

type pullRequestEvent struct {
	Action     string `json:"action"`
	Number     int    `json:"number"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest *struct {
		Base struct {
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA        string           `json:"sha"`
			Repository *eventRepository `json:"repo"`
		} `json:"head"`
	} `json:"pull_request"`
}

type eventRepository struct {
	FullName string `json:"full_name"`
}

func actionFor(value string) domain.EventAction {
	switch strings.TrimSpace(value) {
	case string(domain.ActionOpened):
		return domain.ActionOpened
	case string(domain.ActionSynchronize):
		return domain.ActionSynchronize
	case string(domain.ActionReopened):
		return domain.ActionReopened
	default:
		return domain.ActionUnknown
	}
}

func sourceKindFor(repository string, headRepository *eventRepository) domain.SourceKind {
	if headRepository == nil || strings.TrimSpace(headRepository.FullName) == "" {
		return domain.SourceUnknown
	}
	if strings.EqualFold(strings.TrimSpace(repository), strings.TrimSpace(headRepository.FullName)) {
		return domain.SourceSameRepo
	}
	return domain.SourceFork
}

var _ error = (*ParseError)(nil)
