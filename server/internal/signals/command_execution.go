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

// UntrustedCommandExecutionRuleID 是外部输入进入命令执行入口线索的稳定标识。
const UntrustedCommandExecutionRuleID = "CR-EXEC-001"

// UntrustedCommandExecutionAnalyzer 检测 Go patch 中带动态参数的 exec.Command 调用。
// 该规则只产生候选 signal，不执行命令，也不证明参数确实来自外部输入。
type UntrustedCommandExecutionAnalyzer struct{}

var _ Analyzer = UntrustedCommandExecutionAnalyzer{}

var (
	execCallPattern       = regexp.MustCompile(`\bexec\.Command(?:Context)?\s*\(`)
	dynamicIdentifier     = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*`)
	staticArgumentKeyword = map[string]struct{}{"nil": {}, "true": {}, "false": {}}
)

// Analyze 检查新增的 Go 命令执行调用，并按调用行生成可复核 Evidence。
func (UntrustedCommandExecutionAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
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
			return nil, fmt.Errorf("parse command patch for %s: %w", file.NewPath, err)
		}
		for _, line := range lines {
			if line.side != domain.SideRight || !containsDynamicCommand(line.text) {
				continue
			}
			signals = append(signals, domain.RiskSignal{
				RuleID:   UntrustedCommandExecutionRuleID,
				Category: domain.CategorySecurity,
				Fact:     "新增命令执行调用包含动态参数，需确认其是否受外部输入影响",
				Evidence: []domain.Evidence{{
					File:      file.NewPath,
					StartLine: line.line,
					EndLine:   line.line,
					Side:      domain.SideRight,
					Fact:      "exec.Command 调用包含动态参数",
				}},
				Source:     domain.SourceDeterministic,
				Confidence: 0.75,
				Weight:     30,
			})
		}
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return commandSignalKey(signals[i]) < commandSignalKey(signals[j])
	})
	return signals, nil
}

func containsDynamicCommand(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
		return false
	}
	masked := maskQuotedStrings(line)
	match := execCallPattern.FindStringIndex(masked)
	if match == nil {
		return false
	}
	callName := masked[match[0]:match[1]]
	call := line[match[1]:]
	if comment := strings.Index(call, "//"); comment >= 0 {
		call = call[:comment]
	}
	if strings.Contains(callName, "CommandContext") {
		if comma := strings.IndexByte(call, ','); comma >= 0 {
			call = call[comma+1:]
		}
	}
	return hasDynamicArguments(call)
}

func hasDynamicArguments(arguments string) bool {
	stripped := stripQuotedStrings(arguments)
	if strings.Contains(stripped, "+") || strings.Contains(stripped, "fmt.Sprintf") {
		return true
	}
	for _, identifier := range dynamicIdentifier.FindAllString(stripped, -1) {
		if _, ok := staticArgumentKeyword[identifier]; ok {
			continue
		}
		return true
	}
	return false
}

func stripQuotedStrings(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		quote := value[i]
		if quote != '"' && quote != '\'' && quote != '`' {
			builder.WriteByte(quote)
			continue
		}
		i++
		for i < len(value) {
			if quote != '`' && value[i] == '\\' {
				i++
				continue
			}
			if value[i] == quote {
				break
			}
			i++
		}
		builder.WriteString("\"\"")
	}
	return builder.String()
}

func maskQuotedStrings(value string) string {
	masked := []byte(value)
	var quote byte
	escaped := false
	for i := 0; i < len(masked); i++ {
		if quote == 0 {
			if masked[i] == '"' || masked[i] == '\'' || masked[i] == '`' {
				quote = masked[i]
			}
			continue
		}
		current := masked[i]
		masked[i] = ' '
		if quote != '`' && escaped {
			escaped = false
			continue
		}
		if quote != '`' && current == '\\' {
			escaped = true
			continue
		}
		if current == quote {
			quote = 0
		}
	}
	return string(masked)
}

func isGoSourcePath(filePath string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(path.Clean(filePath), "./"))
	if normalized == ".." || strings.HasPrefix(normalized, "../") || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\\") {
		return false
	}
	return strings.HasSuffix(normalized, ".go") && !strings.HasSuffix(normalized, "_test.go")
}

func commandSignalKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.RuleID)
}
