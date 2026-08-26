// test_evidence.go 实现 CR-TEST-001：敏感路径变更缺少观察到的配套测试证据。
//
// 这是弱线索规则：只报告「本次变更中未观察到配套测试类文件的变更」这一
// 观察事实。它不断言「没有测试」——测试可能存在于未变更的既有用例或
// 仓库之外，最终判断由人工与策略层完成。
package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"sort"
	"strings"
)

// TestEvidenceRuleID 是测试证据缺失候选规则的稳定标识。
const TestEvidenceRuleID = "CR-TEST-001"

// TestEvidenceAnalyzer 检查敏感路径的变更在同一 ChangeSet 中是否观察到
// 配套测试类变更；未观察到时输出谨慎措辞的候选信号。
type TestEvidenceAnalyzer struct{}

var _ Analyzer = TestEvidenceAnalyzer{}

// sensitivePathKeywords 按路径段子串匹配（小写）定义敏感变更。
var sensitivePathKeywords = []string{"migration", "auth", "payment", "billing", "security", "admin"}

// Analyze 对未观察到配套测试变更的敏感文件按顶级目录分组输出 signal。
func (TestEvidenceAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	testCoveredDirs := make(map[string]struct{})
	for _, file := range changeSet.Files {
		normalized, ok := normalizeRelativePath(file.NewPath)
		if !ok {
			continue
		}
		if isTestEvidencePath(normalized) {
			testCoveredDirs[topLevelDir(normalized)] = struct{}{}
		}
	}

	groups := make(map[string][]domain.Evidence)
	for _, file := range changeSet.Files {
		if file.IsBinary {
			continue
		}
		normalized, ok := normalizeRelativePath(file.NewPath)
		if !ok || isTestEvidencePath(normalized) || !isSensitivePath(normalized) {
			continue
		}
		dir := topLevelDir(normalized)
		if _, covered := testCoveredDirs[dir]; covered {
			continue
		}
		groups[dir] = append(groups[dir], domain.Evidence{
			File: normalized,
			Side: domain.SideFile,
			Fact: fmt.Sprintf("未观察到 %s 的配套测试变更，建议补充验证", normalized),
		})
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	signals := make([]domain.RiskSignal, 0, len(groups))
	for _, key := range keys {
		evidence := groups[key]
		sort.Slice(evidence, func(i, j int) bool { return evidence[i].File < evidence[j].File })
		signals = append(signals, domain.RiskSignal{
			RuleID:     TestEvidenceRuleID,
			Category:   domain.CategoryTestability,
			Fact:       "敏感路径变更未观察到配套测试变更",
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 0.6,
			Weight:     15,
		})
	}
	return signals, nil
}

// isSensitivePath 判断路径是否命中敏感关键词（对每个路径段做小写子串匹配，
// 因此目录名与文件名都会参与，author.go 这类包含 auth 子串的名字也会命中，
// 属于已文档化的保守取舍）。
func isSensitivePath(normalized string) bool {
	for _, segment := range strings.Split(normalized, "/") {
		for _, keyword := range sensitivePathKeywords {
			if strings.Contains(segment, keyword) {
				return true
			}
		}
	}
	return false
}

// isTestEvidencePath 判断路径是否为测试类变更：`_test.go` 后缀，
// 或路径中含 tests/test/testdata 目录段。
func isTestEvidencePath(normalized string) bool {
	if strings.HasSuffix(normalized, "_test.go") {
		return true
	}
	for _, segment := range strings.Split(normalized, "/") {
		switch segment {
		case "tests", "test", "testdata":
			return true
		}
	}
	return false
}

// topLevelDir 返回路径的第一级目录；根目录文件返回 "."。
func topLevelDir(normalized string) string {
	if index := strings.Index(normalized, "/"); index >= 0 {
		return normalized[:index]
	}
	return "."
}
