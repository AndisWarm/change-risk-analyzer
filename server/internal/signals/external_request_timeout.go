package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ExternalRequestWithoutTimeoutRuleID is the stable identifier for HTTP request timeout signals.
const ExternalRequestWithoutTimeoutRuleID = "CR-REL-001"

// ExternalRequestWithoutTimeoutAnalyzer detects narrow, directly visible Go HTTP calls without a timeout boundary.
// It is a lexical candidate rule and does not infer the configuration of a named http.Client variable.
type ExternalRequestWithoutTimeoutAnalyzer struct{}

var _ Analyzer = ExternalRequestWithoutTimeoutAnalyzer{}

var (
	defaultHTTPCallPattern = regexp.MustCompile(`\bhttp\.(?:Get|Head|Post|PostForm)\s*\(`)
	defaultClientDoPattern = regexp.MustCompile(`\bhttp\.DefaultClient\.Do\s*\(`)
	inlineClientDoPattern  = regexp.MustCompile(`(?:\(\s*)?&?\s*http\.Client\s*\{([^{}]*)\}\s*\)?\s*\.Do\s*\(`)
	timeoutFieldPattern    = regexp.MustCompile(`\bTimeout\s*:`)
	requestContextPattern  = regexp.MustCompile(`\b(?:WithContext|NewRequestWithContext|WithTimeout|WithDeadline)\s*\(`)
)

// Analyze checks added Go patch lines for directly visible HTTP requests without a timeout or cancellation boundary.
func (ExternalRequestWithoutTimeoutAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	signals := make([]domain.RiskSignal, 0)
	for _, file := range changeSet.Files {
		if !isGoSourcePath(file.NewPath) || file.IsBinary || file.Patch == nil || *file.Patch == "" {
			continue
		}
		lines, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse reliability patch for %s: %w", file.NewPath, err)
		}
		for _, line := range lines {
			if line.side != domain.SideRight {
				continue
			}
			requestKind := unboundedHTTPRequestKind(line.text)
			if requestKind == "" {
				continue
			}
			signals = append(signals, domain.RiskSignal{
				RuleID:   ExternalRequestWithoutTimeoutRuleID,
				Category: domain.CategoryReliability,
				Fact:     "新增外部 HTTP 请求未见超时或取消边界，需确认故障恢复策略",
				Evidence: []domain.Evidence{{
					File:      file.NewPath,
					StartLine: line.line,
					EndLine:   line.line,
					Side:      domain.SideRight,
					Fact:      "新增未见超时边界的 " + requestKind,
				}},
				Source:     domain.SourceDeterministic,
				Confidence: 0.8,
				Weight:     20,
			})
		}
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return reliabilitySignalKey(signals[i]) < reliabilitySignalKey(signals[j])
	})
	return signals, nil
}

func unboundedHTTPRequestKind(line string) string {
	code := goCodeLine(line)
	if code == "" {
		return ""
	}
	if defaultHTTPCallPattern.MatchString(code) {
		return "默认 HTTP 辅助方法调用"
	}
	if match := defaultClientDoPattern.FindStringIndex(code); match != nil {
		if !hasVisibleRequestContext(code[match[1]:]) {
			return "http.DefaultClient.Do 调用"
		}
		return ""
	}
	if match := inlineClientDoPattern.FindStringSubmatchIndex(code); match != nil {
		clientFields := code[match[2]:match[3]]
		if !timeoutFieldPattern.MatchString(clientFields) && !hasVisibleRequestContext(code[match[1]:]) {
			return "未配置 Timeout 的内联 http.Client 调用"
		}
	}
	return ""
}

func goCodeLine(line string) string {
	masked := maskQuotedStrings(line)
	if comment := strings.Index(masked, "//"); comment >= 0 {
		masked = masked[:comment]
	}
	return strings.TrimSpace(masked)
}

func hasVisibleRequestContext(call string) bool {
	return requestContextPattern.MatchString(call)
}

func reliabilitySignalKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.RuleID)
}
