// Package github 提供只读 GitHub 访问的接口定义与可编程内存版假客户端。
//
// 本包是 C5 检查点的交付物：真实 REST 客户端（C6）将实现同一接口，
// 并使用这里的错误分类与分页契约。整个包不发任何真实网络请求。
package github

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// PullRequestMeta 是分析所需的 PR 元数据最小集。
type PullRequestMeta struct {
	Owner      string
	Repo       string
	Number     int
	BaseSHA    string
	HeadSHA    string
	IsFromFork bool // head 仓库与目标仓库不同即为 Fork
}

// FileEntry 是 PR 变更文件的最小描述，字段形态对齐未来 ChangeSet 组装。
type FileEntry struct {
	Filename  string
	Status    string // added / modified / removed / renamed / copied
	Additions int
	Deletions int
	Patch     *string // 二进制或无 patch 时为 nil
}

// FilePage 是一页文件列表；NextPage 为 0 表示没有更多页。
type FilePage struct {
	Files    []FileEntry
	NextPage int
}

// Client 是只读 GitHub 访问接口。expectedHeadSHA 参数用于防止
// 「取元数据与取文件之间 PR 被更新」的竞态：不一致时返回
// *HeadSHAMismatchError。
type Client interface {
	GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequestMeta, error)
	ListPullRequestFilesPage(ctx context.Context, owner, repo string, number int, expectedHeadSHA string, page, perPage int) (FilePage, error)
}

// 哨兵与类型化错误。真实客户端应把 HTTP 状态码映射到这些类型。
var (
	ErrNotFound = errors.New("github: pull request not found")
)

// RateLimitError 表示 429 限流；RetryAfter 为服务端建议的等待时长。
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limited, retry after %s", e.RetryAfter)
}

// PermissionError 表示 401 未认证或 403 无权限。
type PermissionError struct {
	StatusCode int
	Message    string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("github: permission denied (status %d): %s", e.StatusCode, e.Message)
}

// HeadSHAMismatchError 表示期望的 head SHA 与当前实际值不一致。
type HeadSHAMismatchError struct {
	Expected string
	Actual   string
}

func (e *HeadSHAMismatchError) Error() string {
	return fmt.Sprintf("github: head sha mismatch: expected %s, actual %s", e.Expected, e.Actual)
}

// listCall 记录一次分页调用的参数，供测试断言分页行为。
type listCall struct {
	Page    int
	PerPage int
}

// FakeClient 是纯内存、可并发安全、可脚本化的 Client 实现。
type FakeClient struct {
	mu          sync.RWMutex
	meta        PullRequestMeta
	files       []FileEntry
	pageSize    int
	listCalls   []listCall
	nextListErr error // 一次性注入，触发后清除
	nextMetaErr error // 一次性注入，触发后清除
}

// NewFakeClient 创建假客户端；pageSize 为分页大小，必须为正。
func NewFakeClient(meta PullRequestMeta, files []FileEntry, pageSize int) (*FakeClient, error) {
	if pageSize <= 0 {
		return nil, fmt.Errorf("github: fake client page size must be positive, got %d", pageSize)
	}
	return &FakeClient{
		meta:     meta,
		files:    append([]FileEntry(nil), files...),
		pageSize: pageSize,
	}, nil
}

// SetPageSize 调整后续分页大小。
func (f *FakeClient) SetPageSize(size int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if size <= 0 {
		return fmt.Errorf("github: fake client page size must be positive, got %d", size)
	}
	f.pageSize = size
	return nil
}

// SetCurrentHeadSHA 更新当前 head SHA，用于模拟 SHA 不匹配竞态。
func (f *FakeClient) SetCurrentHeadSHA(sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.meta.HeadSHA = sha
}

// InjectNextListError 注入一次性列表错误（如 &RateLimitError{}），
// 触发一次后自动清除。
func (f *FakeClient) InjectNextListError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextListErr = err
}

// InjectNextMetaError 注入一次性元数据错误，触发一次后自动清除。
func (f *FakeClient) InjectNextMetaError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextMetaErr = err
}

// ListCalls 返回到目前为止的分页调用记录快照。
func (f *FakeClient) ListCalls() []listCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]listCall(nil), f.listCalls...)
}

// GetPullRequest 返回配置的 PR 元数据。编号不匹配返回 ErrNotFound；
// 存在一次性注入错误时返回它并清除。
func (f *FakeClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequestMeta, error) {
	if err := ctx.Err(); err != nil {
		return PullRequestMeta{}, fmt.Errorf("github: get pull request canceled: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextMetaErr != nil {
		err := f.nextMetaErr
		f.nextMetaErr = nil
		return PullRequestMeta{}, err
	}
	if f.meta.Owner != owner || f.meta.Repo != repo || f.meta.Number != number {
		return PullRequestMeta{}, ErrNotFound
	}
	return f.meta, nil
}

// ListPullRequestFilesPage 返回指定页的文件列表，并校验期望的 head SHA。
func (f *FakeClient) ListPullRequestFilesPage(ctx context.Context, owner, repo string, number int, expectedHeadSHA string, page, perPage int) (FilePage, error) {
	if err := ctx.Err(); err != nil {
		return FilePage{}, fmt.Errorf("github: list files canceled: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nextListErr != nil {
		err := f.nextListErr
		f.nextListErr = nil
		return FilePage{}, err
	}
	if f.meta.Owner != owner || f.meta.Repo != repo || f.meta.Number != number {
		return FilePage{}, ErrNotFound
	}
	if f.meta.HeadSHA != expectedHeadSHA {
		return FilePage{}, &HeadSHAMismatchError{Expected: expectedHeadSHA, Actual: f.meta.HeadSHA}
	}
	if page < 1 || perPage <= 0 {
		return FilePage{}, fmt.Errorf("github: invalid pagination page=%d perPage=%d", page, perPage)
	}
	f.listCalls = append(f.listCalls, listCall{Page: page, PerPage: perPage})

	size := perPage
	if size > f.pageSize {
		size = f.pageSize
	}
	start := (page - 1) * size
	if start >= len(f.files) {
		return FilePage{Files: []FileEntry{}, NextPage: 0}, nil
	}
	end := start + size
	if end > len(f.files) {
		end = len(f.files)
	}
	files := append([]FileEntry(nil), f.files[start:end]...)
	next := page + 1
	if end >= len(f.files) {
		next = 0
	}
	return FilePage{Files: files, NextPage: next}, nil
}

// FetchAllFiles 便捷函数：逐页聚合全部文件列表，直到没有更多页。
// 供未来适配器与测试复用。
func FetchAllFiles(ctx context.Context, client Client, owner, repo string, number int, expectedHeadSHA string, perPage int) ([]FileEntry, error) {
	var all []FileEntry
	for page := 1; ; page++ {
		result, err := client.ListPullRequestFilesPage(ctx, owner, repo, number, expectedHeadSHA, page, perPage)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Files...)
		if result.NextPage == 0 {
			return all, nil
		}
	}
}
