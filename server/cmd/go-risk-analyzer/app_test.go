// app_test.go 覆盖离线 CLI 的端到端正例与主要操作失败路径。
// 全部用例通过同包 Run/runAnalyze 直接调用，不启动子进程、不访问网络。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"change-risk-analyzer/internal/domain"
	"change-risk-analyzer/internal/report"
)

// 与其他包一致的固定测试 SHA，保证 fixture 可复现。
const (
	testBaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testHeadSHA = "abcdef0123456789abcdef0123456789abcdef01"
)

// validEventJSON 与 internal/event 测试使用的最小合法 pull_request 事件同构。
func validEventJSON() string {
	return `{
		"action":"opened",
		"number":42,
		"repository":{"owner":{"login":"acme"},"name":"risk","full_name":"acme/risk"},
		"pull_request":{
			"base":{"sha":"` + testBaseSHA + `"},
			"head":{"sha":"` + testHeadSHA + `","repo":{"full_name":"acme/risk"}}
		}
	}`
}

// sampleDiff 触发两条 CR-SEC-001 Workflow 写权限线索（各 30 分，合计
// 60 分 → 总体 high），其余九条规则对该输入保持沉默。
const sampleDiff = `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -0,0 +1,3 @@
+name: CI
+on: [push]
+  contents: write
diff --git a/.github/workflows/deploy.yml b/.github/workflows/deploy.yml
--- a/.github/workflows/deploy.yml
+++ b/.github/workflows/deploy.yml
@@ -1 +1,2 @@
-name: Deploy
+name: Deploy
+permissions: write-all
`

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 fixture %s 失败: %v", name, err)
	}
	return path
}

// runArgs 执行一次 CLI 并返回退出码与两个标准流的内容。
func runArgs(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestAnalyzeEndToEndProducesValidReports(t *testing.T) {
	dir := t.TempDir()
	eventPath := writeFile(t, dir, "event.json", validEventJSON())
	diffPath := writeFile(t, dir, "changes.diff", sampleDiff)
	outDir := filepath.Join(dir, "out")
	before := time.Now().Add(-time.Minute)

	code, stdout, stderr := runArgs("analyze", "--event", eventPath, "--diff", diffPath, "--output", outDir)
	if code != exitSuccess {
		t.Fatalf("退出码 = %d, want %d; stderr=%s", code, exitSuccess, stderr)
	}
	if !strings.Contains(stdout, "分析完成") {
		t.Fatalf("stdout 缺少完成提示: %q", stdout)
	}

	jsonBytes, err := os.ReadFile(filepath.Join(outDir, reportJSONFileName))
	if err != nil {
		t.Fatalf("risk-report.json 未生成: %v", err)
	}
	mdBytes, err := os.ReadFile(filepath.Join(outDir, reportMDFileName))
	if err != nil {
		t.Fatalf("risk-report.md 未生成: %v", err)
	}

	valid, schemaErrors, err := report.ValidateAgainstSchema(jsonBytes)
	if err != nil {
		t.Fatalf("schema 校验执行失败: %v", err)
	}
	if !valid {
		t.Fatalf("报告未通过 schema 校验: %v", schemaErrors)
	}

	var rep domain.RiskReport
	if err := json.Unmarshal(jsonBytes, &rep); err != nil {
		t.Fatalf("报告 JSON 反序列化失败: %v", err)
	}
	res, err := report.Validate(&rep)
	if err != nil {
		t.Fatalf("领域校验执行失败: %v", err)
	}
	if !res.Valid() {
		t.Fatalf("报告未通过双重校验: %+v", res)
	}

	// 请求身份必须来自事件文件。
	if rep.Request.HeadSHA != testHeadSHA || rep.Request.PullRequestNumber != 42 ||
		rep.Request.Repository.FullName != "acme/risk" {
		t.Fatalf("请求身份不符: %+v", rep.Request)
	}
	if rep.AnalyzerVersion != AnalyzerVersion {
		t.Fatalf("analyzer_version = %q, want %q", rep.AnalyzerVersion, AnalyzerVersion)
	}
	if rep.GeneratedAt.Before(before) || time.Until(rep.GeneratedAt) > time.Minute {
		t.Fatalf("generated_at 不是本次运行的当前时间: %v", rep.GeneratedAt)
	}

	// 高风险也不改变成功语义：两条安全线索合计 60 分（high），退出码仍为 0。
	if rep.OverallScore != 60 || rep.OverallLevel != domain.SeverityHigh {
		t.Fatalf("overall = %d/%s, want 60/high", rep.OverallScore, rep.OverallLevel)
	}
	if rep.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want completed", rep.Status)
	}
	if len(rep.Findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(rep.Findings))
	}
	for _, f := range rep.Findings {
		if f.RuleIDs[0] != "CR-SEC-001" {
			t.Fatalf("finding 规则 = %s, want CR-SEC-001", f.RuleIDs[0])
		}
	}
	if len(rep.Dimensions) != 1 || rep.Dimensions[0].Category != domain.CategorySecurity ||
		rep.Dimensions[0].Score != 60 || rep.Dimensions[0].SignalCount != 2 {
		t.Fatalf("维度不符: %+v", rep.Dimensions)
	}

	markdown := string(mdBytes)
	if !strings.Contains(markdown, "# 变更风险分析报告") ||
		!strings.Contains(markdown, "## 需要优先确认的发现") {
		t.Fatal("markdown 缺少预期标题片段")
	}
}

func TestAnalyzeEmptyDiffYieldsValidLowReport(t *testing.T) {
	dir := t.TempDir()
	eventPath := writeFile(t, dir, "event.json", validEventJSON())
	diffPath := writeFile(t, dir, "empty.diff", "")
	outDir := filepath.Join(dir, "out")

	code, _, stderr := runArgs("analyze", "--event", eventPath, "--diff", diffPath, "--output", outDir)
	if code != exitSuccess {
		t.Fatalf("退出码 = %d, want %d; stderr=%s", code, exitSuccess, stderr)
	}
	jsonBytes, err := os.ReadFile(filepath.Join(outDir, reportJSONFileName))
	if err != nil {
		t.Fatalf("risk-report.json 未生成: %v", err)
	}
	var rep domain.RiskReport
	if err := json.Unmarshal(jsonBytes, &rep); err != nil {
		t.Fatalf("报告 JSON 反序列化失败: %v", err)
	}
	if rep.OverallScore != 0 || rep.OverallLevel != domain.SeverityLow {
		t.Fatalf("overall = %d/%s, want 0/low", rep.OverallScore, rep.OverallLevel)
	}
	if rep.Status != domain.StatusCompleted {
		t.Fatalf("status = %q, want completed", rep.Status)
	}
	if len(rep.Findings) != 0 {
		t.Fatalf("findings = %d, want 0", len(rep.Findings))
	}
	if !strings.Contains(string(jsonBytes), `"findings": []`) {
		t.Fatal("空 findings 应序列化为 [] 而不是 null")
	}
	if _, err := os.Stat(filepath.Join(outDir, reportMDFileName)); err != nil {
		t.Fatalf("risk-report.md 未生成: %v", err)
	}
}

func TestAnalyzeRequiresFlags(t *testing.T) {
	dir := t.TempDir()
	eventPath := writeFile(t, dir, "event.json", validEventJSON())
	diffPath := writeFile(t, dir, "changes.diff", sampleDiff)
	outDir := filepath.Join(dir, "out")

	tests := []struct {
		name   string
		args   []string
		wantIn string
	}{
		{
			name:   "no arguments",
			args:   nil,
			wantIn: "用法",
		},
		{
			name:   "missing event flag",
			args:   []string{"analyze", "--diff", diffPath, "--output", outDir},
			wantIn: "--event",
		},
		{
			name:   "missing diff flag",
			args:   []string{"analyze", "--event", eventPath, "--output", outDir},
			wantIn: "--diff",
		},
		{
			name:   "missing output flag",
			args:   []string{"analyze", "--event", eventPath, "--diff", diffPath},
			wantIn: "--output",
		},
		{
			name:   "unknown subcommand",
			args:   []string{"frobnicate"},
			wantIn: "无法识别",
		},
		{
			name:   "unexpected positional argument",
			args:   []string{"analyze", "--event", eventPath, "--diff", diffPath, "--output", outDir, "extra"},
			wantIn: "未识别的参数",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runArgs(tc.args...)
			if code != exitUsage {
				t.Fatalf("退出码 = %d, want %d; stderr=%s", code, exitUsage, stderr)
			}
			if !strings.Contains(stderr, tc.wantIn) {
				t.Fatalf("stderr 缺少 %q: %s", tc.wantIn, stderr)
			}
		})
	}
}

func TestAnalyzeReportsOperationalFailures(t *testing.T) {
	dir := t.TempDir()
	eventPath := writeFile(t, dir, "event.json", validEventJSON())
	diffPath := writeFile(t, dir, "changes.diff", sampleDiff)
	outDir := filepath.Join(dir, "out")

	tests := []struct {
		name   string
		args   []string
		wantIn []string
	}{
		{
			name:   "event file does not exist",
			args:   []string{"--event", filepath.Join(dir, "missing.json"), "--diff", diffPath, "--output", outDir},
			wantIn: []string{"读取事件文件失败"},
		},
		{
			name:   "event json invalid",
			args:   []string{"--event", writeFile(t, dir, "bad-event.json", "{"), "--diff", diffPath, "--output", outDir},
			wantIn: []string{"解析事件文件失败", "invalid JSON"},
		},
		{
			name:   "diff file does not exist",
			args:   []string{"--event", eventPath, "--diff", filepath.Join(dir, "missing.diff"), "--output", outDir},
			wantIn: []string{"读取 diff 文件失败"},
		},
		{
			name:   "diff malformed",
			args:   []string{"--event", eventPath, "--diff", writeFile(t, dir, "bad.diff", "not a diff"), "--output", outDir},
			wantIn: []string{"解析 diff 文件失败", "missing diff --git file header"},
		},
		{
			name:   "output path is a regular file",
			args:   []string{"--event", eventPath, "--diff", diffPath, "--output", writeFile(t, dir, "occupied.txt", "x")},
			wantIn: []string{"创建输出目录失败"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runArgs(append([]string{"analyze"}, tc.args...)...)
			if code != exitFailure {
				t.Fatalf("退出码 = %d, want %d; stderr=%s", code, exitFailure, stderr)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(stderr, want) {
					t.Fatalf("stderr 缺少 %q: %s", want, stderr)
				}
			}
		})
	}
}

func TestVersionFlagPrintsAndExitsZero(t *testing.T) {
	code, stdout, stderr := runArgs("--version")
	if code != exitSuccess {
		t.Fatalf("顶层 --version 退出码 = %d, want %d; stderr=%s", code, exitSuccess, stderr)
	}
	if !strings.Contains(stdout, AnalyzerVersion) {
		t.Fatalf("stdout 缺少版本号 %q: %q", AnalyzerVersion, stdout)
	}

	code, stdout, stderr = runArgs("analyze", "--version")
	if code != exitSuccess {
		t.Fatalf("analyze --version 退出码 = %d, want %d; stderr=%s", code, exitSuccess, stderr)
	}
	if !strings.Contains(stdout, AnalyzerVersion) {
		t.Fatalf("stdout 缺少版本号 %q: %q", AnalyzerVersion, stdout)
	}
}
