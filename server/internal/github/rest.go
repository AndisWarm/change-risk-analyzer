// rest.go 是 C6 检查点的交付物：只读 GitHub REST 适配器。
//
// RESTClient 基于标准库 net/http 实现同包 fake.go 定义的只读 Client 接口：
//   - 只发 GET 请求，不执行任何写操作；
//   - 统一重试策略：429/502/503/504 与传输错误最多重试 3 次（总尝试 4 次），
//     其余状态码、解码失败、响应体超限与 ctx 取消一律不重试；
//   - 状态码映射：404 映射为 ErrNotFound，401/403 映射为 *PermissionError，
//     429 映射为 *RateLimitError；
//   - 响应体统一经 io.LimitReader 限长读取，超限报错且不重试；
//   - 安全约束：错误信息绝不包含 token、Authorization 头的值或原始响应体，
//     全程不打印任何日志。

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// RESTClient 是只读 Client 接口的真实 REST 实现。
// 字段供包内实现与白盒测试注入使用，外部应通过 NewRESTClient 构造。
type RESTClient struct {
	// baseURL 是规范化后不带尾部斜杠的 API 根地址。
	baseURL string
	// token 为空表示匿名请求；非空时以 Bearer 方式携带。
	// 该值绝不进入任何错误信息或日志。
	token string
	// httpClient 为 nil 时使用带 defaultHTTPTimeout 的默认客户端。
	httpClient *http.Client
	// maxBodyBytes 是单个响应体的最大读取字节数，默认 defaultMaxResponseBytes。
	maxBodyBytes int64
	// sleep 是重试退避注入点；nil 时使用真实的可取消睡眠 defaultSleep。
	sleep func(ctx context.Context, d time.Duration) error
}

// 编译期断言：RESTClient 满足只读 Client 接口。
var _ Client = (*RESTClient)(nil)

const (
	// defaultMaxResponseBytes 是单个响应体允许读取的最大字节数（64 MiB）。
	defaultMaxResponseBytes int64 = 64 << 20
	// defaultHTTPTimeout 是未注入 http.Client 时的默认请求超时。
	defaultHTTPTimeout = 30 * time.Second
	// maxRetryAfterDelay 是 429 Retry-After 退避时长的上限。
	maxRetryAfterDelay = 60 * time.Second
	// retryBaseDelay 是指数退避的基准延迟，实际延迟为 retryBaseDelay*2^attempt。
	retryBaseDelay = 500 * time.Millisecond
	// maxAttempts 是总尝试次数：首次请求加最多 3 次重试。
	maxAttempts = 4
)

// 请求头固定值，符合 GitHub REST API 约定。
const (
	acceptHeaderValue     = "application/vnd.github+json"
	apiVersionHeaderValue = "2022-11-28"
	userAgentHeaderValue  = "change-risk-analyzer"
)

// NewRESTClient 创建只读 REST 客户端。
// baseURL 必须为非空 http/https URL，不得携带 query 或 fragment，
// Host 不得为空；创建时会去除尾部斜杠完成规范化。
// token 可为空（匿名请求）；httpClient 为 nil 时使用默认超时的客户端。
func NewRESTClient(baseURL, token string, httpClient *http.Client) (*RESTClient, error) {
	if baseURL == "" {
		return nil, errors.New("github: base url must not be empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("github: invalid base url: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, errors.New("github: base url scheme must be http or https")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return nil, errors.New("github: base url must not contain query or fragment")
	}
	if parsed.Host == "" {
		return nil, errors.New("github: base url host must not be empty")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &RESTClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		token:        token,
		httpClient:   httpClient,
		maxBodyBytes: defaultMaxResponseBytes,
		sleep:        defaultSleep,
	}, nil
}

// defaultSleep 是默认退避实现：真实睡眠，ctx 取消时立即返回 ctx.Err()。
func defaultSleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// pullRequestResponse 对应 GET /repos/{owner}/{repo}/pulls/{number} 的最小响应字段。
type pullRequestResponse struct {
	Number int          `json:"number"`
	Base   *branchState `json:"base"`
	Head   *branchState `json:"head"`
}

// branchState 是 PR 的 base/head 端点信息。
type branchState struct {
	SHA  string   `json:"sha"`
	Repo *repoRef `json:"repo"`
}

// repoRef 只保留仓库全名，用于 Fork 判定。
type repoRef struct {
	FullName string `json:"full_name"`
}

// GetPullRequest 拉取 PR 元数据（GET /repos/{owner}/{repo}/pulls/{number}）。
//
// 响应中解码出的 number 与请求编号不一致时报错（防止代理误路由）；
// base 或 head SHA 缺失时报错。IsFromFork 的判定：
// head.repo 为 null（Fork 已删除），或 head.repo.full_name 与
// base.repo.full_name 不一致（base.repo 为 null 时按空串比较）。
func (r *RESTClient) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequestMeta, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return PullRequestMeta{}, err
	}
	if number < 1 {
		return PullRequestMeta{}, fmt.Errorf("github: invalid argument: number must be >= 1, got %d", number)
	}
	endpoint := "/repos/" + owner + "/" + repo + "/pulls/" + strconv.Itoa(number)
	u, err := r.buildURL(endpoint, nil)
	if err != nil {
		return PullRequestMeta{}, err
	}
	resp, err := r.getWithRetry(ctx, u)
	if err != nil {
		return PullRequestMeta{}, err
	}
	defer resp.Body.Close()
	body, err := r.readBody(u, resp)
	if err != nil {
		return PullRequestMeta{}, err
	}
	var payload pullRequestResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return PullRequestMeta{}, fmt.Errorf("github: decode pull request response of %s: %w", u.Path, err)
	}
	if payload.Number != number {
		return PullRequestMeta{}, fmt.Errorf("github: pull request response of %s: number %d does not match requested %d", u.Path, payload.Number, number)
	}
	if payload.Base == nil || payload.Base.SHA == "" {
		return PullRequestMeta{}, fmt.Errorf("github: pull request response of %s: missing base sha", u.Path)
	}
	if payload.Head == nil || payload.Head.SHA == "" {
		return PullRequestMeta{}, fmt.Errorf("github: pull request response of %s: missing head sha", u.Path)
	}
	return PullRequestMeta{
		Owner:      owner,
		Repo:       repo,
		Number:     number,
		BaseSHA:    payload.Base.SHA,
		HeadSHA:    payload.Head.SHA,
		IsFromFork: isFromFork(&payload),
	}, nil
}

// isFromFork 依据响应判断 PR 是否来自 Fork：head 仓库缺失（Fork 已删除）
// 或 head 仓库与 base 仓库全名不同即视为 Fork；base 仓库缺失时按空串比较。
func isFromFork(payload *pullRequestResponse) bool {
	if payload.Head.Repo == nil {
		return true
	}
	baseFullName := ""
	if payload.Base.Repo != nil {
		baseFullName = payload.Base.Repo.FullName
	}
	return payload.Head.Repo.FullName != baseFullName
}

// fileResponse 对应 files 端点返回的单个文件对象。
type fileResponse struct {
	Filename  string  `json:"filename"`
	Status    string  `json:"status"`
	Additions int     `json:"additions"`
	Deletions int     `json:"deletions"`
	Patch     *string `json:"patch"`
}

// ListPullRequestFilesPage 拉取 PR 变更文件的一页
// （GET /repos/{owner}/{repo}/pulls/{number}/files?page&per_page）。
//
// expectedHeadSHA 语义说明：GitHub 的 files 端点响应不包含 head SHA，
// REST 实现无法在本方法内完成一致性校验；该参数仅做非空校验以保留
// Client 接口契约，真正的竞态防护由调用方（编排层）在列取文件前后
// 各调用一次 GetPullRequest 对比 head SHA 实现。
//
// NextPage 取自 Link 响应头：无 Link 头或无 rel="next" 时为 0；
// 存在 rel="next" 但缺少 page 参数或 page 不是正整数时报错，
// 宁可报错也不静默截断。
func (r *RESTClient) ListPullRequestFilesPage(ctx context.Context, owner, repo string, number int, expectedHeadSHA string, page, perPage int) (FilePage, error) {
	if err := validateOwnerRepo(owner, repo); err != nil {
		return FilePage{}, err
	}
	if number < 1 {
		return FilePage{}, fmt.Errorf("github: invalid argument: number must be >= 1, got %d", number)
	}
	if expectedHeadSHA == "" {
		return FilePage{}, errors.New("github: invalid argument: expectedHeadSHA must not be empty")
	}
	if page < 1 {
		return FilePage{}, fmt.Errorf("github: invalid argument: page must be >= 1, got %d", page)
	}
	if perPage < 1 || perPage > 100 {
		return FilePage{}, fmt.Errorf("github: invalid argument: perPage must be in [1,100], got %d", perPage)
	}
	endpoint := "/repos/" + owner + "/" + repo + "/pulls/" + strconv.Itoa(number) + "/files"
	query := url.Values{}
	query.Set("page", strconv.Itoa(page))
	query.Set("per_page", strconv.Itoa(perPage))
	u, err := r.buildURL(endpoint, query)
	if err != nil {
		return FilePage{}, err
	}
	resp, err := r.getWithRetry(ctx, u)
	if err != nil {
		return FilePage{}, err
	}
	defer resp.Body.Close()
	body, err := r.readBody(u, resp)
	if err != nil {
		return FilePage{}, err
	}
	var payload []fileResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return FilePage{}, fmt.Errorf("github: decode pull request files response of %s: %w", u.Path, err)
	}
	files := make([]FileEntry, 0, len(payload))
	for _, item := range payload {
		files = append(files, FileEntry{
			Filename:  item.Filename,
			Status:    item.Status,
			Additions: item.Additions,
			Deletions: item.Deletions,
			Patch:     item.Patch,
		})
	}
	nextPage, err := parseNextPageLink(resp.Header.Get("Link"))
	if err != nil {
		return FilePage{}, fmt.Errorf("github: list files of %s: %w", u.Path, err)
	}
	return FilePage{Files: files, NextPage: nextPage}, nil
}

// parseNextPageLink 从 Link 响应头解析 rel="next" 的下一页页码。
// 无 Link 头或没有 rel="next" 时返回 0；存在 rel="next" 但目标 URL
// 缺少 page 参数或 page 不是正整数时返回错误。
func parseNextPageLink(link string) (int, error) {
	if strings.TrimSpace(link) == "" {
		return 0, nil
	}
	for _, part := range strings.Split(link, ",") {
		segments := strings.Split(part, ";")
		target := strings.TrimSpace(segments[0])
		target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
		isNext := false
		for _, param := range segments[1:] {
			if isRelNext(param) {
				isNext = true
				break
			}
		}
		if !isNext {
			continue
		}
		nextURL, err := url.Parse(target)
		if err != nil {
			return 0, fmt.Errorf("parse next page link target: %w", err)
		}
		pageValue := nextURL.Query().Get("page")
		if pageValue == "" {
			return 0, errors.New(`next page link has rel="next" but missing page parameter`)
		}
		page, err := strconv.Atoi(pageValue)
		if err != nil || page < 1 {
			return 0, fmt.Errorf("next page link page parameter %q is not a positive integer", pageValue)
		}
		return page, nil
	}
	return 0, nil
}

// isRelNext 判断 Link 头的一个参数段是否为 rel="next"。
func isRelNext(param string) bool {
	key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(key), "rel") {
		return false
	}
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	return strings.EqualFold(value, "next")
}

// getWithRetry 执行一次 GET 请求并应用统一的重试与错误映射策略：
//   - 总尝试次数为 maxAttempts（首次请求加最多 3 次重试）；
//   - 可重试：429、502、503、504 与传输错误；其余情况一律立即失败；
//   - 每次发请求前检查 ctx，已取消则中止并返回包裹 ctx.Err() 的错误；
//   - 429 且 Retry-After 为合法非负整数秒时按其值退避（上限
//     maxRetryAfterDelay），否则按 retryBaseDelay*2^attempt 指数退避；
//     退避通过注入的 sleep 执行，sleep 返回错误即中止；
//   - 非 2xx 响应体只读取前 errBodyLimit 字节用于提取 message，随后关闭，
//     兼作重试前释放连接的预读。
//
// 成功时返回状态码为 2xx 的响应，响应体未读取，由调用方负责读取并关闭。
func (r *RESTClient) getWithRetry(ctx context.Context, u *url.URL) (*http.Response, error) {
	requestPath := u.Path
	sleepFn := r.sleep
	if sleepFn == nil {
		sleepFn = defaultSleep
	}
	httpClient := r.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	var lastRetryAfter time.Duration
	var lastRetryAfterValid bool
	for attempt := 0; ; attempt++ {
		// 发请求前检查取消状态；ctx 已取消时直接中止，不重试。
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("github: request %s canceled: %w", requestPath, err)
		}
		resp, err := r.doOnce(ctx, httpClient, u)
		if err != nil {
			// 传输层错误可重试，但 ctx 取消/超时导致的失败除外。
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, fmt.Errorf("github: request %s canceled: %w", requestPath, ctxErr)
			}
			if attempt == maxAttempts-1 {
				return nil, fmt.Errorf("github: GET %s: %w", requestPath, err)
			}
			if sleepErr := sleepFn(ctx, retryBaseDelay<<attempt); sleepErr != nil {
				return nil, fmt.Errorf("github: request %s aborted during backoff: %w", requestPath, sleepErr)
			}
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		// 非 2xx：解析限流重试头，读取受限错误体后关闭响应，再决定重试或映射。
		retryable := statusCodeRetryable(resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests {
			lastRetryAfter, lastRetryAfterValid = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
		errBody := readErrorBody(resp.Body)
		if !retryable {
			return nil, mapStatusError(requestPath, resp.StatusCode, errBody)
		}
		if attempt == maxAttempts-1 {
			// 重试耗尽：429 返回限流错误，其余 5xx 返回带路径与状态码的错误。
			if resp.StatusCode == http.StatusTooManyRequests {
				return nil, &RateLimitError{RetryAfter: lastRetryAfter}
			}
			return nil, fmt.Errorf("github: GET %s: status %d after %d attempts", requestPath, resp.StatusCode, maxAttempts)
		}
		delay := retryBaseDelay << attempt
		if resp.StatusCode == http.StatusTooManyRequests && lastRetryAfterValid {
			delay = lastRetryAfter
			if delay > maxRetryAfterDelay {
				delay = maxRetryAfterDelay
			}
		}
		if sleepErr := sleepFn(ctx, delay); sleepErr != nil {
			return nil, fmt.Errorf("github: request %s aborted during backoff: %w", requestPath, sleepErr)
		}
	}
}

// doOnce 发送一次 GET 请求。固定请求头符合 GitHub REST API 约定；
// token 非空时才携带 Authorization 头，且该头的值不会进入任何错误信息。
func (r *RESTClient) doOnce(ctx context.Context, httpClient *http.Client, u *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", acceptHeaderValue)
	req.Header.Set("X-GitHub-Api-Version", apiVersionHeaderValue)
	req.Header.Set("User-Agent", userAgentHeaderValue)
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	return httpClient.Do(req)
}

// buildURL 基于已校验的 baseURL 构造请求 URL。endpoint 以 / 开头；
// query 为 nil 时不追加查询串。通过 url.URL 结构体拼装，
// 使路径片段中的保留字符被正确转义。
func (r *RESTClient) buildURL(endpoint string, query url.Values) (*url.URL, error) {
	base, err := url.Parse(r.baseURL)
	if err != nil {
		return nil, fmt.Errorf("github: invalid base url: %w", err)
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + endpoint
	u.RawPath = ""
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return &u, nil
}

// readBody 以 maxBodyBytes+1 的 LimitReader 读取 2xx 响应体。
// 超过上限时报错（包含请求路径与上限字节数，不包含响应内容），不重试。
func (r *RESTClient) readBody(u *url.URL, resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, r.maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("github: read response body of %s: %w", u.Path, err)
	}
	if int64(len(data)) > r.maxBodyBytes {
		return nil, fmt.Errorf("github: response body of %s exceeds limit of %d bytes", u.Path, r.maxBodyBytes)
	}
	return data, nil
}

// errBodyLimit 限制错误响应体的读取字节数：错误体只用于提取 message
// 字段，限制读取量可防止异常大的错误响应拖慢请求。
const errBodyLimit = 4096

// readErrorBody 读取错误响应体前 errBodyLimit 字节并关闭响应体，
// 兼作重试前释放连接的预读。读取的内容仅用于提取 message 字段，
// 绝不进入错误信息或日志。
func readErrorBody(body io.ReadCloser) []byte {
	if body == nil {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(body, errBodyLimit))
	_ = body.Close()
	return data
}

// mapStatusError 将最终失败的非 2xx 响应映射为包内约定的错误类型。
// 错误信息只包含请求路径与状态码，绝不回显原始响应体。
func mapStatusError(requestPath string, statusCode int, errBody []byte) error {
	switch statusCode {
	case http.StatusNotFound:
		return fmt.Errorf("github: GET %s: %w", requestPath, ErrNotFound)
	case http.StatusUnauthorized, http.StatusForbidden:
		return &PermissionError{StatusCode: statusCode, Message: extractMessage(errBody)}
	case http.StatusTooManyRequests:
		return &RateLimitError{}
	default:
		return fmt.Errorf("github: GET %s: unexpected status %d", requestPath, statusCode)
	}
}

// statusCodeRetryable 判断状态码是否可重试：仅 429、502、503、504。
func statusCodeRetryable(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// parseRetryAfter 解析 Retry-After 响应头（非负整数秒）。
// 头缺失、不是整数或为负数时返回 (0, false)。
func parseRetryAfter(header string) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// permissionMessageLimit 是 PermissionError.Message 的最大 rune 数。
const permissionMessageLimit = 256

// extractMessage 从错误响应体 JSON 中提取 message 字段并按 rune 截断到
// permissionMessageLimit 字符；响应体不是合法 JSON 或缺少该字段时返回空串。
func extractMessage(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return truncateRunes(payload.Message, permissionMessageLimit)
}

// truncateRunes 按 rune 数量截断字符串，避免把多字节字符截成非法序列。
func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// validateOwnerRepo 校验 owner 与 repo：非空、不含 "/" 与任何空白字符，
// 防止 URL 路径拼接被注入额外路径段或被空白干扰。
func validateOwnerRepo(owner, repo string) error {
	if err := validatePathSegment(owner, "owner"); err != nil {
		return err
	}
	return validatePathSegment(repo, "repo")
}

// validatePathSegment 校验单个 URL 路径片段。
func validatePathSegment(value, name string) error {
	if value == "" {
		return fmt.Errorf("github: invalid argument: %s must not be empty", name)
	}
	if strings.Contains(value, "/") {
		return fmt.Errorf("github: invalid argument: %s must not contain %q", name, "/")
	}
	if strings.ContainsFunc(value, unicode.IsSpace) {
		return fmt.Errorf("github: invalid argument: %s must not contain whitespace", name)
	}
	return nil
}
