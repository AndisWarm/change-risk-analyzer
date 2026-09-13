// version_test.go 验证 AnalyzerVersion 的构建期注入路径（切片 16）。
//
// 发布构建通过 -ldflags -X 替换包级变量 AnalyzerVersion；本测试用白盒
// 方式直接赋值模拟该注入，断言注入值必须同时体现到 --version 输出与
// 报告 analyzer_version 字段，保证「构建注入的版本与运行时可见版本一致」。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"change-risk-analyzer/internal/domain"
	"change-risk-analyzer/internal/report"
)

func TestAnalyzerVersionIsInjectable(t *testing.T) {
	orig := AnalyzerVersion
	t.Cleanup(func() { AnalyzerVersion = orig })
	AnalyzerVersion = "v9.9.9-test"

	t.Run("--version 输出注入版本", func(t *testing.T) {
		code, stdout, stderr := runArgs("--version")
		if code != exitSuccess {
			t.Fatalf("退出码 = %d, want %d; stderr=%s", code, exitSuccess, stderr)
		}
		if !strings.Contains(stdout, "v9.9.9-test") {
			t.Fatalf("--version 输出缺少注入版本: %q", stdout)
		}
		if strings.Contains(stdout, orig) {
			t.Errorf("--version 输出不应再包含默认版本 %q: %q", orig, stdout)
		}
	})

	t.Run("报告 analyzer_version 使用注入版本", func(t *testing.T) {
		dir := t.TempDir()
		eventPath := writeFile(t, dir, "event.json", validEventJSON())
		diffPath := writeFile(t, dir, "changes.diff", sampleDiff)
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
		if rep.AnalyzerVersion != "v9.9.9-test" {
			t.Fatalf("analyzer_version = %q, want 注入版本 %q", rep.AnalyzerVersion, "v9.9.9-test")
		}

		// 注入版本后的报告仍必须通过 schema 校验，保证发布版本号是合法字段值。
		valid, schemaErrors, err := report.ValidateAgainstSchema(jsonBytes)
		if err != nil {
			t.Fatalf("schema 校验执行失败: %v", err)
		}
		if !valid {
			t.Fatalf("注入版本后的报告未通过 schema 校验: %v", schemaErrors)
		}
	})
}

func TestAnalyzerVersionDefault(t *testing.T) {
	if !strings.HasSuffix(AnalyzerVersion, "-dev") {
		t.Errorf("未注入时默认版本应保留 dev 后缀, got %q", AnalyzerVersion)
	}
}
