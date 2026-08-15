// Package domain 定义 Change Risk Analyzer 的领域对象和不变式。
// 领域包不依赖 GitHub SDK、具体模型 SDK、环境变量或文件系统。
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SourceKind 描述 Pull Request 的来源，只影响权限和模型策略，不改变代码事实解析。
type SourceKind string

const (
	SourceSameRepo SourceKind = "same_repository"
	SourceFork     SourceKind = "fork"
	SourceUnknown  SourceKind = "unknown"
)

// EventAction 描述触发分析的事件动作。
type EventAction string

const (
	ActionOpened           EventAction = "opened"
	ActionSynchronize      EventAction = "synchronize"
	ActionReopened         EventAction = "reopened"
	ActionWorkflowDispatch EventAction = "workflow_dispatch"
	ActionUnknown          EventAction = "unknown"
)

// RepositoryRef 标识一个仓库。
type RepositoryRef struct {
	Owner    string `json:"owner"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

// ReviewRequest 表示一次可复现的分析请求。
// head_sha 是幂等和报告身份的一部分。
type ReviewRequest struct {
	Repository        RepositoryRef `json:"repository"`
	PullRequestNumber int           `json:"pull_request_number"`
	EventAction       EventAction   `json:"event_action"`
	BaseSHA           string        `json:"base_sha"`
	HeadSHA           string        `json:"head_sha"`
	SourceKind        SourceKind    `json:"source_kind"`
	WorkflowRunID     string        `json:"workflow_run_id,omitempty"`
	// RequestedAt 是内部元数据，不进入报告协议。
	RequestedAt time.Time `json:"-"`
}

var (
	shaPattern       = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	repoNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	repoFullPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	degradationRe    = regexp.MustCompile(`^[a-z0-9_.-]{1,80}$`)
	findingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,160}$`)
)

// ValidationError 聚合一条记录上的多个校验问题。
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "domain validation failed: " + strings.Join(e.Problems, "; ")
}

type validator struct{ problems []string }

func (v *validator) check(ok bool, format string, args ...any) {
	if !ok {
		v.problems = append(v.problems, fmt.Sprintf(format, args...))
	}
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: v.problems}
}

// Validate 校验仓库引用字段格式和 full_name 一致性。
func (r RepositoryRef) Validate() error {
	v := &validator{}
	v.check(repoNamePattern.MatchString(r.Owner), "owner %q 不符合 ^[A-Za-z0-9_.-]+$", r.Owner)
	v.check(repoNamePattern.MatchString(r.Name), "name %q 不符合 ^[A-Za-z0-9_.-]+$", r.Name)
	v.check(repoFullPattern.MatchString(r.FullName), "full_name %q 不符合 ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", r.FullName)
	v.check(r.FullName == r.Owner+"/"+r.Name, "full_name %q 与 owner/name 不一致", r.FullName)
	return v.err()
}

// Validate 校验请求身份字段：SHA 格式、枚举和仓库引用。
func (r ReviewRequest) Validate() error {
	v := &validator{}
	v.check(r.Repository.Owner != "", "repository.owner 不能为空")
	v.check(r.Repository.Name != "", "repository.name 不能为空")
	v.check(r.Repository.FullName != "", "repository.full_name 不能为空")
	v.check(r.PullRequestNumber >= 1, "pull_request_number 必须 >= 1，实际为 %d", r.PullRequestNumber)
	v.check(validEventAction(r.EventAction), "event_action %q 非法", r.EventAction)
	v.check(shaPattern.MatchString(r.BaseSHA), "base_sha %q 不符合 ^[0-9a-fA-F]{7,64}$", r.BaseSHA)
	v.check(shaPattern.MatchString(r.HeadSHA), "head_sha %q 不符合 ^[0-9a-fA-F]{7,64}$", r.HeadSHA)
	v.check(validSourceKind(r.SourceKind), "source_kind %q 非法", r.SourceKind)
	if err := r.Repository.Validate(); err != nil {
		v.problems = append(v.problems, err.(*ValidationError).Problems...)
	}
	return v.err()
}

func validEventAction(a EventAction) bool {
	switch a {
	case ActionOpened, ActionSynchronize, ActionReopened, ActionWorkflowDispatch, ActionUnknown:
		return true
	}
	return false
}

func validSourceKind(k SourceKind) bool {
	switch k {
	case SourceSameRepo, SourceFork, SourceUnknown:
		return true
	}
	return false
}
