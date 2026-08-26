package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestMeta() PullRequestMeta {
	return PullRequestMeta{
		Owner:   "octo",
		Repo:    "hello",
		Number:  7,
		BaseSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HeadSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
}

func newTestFiles(count int) []FileEntry {
	files := make([]FileEntry, 0, count)
	for i := 1; i <= count; i++ {
		patch := fmt.Sprintf("+line-%d", i)
		files = append(files, FileEntry{
			Filename:  fmt.Sprintf("pkg/file%d.go", i),
			Status:    "modified",
			Additions: 1,
			Patch:     &patch,
		})
	}
	return files
}

func newTestClient(t *testing.T, fileCount int) *FakeClient {
	t.Helper()
	client, err := NewFakeClient(newTestMeta(), newTestFiles(fileCount), 3)
	if err != nil {
		t.Fatalf("NewFakeClient failed: %v", err)
	}
	return client
}

func TestFakeClientPaginationAggregation(t *testing.T) {
	client := newTestClient(t, 7)
	all, err := FetchAllFiles(context.Background(), client, "octo", "hello", 7, client.meta.HeadSHA, 3)
	if err != nil {
		t.Fatalf("FetchAllFiles failed: %v", err)
	}
	if len(all) != 7 {
		t.Fatalf("aggregated %d files, want 7", len(all))
	}
	for i, entry := range all {
		want := fmt.Sprintf("pkg/file%d.go", i+1)
		if entry.Filename != want {
			t.Errorf("file[%d]=%q, want %q", i, entry.Filename, want)
		}
	}
	calls := client.ListCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 page calls, got %d: %+v", len(calls), calls)
	}
	for i, call := range calls {
		if call.Page != i+1 || call.PerPage != 3 {
			t.Errorf("call[%d]=%+v, want page %d perPage 3", i, call, i+1)
		}
	}
}

func TestFakeClientOutOfRangePageAndEmptyList(t *testing.T) {
	client := newTestClient(t, 0)
	page, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, client.meta.HeadSHA, 1, 3)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(page.Files) != 0 || page.NextPage != 0 {
		t.Fatalf("empty repo returned %+v", page)
	}
	all, err := FetchAllFiles(context.Background(), client, "octo", "hello", 7, client.meta.HeadSHA, 3)
	if err != nil || len(all) != 0 {
		t.Fatalf("empty aggregation = %v, %v", all, err)
	}

	filled := newTestClient(t, 2)
	page, err = filled.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, filled.meta.HeadSHA, 9, 3)
	if err != nil {
		t.Fatalf("out-of-range page failed: %v", err)
	}
	if len(page.Files) != 0 || page.NextPage != 0 {
		t.Fatalf("out-of-range page returned %+v", page)
	}
}

func TestFakeClientRateLimitOneShotWithRetryAfter(t *testing.T) {
	client := newTestClient(t, 1)
	client.InjectNextListError(&RateLimitError{RetryAfter: 17 * time.Second})

	_, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, client.meta.HeadSHA, 1, 3)
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("expected RateLimitError, got %v", err)
	}
	if rateLimit.RetryAfter != 17*time.Second {
		t.Errorf("RetryAfter=%s, want 17s", rateLimit.RetryAfter)
	}

	files, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, client.meta.HeadSHA, 1, 3)
	if err != nil || len(files.Files) != 1 {
		t.Fatalf("one-shot injection was not cleared: files=%+v err=%v", files, err)
	}
}

func TestFakeClientPermissionErrors(t *testing.T) {
	client := newTestClient(t, 1)

	client.InjectNextListError(&PermissionError{StatusCode: 401, Message: "bad credentials"})
	_, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, client.meta.HeadSHA, 1, 3)
	var permission *PermissionError
	if !errors.As(err, &permission) || permission.StatusCode != 401 {
		t.Fatalf("expected 401 PermissionError, got %v", err)
	}

	client.InjectNextMetaError(&PermissionError{StatusCode: 403, Message: "forbidden"})
	_, err = client.GetPullRequest(context.Background(), "octo", "hello", 7)
	if !errors.As(err, &permission) || permission.StatusCode != 403 {
		t.Fatalf("expected 403 PermissionError, got %v", err)
	}
}

func TestFakeClientHeadSHAMismatch(t *testing.T) {
	client := newTestClient(t, 2)
	stale := strings.Repeat("c", 40)

	_, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, stale, 1, 3)
	var mismatch *HeadSHAMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("expected HeadSHAMismatchError, got %v", err)
	}
	if mismatch.Expected != stale || mismatch.Actual != client.meta.HeadSHA {
		t.Errorf("mismatch fields = %+v", mismatch)
	}

	client.SetCurrentHeadSHA(stale)
	files, err := client.ListPullRequestFilesPage(context.Background(), "octo", "hello", 7, stale, 1, 3)
	if err != nil || len(files.Files) == 0 {
		t.Fatalf("updated SHA should succeed: files=%+v err=%v", files, err)
	}
}

func TestFakeClientNotFoundAndCanceledContext(t *testing.T) {
	client := newTestClient(t, 1)

	if _, err := client.GetPullRequest(context.Background(), "octo", "hello", 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong number: expected ErrNotFound, got %v", err)
	}
	if _, err := client.ListPullRequestFilesPage(context.Background(), "other", "hello", 7, client.meta.HeadSHA, 1, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong owner: expected ErrNotFound, got %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.GetPullRequest(canceled, "octo", "hello", 7); !errors.Is(err, context.Canceled) {
		t.Fatalf("meta: canceled context ignored, got %v", err)
	}
	if _, err := client.ListPullRequestFilesPage(canceled, "octo", "hello", 7, client.meta.HeadSHA, 1, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("list: canceled context ignored, got %v", err)
	}
}

func TestFakeClientConstructorAndPageSizeValidation(t *testing.T) {
	if _, err := NewFakeClient(newTestMeta(), nil, 0); err == nil {
		t.Fatal("zero page size accepted by constructor")
	}
	client := newTestClient(t, 1)
	if err := client.SetPageSize(-1); err == nil {
		t.Fatal("negative page size accepted")
	}
}

func TestFakeClientConcurrentAccessUnderRace(t *testing.T) {
	client := newTestClient(t, 20)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := 0; round < 20; round++ {
				ctx := context.Background()
				if _, err := client.GetPullRequest(ctx, "octo", "hello", 7); err != nil {
					t.Errorf("worker %d meta read failed: %v", worker, err)
					return
				}
				if _, err := FetchAllFiles(ctx, client, "octo", "hello", 7, client.meta.HeadSHA, 3); err != nil {
					t.Errorf("worker %d list failed: %v", worker, err)
					return
				}
			}
		}(worker)
	}
	// 并发调整分页大小：聚合结果允许重叠或跳过（不断言内容），只验证
	// 不发生数据竞争、不 panic、循环可终止。
	for size := 1; size <= 4; size++ {
		wg.Add(1)
		go func(size int) {
			defer wg.Done()
			for round := 0; round < 10; round++ {
				if err := client.SetPageSize(size); err != nil {
					t.Errorf("SetPageSize(%d) failed: %v", size, err)
					return
				}
			}
		}(size)
	}
	wg.Wait()
}
