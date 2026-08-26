// authorization_boundary.go 实现 CR-SEC-003：授权边界变化线索。
//
// 这是词法候选规则：报告「授权相关代码被删除」与「显式跳过或放宽鉴权」
// 两类候选事实。它不判断变更是否真的引入越权漏洞，也不决定最终严重度。
package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// AuthorizationBoundaryRuleID 是授权边界变化规则的稳定标识。
const AuthorizationBoundaryRuleID = "CR-SEC-003"

// AuthorizationBoundaryAnalyzer 检测 Go 源文件中授权相关代码的删除，
// 以及新增行里显式跳过或放宽鉴权的写法。
type AuthorizationBoundaryAnalyzer struct{}

var _ Analyzer = AuthorizationBoundaryAnalyzer{}

var (
	authKeywordPattern = regexp.MustCompile(`(?i)\b(?:authorize|authorise|authenticate|authentication|authmiddleware|auth_middleware|checkauth|check_auth|checkpermission|check_permission|requireauth|require_auth|requirepermission|require_permission|verifytoken|verify_token|isadmin|is_admin|hasrole|has_role|enforcepolicy|enforce_policy)\b`)
	middlewareUseAuth  = regexp.MustCompile(`(?i)\.\s*Use\s*\([^)]*auth`)
	weakeningPattern   = regexp.MustCompile(`(?i)\b(?:skip[_-]?auth|disable[_-]?auth|allow[_-]?all|permit[_-]?all|no[_-]?auth|without[_-]?auth|anonymous[_-]?access|public[_-]?route)\b`)
)

// Analyze 输出按文件合并的授权边界变化 signal；同一文件内删除侧与
// 新增侧证据共存时合并为一条。
func (AuthorizationBoundaryAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	signals := make([]domain.RiskSignal, 0)
	for _, file := range changeSet.Files {
		if file.IsBinary || file.Patch == nil || *file.Patch == "" {
			continue
		}
		if !authorizationScanPath(file.NewPath) {
			continue
		}
		lines, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse authorization boundary patch for %s: %w", file.NewPath, err)
		}
		evidence := authorizationEvidenceForFile(file.NewPath, lines)
		if len(evidence) == 0 {
			continue
		}
		signals = append(signals, domain.RiskSignal{
			RuleID:     AuthorizationBoundaryRuleID,
			Category:   domain.CategorySecurity,
			Fact:       "授权边界可能发生变化",
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 0.7,
			Weight:     25,
		})
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return firstEvidenceKey(signals[i]) < firstEvidenceKey(signals[j])
	})
	return signals, nil
}

func authorizationEvidenceForFile(filePath string, lines []apiPatchLine) []domain.Evidence {
	evidence := make([]domain.Evidence, 0)
	seen := make(map[string]struct{})
	for _, patchLine := range lines {
		if patchLine.line < 1 {
			continue
		}
		code := goCodeLine(patchLine.text)
		if code == "" {
			continue
		}
		side, fact := "", ""
		switch patchLine.side {
		case domain.SideLeft:
			if authKeywordPattern.MatchString(code) || middlewareUseAuth.MatchString(code) {
				side = "left"
				fact = "删除了疑似授权校验或中间件代码，需确认是否有替代防护"
			}
		case domain.SideRight:
			if weakeningPattern.MatchString(code) {
				side = "right"
				fact = "疑似显式跳过或放宽鉴权，需确认意图"
			}
		default:
			continue
		}
		if side == "" {
			continue
		}
		key := side + ":" + fmt.Sprint(patchLine.line)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		evidenceSide := domain.SideLeft
		if side == "right" {
			evidenceSide = domain.SideRight
		}
		evidence = append(evidence, domain.Evidence{
			File:      filePath,
			StartLine: patchLine.line,
			EndLine:   patchLine.line,
			Side:      evidenceSide,
			Fact:      fact,
		})
	}
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].StartLine != evidence[j].StartLine {
			return evidence[i].StartLine < evidence[j].StartLine
		}
		return evidence[i].Side < evidence[j].Side
	})
	return evidence
}

// authorizationScanPath 匹配需要扫描的 Go 源文件路径：
// 排除测试文件与 vendor 目录，拒绝路径穿越输入。
func authorizationScanPath(filePath string) bool {
	normalized, ok := normalizeRelativePath(filePath)
	if !ok {
		return false
	}
	if !strings.HasSuffix(normalized, ".go") || strings.HasSuffix(normalized, "_test.go") {
		return false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "vendor" {
			return false
		}
	}
	base := path.Base(normalized)
	return base != "" && base != "."
}
