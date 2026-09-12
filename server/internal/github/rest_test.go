// rest_test.go 是 C6 只读 REST 适配器（rest.go）的白盒单元测试。
//
// 全部请求都打向 httptest.NewServer 启动的本地测试服务器，绝不访问真实
// github.com；重试延迟通过白盒注入 sleep 记录器模拟，任何测试都不会真实
// 睡眠超过毫秒级。

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 测试共用常量：owner/repo/编号与 40 位十六进制假 SHA，
// 统一加 restTest 前缀避免与同包其他文件的辅助符号冲突。
const (
	restTestOwner   = "octo"
	restTestRepo    = "hello"
	restTestNumber  = 7
	restTestBaseSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	restTestHeadSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	restTestToken   = "super-secret-token-value"
)

// ---- 测试 JSON 构造 ----

// restTestRepoDetail 对应 wire 格式中的 repo 对象。
type restTestRepoDetail struct {
	FullName string `json:"full_name"`
}

// restTestRepoRef 对应 base/head 端点；Repo 为 nil 时序列化为 JSON null。
type restTestRepoRef struct {
	SHA  string              `json:"sha"`
	Repo *restTestRepoDetail `json:"repo"`
}

// restTestPRBody 对应 GET /pulls/{number} 响应中被解析的最小字段集。
type restTestPRBody struct {
	Number int             `json:"number"`
	Base   restTestRepoRef `json:"base"`
	Head   restTestRepoRef `json:"head"`
}

// restTestFileBody 对应 files 列表项；Patch 为 nil 时省略 patch 字段。
type restTestFileBody struct {
	Filename  string  `json:"filename"`
	Status    string  `json:"status"`
	Additions int     `json:"additions"`
	Deletions int     `json:"deletions"`
	Patch     *string `json:"patch,omitempty"`
}

// restTestRef 构造带仓库全名的 base/head 引用。
func restTestRef(sha, fullName string) restTestRepoRef {
	return restTestRepoRef{SHA: sha, Repo: &restTestRepoDetail{FullName: fullName}}
}

// restTestPRJSON 构造 PR 元数据 JSON。
func restTestPRJSON(base, head restTestRepoRef, number int) string {
	data, err := json.Marshal(restTestPRBody{Number: number, Base: base, Head: head})
	if err != nil {
		panic(fmt.Sprintf("restTestPRJSON: %v", err)) // 纯字符串结构，不会发生
	}
	return string(data)
}

// restTestFilesJSON 构造 files 列表 JSON。
func restTestFilesJSON(files []restTestFileBody) string {
	data, err := json.Marshal(files)
	if err != nil {
		panic(fmt.Sprintf("restTestFilesJSON: %v", err)) // 纯字符串结构，不会发生
	}
	return string(data)
}

// restTestPtr 返回字符串指针，便于内联构造 patch 字段。
func restTestPtr(s string) *string { return &s }

// ---- httptest spy 服务器 ----

// restRecordedRequest 记录一次服务端收到的请求快照，供回放断言。
type restRecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
}

// restSpyServer 包装 httptest.Server，并发安全地记录每个收到的请求；
// 记录完成后调用 respond 决定响应内容。
type restSpyServer struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []restRecordedRequest
}

// newRestSpyServer 创建 spy 服务器并注册 t.Cleanup 自动关闭。
func newRestSpyServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *restSpyServer {
	t.Helper()
	s := &restSpyServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := restRecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
		}
		s.mu.Lock()
		s.reqs = append(s.reqs, rec)
		s.mu.Unlock()
		respond(w, r)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// baseURL 返回服务器根地址，用作 RESTClient 的 baseURL。
func (s *restSpyServer) baseURL() string { return s.srv.URL }

// count 返回已收到的请求数。
func (s *restSpyServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reqs)
}

// requests 返回请求快照切片。
func (s *restSpyServer) requests() []restRecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]restRecordedRequest(nil), s.reqs...)
}

// restTestRespond 按给定状态码与响应体写回响应；extra 为附加响应头
// （如 Link、Retry-After）。
func restTestRespond(w http.ResponseWriter, status int, body string, extra http.Header) {
	for key, values := range extra {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}

// ---- 客户端构造与 sleep 注入 ----

// restSleepRecorder 替换 RESTClient.sleep：记录每次重试延迟并立即返回。
// 上下文已取消时返回 ctx.Err()，模拟真实 sleep 的可取消语义。
type restSleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *restSleepRecorder) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	r.delays = append(r.delays, d)
	r.mu.Unlock()
	return nil
}

func (r *restSleepRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.delays...)
}

// newRESTClientForTest 基于 baseURL 创建 REST 客户端，失败即终止测试。
func newRESTClientForTest(t *testing.T, baseURL, token string) *RESTClient {
	t.Helper()
	client, err := NewRESTClient(baseURL, token, &http.Client{})
	if err != nil {
		t.Fatalf("NewRESTClient(%q) failed: %v", baseURL, err)
	}
	return client
}

// newSpyClientForTest 创建绑定 spy 服务器的客户端，并注入 sleep 记录器，
// 保证即使实现意外触发重试也不会真实睡眠。
func newSpyClientForTest(t *testing.T, s *restSpyServer, token string) (*RESTClient, *restSleepRecorder) {
	t.Helper()
	client := newRESTClientForTest(t, s.baseURL(), token)
	rec := &restSleepRecorder{}
	client.sleep = rec.sleep
	return client, rec
}

// ---- 断言辅助 ----

// restAssertRequestCount 断言服务器收到的请求次数。
func restAssertRequestCount(t *testing.T, s *restSpyServer, want int) {
	t.Helper()
	if got := s.count(); got != want {
		t.Errorf("服务器收到 %d 次请求, want %d", got, want)
	}
}

// restAssertDelays 断言注入的 sleep 延迟序列与期望完全一致。
func restAssertDelays(t *testing.T, rec *restSleepRecorder, want []time.Duration) {
	t.Helper()
	got := rec.recorded()
	if len(got) != len(want) {
		t.Fatalf("sleep 记录 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sleep[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

// restAssertPlainGitHubError 断言 err 是带 github: 前缀、包含状态码的普通
// 错误，而非本包定义的任何类型化错误。
func restAssertPlainGitHubError(t *testing.T, err error, wantStatus string) {
	t.Helper()
	var perm *PermissionError
	if errors.As(err, &perm) {
		t.Errorf("不应映射为 PermissionError: %v", err)
	}
	var rate *RateLimitError
	if errors.As(err, &rate) {
		t.Errorf("不应映射为 RateLimitError: %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("不应映射为 ErrNotFound: %v", err)
	}
	if !strings.Contains(err.Error(), wantStatus) {
		t.Errorf("错误信息应包含状态码 %s: %v", wantStatus, err)
	}
}

// restAssertPageQueries 断言每次请求的 page 与 per_page 查询参数。
func restAssertPageQueries(t *testing.T, reqs []restRecordedRequest, wantPages []string, wantPerPage string) {
	t.Helper()
	if len(reqs) != len(wantPages) {
		t.Fatalf("请求数 = %d, want %d", len(reqs), len(wantPages))
	}
	for i, wantPage := range wantPages {
		if got := reqs[i].Query.Get("page"); got != wantPage {
			t.Errorf("请求 %d page = %q, want %q", i, got, wantPage)
		}
		if got := reqs[i].Query.Get("per_page"); got != wantPerPage {
			t.Errorf("请求 %d per_page = %q, want %q", i, got, wantPerPage)
		}
	}
}

// TestRESTGetPullRequestSuccess 覆盖正常获取 PR 元数据：meta 字段映射、
// 请求方法与路径、固定请求头，以及 Bearer 与匿名两种鉴权形态。
func TestRESTGetPullRequestSuccess(t *testing.T) {
	fullName := restTestOwner + "/" + restTestRepo
	body := restTestPRJSON(
		restTestRef(restTestBaseSHA, fullName),
		restTestRef(restTestHeadSHA, fullName),
		restTestNumber,
	)

	for _, tc := range []struct {
		name     string
		token    string
		wantAuth string // 期望的 Authorization 头值；空串表示不应携带该头
	}{
		{name: "带令牌", token: restTestToken, wantAuth: "Bearer " + restTestToken},
		{name: "匿名", token: "", wantAuth: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, http.StatusOK, body, nil)
			})
			client, rec := newSpyClientForTest(t, s, tc.token)

			meta, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
			if err != nil {
				t.Fatalf("GetPullRequest failed: %v", err)
			}
			want := PullRequestMeta{
				Owner:      restTestOwner,
				Repo:       restTestRepo,
				Number:     restTestNumber,
				BaseSHA:    restTestBaseSHA,
				HeadSHA:    restTestHeadSHA,
				IsFromFork: false,
			}
			if meta != want {
				t.Errorf("meta = %+v, want %+v", meta, want)
			}

			restAssertRequestCount(t, s, 1)
			if len(rec.recorded()) != 0 {
				t.Errorf("正常请求不应触发重试延迟: %v", rec.recorded())
			}

			req := s.requests()[0]
			if req.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", req.Method)
			}
			wantPath := fmt.Sprintf("/repos/%s/%s/pulls/%d", restTestOwner, restTestRepo, restTestNumber)
			if req.Path != wantPath {
				t.Errorf("path = %s, want %s", req.Path, wantPath)
			}
			if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Errorf("Accept = %q, want %q", got, "application/vnd.github+json")
			}
			if got := req.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
				t.Errorf("X-GitHub-Api-Version = %q, want %q", got, "2022-11-28")
			}
			if got := req.Header.Get("User-Agent"); got != "change-risk-analyzer" {
				t.Errorf("User-Agent = %q, want %q", got, "change-risk-analyzer")
			}
			if tc.wantAuth == "" {
				if values := req.Header.Values("Authorization"); len(values) != 0 {
					t.Errorf("匿名请求不应携带 Authorization 头: %v", values)
				}
			} else if got := req.Header.Get("Authorization"); got != tc.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tc.wantAuth)
			}
		})
	}
}

// TestRESTGetPullRequestForkDetection 覆盖 IsFromFork 的三种判定分支：
// 同名仓库为 false、不同仓库为 true、head.repo 为 JSON null 为 true。
func TestRESTGetPullRequestForkDetection(t *testing.T) {
	fullName := restTestOwner + "/" + restTestRepo
	cases := []struct {
		name       string
		headRepo   *restTestRepoDetail
		wantIsFork bool
	}{
		{name: "同名仓库", headRepo: &restTestRepoDetail{FullName: fullName}, wantIsFork: false},
		{name: "不同仓库", headRepo: &restTestRepoDetail{FullName: "forker/" + restTestRepo}, wantIsFork: true},
		{name: "head 仓库为 JSON null", headRepo: nil, wantIsFork: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := restTestPRJSON(
				restTestRef(restTestBaseSHA, fullName),
				restTestRepoRef{SHA: restTestHeadSHA, Repo: tc.headRepo},
				restTestNumber,
			)
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, http.StatusOK, body, nil)
			})
			client, _ := newSpyClientForTest(t, s, restTestToken)

			meta, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
			if err != nil {
				t.Fatalf("GetPullRequest failed: %v", err)
			}
			if meta.IsFromFork != tc.wantIsFork {
				t.Errorf("IsFromFork = %t, want %t", meta.IsFromFork, tc.wantIsFork)
			}
			restAssertRequestCount(t, s, 1)
		})
	}
}

// TestRESTGetPullRequestInvalidMeta 覆盖 200 响应但元数据非法的反例：
// 返回编号与请求不一致、缺少 base.sha、缺少 head.sha。
func TestRESTGetPullRequestInvalidMeta(t *testing.T) {
	fullName := restTestOwner + "/" + restTestRepo
	cases := []struct {
		name string
		body string
	}{
		{
			name: "返回编号与请求不一致",
			body: restTestPRJSON(restTestRef(restTestBaseSHA, fullName), restTestRef(restTestHeadSHA, fullName), restTestNumber+1),
		},
		{
			name: "缺少 base.sha",
			body: restTestPRJSON(restTestRef("", fullName), restTestRef(restTestHeadSHA, fullName), restTestNumber),
		},
		{
			name: "缺少 head.sha",
			body: restTestPRJSON(restTestRef(restTestBaseSHA, fullName), restTestRef("", fullName), restTestNumber),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, http.StatusOK, tc.body, nil)
			})
			client, rec := newSpyClientForTest(t, s, restTestToken)

			if _, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber); err == nil {
				t.Fatal("非法元数据应返回错误")
			}
			restAssertRequestCount(t, s, 1)
			if len(rec.recorded()) != 0 {
				t.Errorf("元数据校验失败不应重试: %v", rec.recorded())
			}
		})
	}
}

// TestRESTListFilesSinglePage 覆盖单页文件列表：字段映射、二进制文件
// patch 为 nil、无 Link 头时 NextPage 为 0、query 参数正确。
func TestRESTListFilesSinglePage(t *testing.T) {
	t.Run("字段映射且二进制 patch 为 nil", func(t *testing.T) {
		// 第二个文件显式携带 JSON null patch，模拟二进制文件。
		body := `[
			{"filename":"pkg/a.go","status":"modified","additions":3,"deletions":1,"patch":"@@ -10,7 +10,9 @@\n+warn"},
			{"filename":"bin/logo.png","status":"added","additions":0,"deletions":0,"patch":null}
		]`
		s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
			restTestRespond(w, http.StatusOK, body, nil)
		})
		client, rec := newSpyClientForTest(t, s, restTestToken)

		page, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 30)
		if err != nil {
			t.Fatalf("ListPullRequestFilesPage failed: %v", err)
		}
		if len(page.Files) != 2 {
			t.Fatalf("files = %d 项, want 2", len(page.Files))
		}
		first := page.Files[0]
		if first.Filename != "pkg/a.go" || first.Status != "modified" || first.Additions != 3 || first.Deletions != 1 {
			t.Errorf("files[0] = %+v", first)
		}
		if first.Patch == nil || *first.Patch != "@@ -10,7 +10,9 @@\n+warn" {
			t.Errorf("files[0].Patch = %v, want 与响应中的 patch 一致", first.Patch)
		}
		if page.Files[1].Patch != nil {
			t.Errorf("二进制文件 patch 应为 nil, got %q", *page.Files[1].Patch)
		}
		if page.NextPage != 0 {
			t.Errorf("无 Link 头时 NextPage = %d, want 0", page.NextPage)
		}

		restAssertRequestCount(t, s, 1)
		if len(rec.recorded()) != 0 {
			t.Errorf("正常请求不应触发重试延迟: %v", rec.recorded())
		}
		req := s.requests()[0]
		wantPath := fmt.Sprintf("/repos/%s/%s/pulls/%d/files", restTestOwner, restTestRepo, restTestNumber)
		if req.Path != wantPath {
			t.Errorf("path = %s, want %s", req.Path, wantPath)
		}
		if got := req.Query.Get("page"); got != "1" {
			t.Errorf("page = %q, want 1", got)
		}
		if got := req.Query.Get("per_page"); got != "30" {
			t.Errorf("per_page = %q, want 30", got)
		}
	})

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "空数组", body: "[]"},
		{name: "JSON null 主体", body: "null"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, http.StatusOK, tc.body, nil)
			})
			client, _ := newSpyClientForTest(t, s, restTestToken)

			page, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 30)
			if err != nil {
				t.Fatalf("ListPullRequestFilesPage failed: %v", err)
			}
			if page.Files == nil {
				t.Error("空结果 Files 不应为 nil")
			}
			if len(page.Files) != 0 {
				t.Errorf("len(Files) = %d, want 0", len(page.Files))
			}
			if page.NextPage != 0 {
				t.Errorf("NextPage = %d, want 0", page.NextPage)
			}
			restAssertRequestCount(t, s, 1)
		})
	}
}

// restTestTwoPageFilesServer 构造两页文件列表的 spy 服务器：
// 第 1 页携带 rel="next" Link 头，第 2 页无 Link 头。
func restTestTwoPageFilesServer(t *testing.T) *restSpyServer {
	t.Helper()
	page1 := restTestFilesJSON([]restTestFileBody{
		{Filename: "pkg/a.go", Status: "modified", Additions: 1, Deletions: 0, Patch: restTestPtr("+a")},
		{Filename: "pkg/b.go", Status: "modified", Additions: 2, Deletions: 1, Patch: restTestPtr("+b")},
	})
	page2 := restTestFilesJSON([]restTestFileBody{
		{Filename: "pkg/c.go", Status: "added", Additions: 5, Deletions: 0, Patch: restTestPtr("+c")},
	})
	return newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "1" {
			link := fmt.Sprintf("<http://%s%s?page=2&per_page=2>; rel=\"next\"", r.Host, r.URL.Path)
			restTestRespond(w, http.StatusOK, page1, http.Header{"Link": []string{link}})
			return
		}
		restTestRespond(w, http.StatusOK, page2, nil)
	})
}

// TestRESTListFilesPagination 覆盖 Link 头驱动的分页与 FetchAllFiles 聚合。
func TestRESTListFilesPagination(t *testing.T) {
	t.Run("NextPage 依次为 2 和 0", func(t *testing.T) {
		s := restTestTwoPageFilesServer(t)
		client, _ := newSpyClientForTest(t, s, restTestToken)

		first, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 2)
		if err != nil {
			t.Fatalf("第 1 页请求失败: %v", err)
		}
		if first.NextPage != 2 || len(first.Files) != 2 {
			t.Fatalf("第 1 页 = %+v, want NextPage 2 且 2 个文件", first)
		}
		second, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 2, 2)
		if err != nil {
			t.Fatalf("第 2 页请求失败: %v", err)
		}
		if second.NextPage != 0 || len(second.Files) != 1 {
			t.Fatalf("第 2 页 = %+v, want NextPage 0 且 1 个文件", second)
		}
		restAssertRequestCount(t, s, 2)
		restAssertPageQueries(t, s.requests(), []string{"1", "2"}, "2")
	})

	t.Run("FetchAllFiles 聚合两页且顺序正确", func(t *testing.T) {
		s := restTestTwoPageFilesServer(t)
		client, _ := newSpyClientForTest(t, s, restTestToken)

		all, err := FetchAllFiles(context.Background(), client, restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 2)
		if err != nil {
			t.Fatalf("FetchAllFiles failed: %v", err)
		}
		wantNames := []string{"pkg/a.go", "pkg/b.go", "pkg/c.go"}
		if len(all) != len(wantNames) {
			t.Fatalf("聚合文件数 = %d, want %d", len(all), len(wantNames))
		}
		for i, name := range wantNames {
			if all[i].Filename != name {
				t.Errorf("all[%d].Filename = %q, want %q", i, all[i].Filename, name)
			}
		}
		restAssertRequestCount(t, s, 2)
		restAssertPageQueries(t, s.requests(), []string{"1", "2"}, "2")
	})
}

// TestRESTErrorMapping 覆盖非 2xx 状态码的错误映射：只发一次请求不重试、
// 错误信息带 github: 前缀、不泄露 token、不回显响应体。
func TestRESTErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		check  func(t *testing.T, err error)
	}{
		{
			name:   "404 映射为 ErrNotFound",
			status: http.StatusNotFound,
			body:   `{"message":"Not Found"}`,
			check: func(t *testing.T, err error) {
				if !errors.Is(err, ErrNotFound) {
					t.Errorf("期望 ErrNotFound, got %v", err)
				}
			},
		},
		{
			name:   "401 映射为 PermissionError",
			status: http.StatusUnauthorized,
			body:   `{"message":"Bad credentials"}`,
			check: func(t *testing.T, err error) {
				var perm *PermissionError
				if !errors.As(err, &perm) {
					t.Fatalf("期望 PermissionError, got %v", err)
				}
				if perm.StatusCode != http.StatusUnauthorized {
					t.Errorf("StatusCode = %d, want 401", perm.StatusCode)
				}
			},
		},
		{
			name:   "403 映射为 PermissionError",
			status: http.StatusForbidden,
			body:   `{"message":"Resource not accessible by integration"}`,
			check: func(t *testing.T, err error) {
				var perm *PermissionError
				if !errors.As(err, &perm) {
					t.Fatalf("期望 PermissionError, got %v", err)
				}
				if perm.StatusCode != http.StatusForbidden {
					t.Errorf("StatusCode = %d, want 403", perm.StatusCode)
				}
			},
		},
		{
			name:   "422 映射为普通错误且不回显响应体",
			status: http.StatusUnprocessableEntity,
			body:   `{"message":"unique-marker validation failed"}`,
			check: func(t *testing.T, err error) {
				restAssertPlainGitHubError(t, err, "422")
				if strings.Contains(err.Error(), "unique-marker") {
					t.Errorf("错误信息不应回显响应体: %v", err)
				}
			},
		},
		{
			name:   "500 映射为普通错误且不回显响应体",
			status: http.StatusInternalServerError,
			body:   `{"message":"unique-marker boom"}`,
			check: func(t *testing.T, err error) {
				restAssertPlainGitHubError(t, err, "500")
				if strings.Contains(err.Error(), "unique-marker") {
					t.Errorf("错误信息不应回显响应体: %v", err)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, tc.status, tc.body, nil)
			})
			client, rec := newSpyClientForTest(t, s, restTestToken)

			_, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
			if err == nil {
				t.Fatalf("状态码 %d 应返回错误", tc.status)
			}
			tc.check(t, err)
			restAssertRequestCount(t, s, 1) // 这些状态码不应重试
			if len(rec.recorded()) != 0 {
				t.Errorf("不应注入重试延迟: %v", rec.recorded())
			}
			if strings.Contains(err.Error(), restTestToken) {
				t.Errorf("错误信息泄露 token: %v", err)
			}
			if !strings.HasPrefix(err.Error(), "github:") {
				t.Errorf("错误信息应以 github: 前缀开头: %v", err)
			}
		})
	}
}

// TestRESTRetryOn503ThenSuccess 覆盖可重试状态码：首次 503，重试一次后
// 成功，注入的 sleep 恰好记录一次 500ms 基准退避。
func TestRESTRetryOn503ThenSuccess(t *testing.T) {
	var attempts int32
	fullName := restTestOwner + "/" + restTestRepo
	body := restTestPRJSON(restTestRef(restTestBaseSHA, fullName), restTestRef(restTestHeadSHA, fullName), restTestNumber)
	s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			restTestRespond(w, http.StatusServiceUnavailable, `{"message":"Service Unavailable"}`, nil)
			return
		}
		restTestRespond(w, http.StatusOK, body, nil)
	})
	client, rec := newSpyClientForTest(t, s, restTestToken)

	meta, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if meta.HeadSHA != restTestHeadSHA {
		t.Errorf("HeadSHA = %q, want %q", meta.HeadSHA, restTestHeadSHA)
	}
	restAssertRequestCount(t, s, 2)
	restAssertDelays(t, rec, []time.Duration{500 * time.Millisecond}) // retryBaseDelay * 2^0
}

// TestREST429RetryExhaustedWithRetryAfter 覆盖 429 携带 Retry-After 的
// 重试耗尽路径：共 4 次尝试，3 次退避均为 Retry-After 值。
func TestREST429RetryExhaustedWithRetryAfter(t *testing.T) {
	s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
		restTestRespond(w, http.StatusTooManyRequests, `{"message":"Too Many Requests"}`,
			http.Header{"Retry-After": []string{"2"}})
	})
	client, rec := newSpyClientForTest(t, s, restTestToken)

	_, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
	var rate *RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("期望 RateLimitError, got %v", err)
	}
	if rate.RetryAfter != 2*time.Second {
		t.Errorf("RetryAfter = %s, want 2s", rate.RetryAfter)
	}
	restAssertRequestCount(t, s, 4) // 首次请求加 3 次重试
	restAssertDelays(t, rec, []time.Duration{2 * time.Second, 2 * time.Second, 2 * time.Second})
}

// TestREST429WithoutRetryAfter 覆盖 429 无 Retry-After 的重试耗尽路径：
// RetryAfter 为 0，退避按 500ms/1s/2s 指数递增。
func TestREST429WithoutRetryAfter(t *testing.T) {
	s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
		restTestRespond(w, http.StatusTooManyRequests, `{"message":"Too Many Requests"}`, nil)
	})
	client, rec := newSpyClientForTest(t, s, restTestToken)

	_, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
	var rate *RateLimitError
	if !errors.As(err, &rate) {
		t.Fatalf("期望 RateLimitError, got %v", err)
	}
	if rate.RetryAfter != 0 {
		t.Errorf("RetryAfter = %s, want 0", rate.RetryAfter)
	}
	restAssertRequestCount(t, s, 4)
	restAssertDelays(t, rec, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second})
}

// restCountingTransport 统计客户端实际发起的 HTTP 尝试次数，
// 用于服务器已关闭、无法在服务端计数的场景。
type restCountingTransport struct {
	mu    sync.Mutex
	count int
	base  http.RoundTripper
}

func (c *restCountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
	return c.base.RoundTrip(req)
}

func (c *restCountingTransport) attempts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

// TestRESTNetworkErrorRetryExhausted 覆盖网络错误重试耗尽：关闭服务器
// 模拟网络不可达，应尝试 4 次并按 500ms/1s/2s 退避。
func TestRESTNetworkErrorRetryExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		restTestRespond(w, http.StatusOK, "{}", nil)
	}))
	t.Cleanup(srv.Close) // httptest.Server.Close 可安全重复调用

	transport := &restCountingTransport{base: http.DefaultTransport}
	client, err := NewRESTClient(srv.URL, restTestToken, &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("NewRESTClient failed: %v", err)
	}
	rec := &restSleepRecorder{}
	client.sleep = rec.sleep

	srv.Close() // 先关闭服务器再发起请求

	_, err = client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
	if err == nil {
		t.Fatal("网络错误应返回错误")
	}
	if !strings.Contains(err.Error(), "github:") {
		t.Errorf("错误信息应包含 github: 前缀: %v", err)
	}
	if got := transport.attempts(); got != 4 {
		t.Errorf("尝试次数 = %d, want 4（首次加 3 次重试）", got)
	}
	restAssertDelays(t, rec, []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second})
}

// TestRESTInvalidJSONNoRetry 覆盖 200 响应体为非法 JSON：解码失败且只请求一次。
func TestRESTInvalidJSONNoRetry(t *testing.T) {
	cases := []struct {
		name string
		call func(*RESTClient) error
	}{
		{
			name: "元数据",
			call: func(c *RESTClient) error {
				_, err := c.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber)
				return err
			},
		},
		{
			name: "文件列表",
			call: func(c *RESTClient) error {
				_, err := c.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 30)
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				restTestRespond(w, http.StatusOK, `{"number":`, nil) // 截断的非法 JSON
			})
			client, rec := newSpyClientForTest(t, s, restTestToken)

			if err := tc.call(client); err == nil {
				t.Fatal("非法 JSON 应返回解码错误")
			}
			restAssertRequestCount(t, s, 1)
			if len(rec.recorded()) != 0 {
				t.Errorf("解码失败不应重试: %v", rec.recorded())
			}
		})
	}
}

// TestRESTContextCancellation 覆盖取消与超时上下文：预先取消时不发起
// 任何请求；服务端阻塞时超时报错且不重试。
func TestRESTContextCancellation(t *testing.T) {
	t.Run("预先取消的上下文不发起请求", func(t *testing.T) {
		s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
			restTestRespond(w, http.StatusOK, "{}", nil)
		})
		client, rec := newSpyClientForTest(t, s, restTestToken)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.GetPullRequest(ctx, restTestOwner, restTestRepo, restTestNumber); !errors.Is(err, context.Canceled) {
			t.Errorf("GetPullRequest 应返回包裹 context.Canceled 的错误, got %v", err)
		}
		if _, err := client.ListPullRequestFilesPage(ctx, restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 30); !errors.Is(err, context.Canceled) {
			t.Errorf("ListPullRequestFilesPage 应返回包裹 context.Canceled 的错误, got %v", err)
		}
		restAssertRequestCount(t, s, 0)
		if len(rec.recorded()) != 0 {
			t.Errorf("取消后不应进入重试等待: %v", rec.recorded())
		}
	})

	t.Run("服务端阻塞时上下文超时报错", func(t *testing.T) {
		release := make(chan struct{})
		s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
			<-release // 阻塞处理程序，直到测试清理阶段释放
			restTestRespond(w, http.StatusOK, "{}", nil)
		})
		t.Cleanup(func() { close(release) }) // 注册晚于服务器关闭，LIFO 先执行，避免 Close 挂起

		client, rec := newSpyClientForTest(t, s, restTestToken)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err := client.GetPullRequest(ctx, restTestOwner, restTestRepo, restTestNumber)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("超时应返回包裹 context.DeadlineExceeded 的错误, got %v", err)
		}
		// 等待服务器侧确认收到首次请求（本地回环几乎立即到达）。
		deadline := time.Now().Add(2 * time.Second)
		for s.count() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		restAssertRequestCount(t, s, 1) // 超时后 ctx 已取消，不应再发起重试
		if len(rec.recorded()) != 0 {
			t.Errorf("超时后不应进入重试等待: %v", rec.recorded())
		}
	})
}

// TestRESTConstructionAndArgumentValidation 覆盖构造函数与各方法的参数
// 校验：全部在发起任何网络请求之前报错。
func TestRESTConstructionAndArgumentValidation(t *testing.T) {
	t.Run("非法 baseURL", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			baseURL string
		}{
			{name: "空地址", baseURL: ""},
			{name: "非 http scheme", baseURL: "ftp://example.com"},
			{name: "携带 query", baseURL: "http://example.com/api?x=1"},
			{name: "携带 fragment", baseURL: "http://example.com/api#frag"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := NewRESTClient(tc.baseURL, restTestToken, &http.Client{}); err == nil {
					t.Errorf("baseURL %q 应被拒绝", tc.baseURL)
				}
			})
		}
	})

	t.Run("方法参数校验不发起网络请求", func(t *testing.T) {
		s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
			restTestRespond(w, http.StatusOK, "{}", nil)
		})
		client, rec := newSpyClientForTest(t, s, restTestToken)

		cases := []struct {
			name string
			call func() error
		}{
			{name: "GetPullRequest 空 owner", call: func() error {
				_, err := client.GetPullRequest(context.Background(), "", restTestRepo, restTestNumber)
				return err
			}},
			{name: "GetPullRequest owner 含斜杠", call: func() error {
				_, err := client.GetPullRequest(context.Background(), restTestOwner+"/"+restTestRepo, restTestRepo, restTestNumber)
				return err
			}},
			{name: "GetPullRequest owner 含空白", call: func() error {
				_, err := client.GetPullRequest(context.Background(), restTestOwner+" orb", restTestRepo, restTestNumber)
				return err
			}},
			{name: "GetPullRequest 空 repo", call: func() error {
				_, err := client.GetPullRequest(context.Background(), restTestOwner, "", restTestNumber)
				return err
			}},
			{name: "GetPullRequest number 为 0", call: func() error {
				_, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, 0)
				return err
			}},
			{name: "ListFiles 空 expectedHeadSHA", call: func() error {
				_, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, "", 1, 30)
				return err
			}},
			{name: "ListFiles page 为 0", call: func() error {
				_, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 0, 30)
				return err
			}},
			{name: "ListFiles perPage 为 0", call: func() error {
				_, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 0)
				return err
			}},
			{name: "ListFiles perPage 超过 100", call: func() error {
				_, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 101)
				return err
			}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if err := tc.call(); err == nil {
					t.Error("非法参数应返回错误")
				}
			})
		}
		restAssertRequestCount(t, s, 0)
		if len(rec.recorded()) != 0 {
			t.Errorf("参数校验失败不应触发重试延迟: %v", rec.recorded())
		}
	})
}

// TestRESTBodyOverLimit 覆盖响应体超过白盒收紧后的上限：报错且不重试。
func TestRESTBodyOverLimit(t *testing.T) {
	fullName := restTestOwner + "/" + restTestRepo
	body := restTestPRJSON(restTestRef(restTestBaseSHA, fullName), restTestRef(restTestHeadSHA, fullName), restTestNumber)
	s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
		restTestRespond(w, http.StatusOK, body, nil)
	})
	client, rec := newSpyClientForTest(t, s, restTestToken)
	client.maxBodyBytes = 16 // 白盒收紧：合法响应也必然超限

	if _, err := client.GetPullRequest(context.Background(), restTestOwner, restTestRepo, restTestNumber); err == nil {
		t.Fatal("超限响应体应返回错误")
	}
	restAssertRequestCount(t, s, 1)
	if len(rec.recorded()) != 0 {
		t.Errorf("超限错误不应重试: %v", rec.recorded())
	}
}

// TestRESTMalformedLinkHeader 覆盖 Link 头存在 rel="next" 但 page 参数
// 缺失或非正整数时报错且不重试。
func TestRESTMalformedLinkHeader(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "缺少 page 参数", query: "per_page=2"},
		{name: "page 非正整数", query: "page=0&per_page=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newRestSpyServer(t, func(w http.ResponseWriter, r *http.Request) {
				link := fmt.Sprintf("<http://%s%s?%s>; rel=\"next\"", r.Host, r.URL.Path, tc.query)
				restTestRespond(w, http.StatusOK, "[]", http.Header{"Link": []string{link}})
			})
			client, rec := newSpyClientForTest(t, s, restTestToken)

			if _, err := client.ListPullRequestFilesPage(context.Background(), restTestOwner, restTestRepo, restTestNumber, restTestHeadSHA, 1, 2); err == nil {
				t.Fatal("畸形 Link 头应返回错误")
			}
			restAssertRequestCount(t, s, 1)
			if len(rec.recorded()) != 0 {
				t.Errorf("Link 解析失败不应重试: %v", rec.recorded())
			}
		})
	}
}
