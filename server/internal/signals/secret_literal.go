// secret_literal.go 实现 CR-SEC-002：新增行中疑似密钥或凭证字面量的线索。
//
// 这是词法候选规则：只报告「这一行可能包含密钥」的事实，不判断密钥是否
// 真实有效。安全不变量：Evidence 的任何字段都不得包含完整原始密钥值，
// 事实描述只使用类型、键名和行号。
package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// SecretLiteralRuleID 是疑似密钥字面量规则的稳定标识。
const SecretLiteralRuleID = "CR-SEC-002"

// SecretLiteralAnalyzer 检测新增 patch 行中的已知令牌格式、私钥块标记
// 和指向字面量的密钥类赋值。
type SecretLiteralAnalyzer struct{}

var _ Analyzer = SecretLiteralAnalyzer{}

var (
	privateKeyBlockPattern = regexp.MustCompile(`-----BEGIN(?: [A-Z0-9]+)* PRIVATE KEY-----`)
	githubTokenPattern     = regexp.MustCompile(`gh[pousr]_[0-9A-Za-z]{16,}`)
	awsAccessKeyPattern    = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)
	slackTokenPattern      = regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)
	googleAPIKeyPattern    = regexp.MustCompile(`AIza[0-9A-Za-z_-]{30,}`)
	// 键名允许出现在更长变量名尾部（如 deploy_token），但分隔符必须紧随其后。
	secretAssignPattern = regexp.MustCompile(`(?i)\b(?:[A-Za-z0-9_]*?)(password|passwd|secret|token|api[_-]?key|access[_-]?key|access[_-]?token|private[_-]?key|auth[_-]?token)["']?\s*[:=]{1,2}\s*["']?([^"'\s]{6,})`)
	valueTailTrim       = regexp.MustCompile(`[),.;]+$`)
	placeholderHint     = regexp.MustCompile(`(?i)(example|sample|placeholder|dummy|changeme|change_me|changeit|your_|your-|xxx|\*\*\*|todo|insert|redacted|masked|getenv|process\.env|os\.environ|notset|not_set|\$\{|<[^>]*>)`)
)

// Analyze 对新增行输出按文件合并的疑似密钥 signal。
func (SecretLiteralAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
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
		if _, ok := normalizeRelativePath(file.NewPath); !ok {
			continue
		}
		lines, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse secret literal patch for %s: %w", file.NewPath, err)
		}
		evidence := secretEvidenceForFile(file.NewPath, lines)
		if len(evidence) == 0 {
			continue
		}
		signals = append(signals, domain.RiskSignal{
			RuleID:     SecretLiteralRuleID,
			Category:   domain.CategorySecurity,
			Fact:       "新增了疑似密钥或凭证字面量",
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 0.85,
			Weight:     30,
		})
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return firstEvidenceKey(signals[i]) < firstEvidenceKey(signals[j])
	})
	return signals, nil
}

func secretEvidenceForFile(filePath string, lines []apiPatchLine) []domain.Evidence {
	evidence := make([]domain.Evidence, 0)
	seen := make(map[int]struct{})
	for _, patchLine := range lines {
		if patchLine.side != domain.SideRight || patchLine.line < 1 {
			continue
		}
		text := secretLineText(patchLine.text)
		if text == "" {
			continue
		}
		kind, ok := classifySecretLine(text)
		if !ok {
			continue
		}
		if _, duplicate := seen[patchLine.line]; duplicate {
			continue
		}
		seen[patchLine.line] = struct{}{}
		evidence = append(evidence, domain.Evidence{
			File:      filePath,
			StartLine: patchLine.line,
			EndLine:   patchLine.line,
			Side:      domain.SideRight,
			Fact:      fmt.Sprintf("新增%s（原始值未写入报告）", kind),
		})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].StartLine < evidence[j].StartLine })
	return evidence
}

// classifySecretLine 返回该行的疑似密钥类型描述；ok 为 false 表示不是候选。
func classifySecretLine(text string) (string, bool) {
	if privateKeyBlockPattern.MatchString(text) {
		return "私钥块起始标记", true
	}
	for _, known := range []struct {
		pattern *regexp.Regexp
		label   string
	}{
		{githubTokenPattern, "GitHub Token 形态令牌"},
		{awsAccessKeyPattern, "AWS Access Key ID 形态密钥"},
		{slackTokenPattern, "Slack Token 形态令牌"},
		{googleAPIKeyPattern, "Google API Key 形态密钥"},
	} {
		match := known.pattern.FindString(text)
		if match == "" {
			continue
		}
		if placeholderHint.MatchString(match) {
			continue
		}
		return known.label, true
	}
	if match := secretAssignPattern.FindStringSubmatch(text); match != nil {
		keyName := strings.ToLower(match[1])
		value := valueTailTrim.ReplaceAllString(match[2], "")
		if secretValueLooksReal(value) {
			return fmt.Sprintf("疑似密钥赋值（键 %s，值已脱敏）", keyName), true
		}
	}
	return "", false
}

// secretValueLooksReal 过滤占位符与引用形态，只保留看起来是真实字面量的值。
// 取舍：要求值至少含一个数字或符号且非纯数字，纯字母口令会漏报，
// 该取舍用于压低误报，已在规则文档中记录。
func secretValueLooksReal(value string) bool {
	if value == "" || strings.HasPrefix(value, "$") {
		return false
	}
	if placeholderHint.MatchString(value) {
		return false
	}
	distinct := make(map[rune]struct{})
	digitsOrSymbols := false
	lettersOnly := true
	for _, r := range value {
		distinct[r] = struct{}{}
		switch {
		case unicode.IsDigit(r):
			digitsOrSymbols = true
			lettersOnly = false
		case !unicode.IsLetter(r):
			digitsOrSymbols = true
			lettersOnly = false
		}
	}
	if len(distinct) <= 2 || lettersOnly {
		return false
	}
	if !digitsOrSymbols {
		return false
	}
	allDigits := true
	for r := range distinct {
		if !unicode.IsDigit(r) {
			allDigits = false
			break
		}
	}
	return !allDigits
}

// secretLineText 去掉整行注释与行内注释；注释行返回空字符串。
// 行内注释仅按 " #" 与 " //" 切分（URL 中的 // 前没有空格，不受影响）。
func secretLineText(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return ""
	}
	if comment := strings.Index(line, " #"); comment >= 0 {
		line = line[:comment]
	}
	if comment := strings.Index(line, " //"); comment >= 0 {
		line = line[:comment]
	}
	return strings.TrimSpace(line)
}
