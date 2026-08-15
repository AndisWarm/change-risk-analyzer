package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// GoroutineLifecycleRuleID is the stable identifier for goroutine lifecycle signals.
const GoroutineLifecycleRuleID = "CR-CON-001"

// GoroutineLifecycleAnalyzer detects added goroutines without a visible lifecycle or cancellation signal.
// It is a lexical candidate rule and does not prove that a goroutine leaks.
type GoroutineLifecycleAnalyzer struct{}

var _ Analyzer = GoroutineLifecycleAnalyzer{}

var (
	goLaunchPattern          = regexp.MustCompile(`^\s*go\s+(?:func\b|[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*\s*\()`)
	anonymousGoLaunchPattern = regexp.MustCompile(`^\s*go\s+func\b`)
	lifecycleArgumentPattern = regexp.MustCompile(`\b(?:ctx|context|done|stop|quit|cancel|wg|waitGroup)\b`)
	lifecycleBodyPattern     = regexp.MustCompile(`(?:\b(?:ctx|context)\.Done\s*\(|<-\s*(?:ctx|done|stop|quit)\b|\b(?:wg|waitGroup)\.Done\s*\(|\bcancel\s*\()`)
)

// Analyze checks added Go goroutines and emits a line-level candidate when no lifecycle clue is visible.
func (GoroutineLifecycleAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
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
			return nil, fmt.Errorf("parse concurrency patch for %s: %w", file.NewPath, err)
		}
		for index, line := range lines {
			if line.side != domain.SideRight || !isGoroutineLaunch(line.text) || goroutineHasVisibleLifecycle(lines, index) {
				continue
			}
			signals = append(signals, domain.RiskSignal{
				RuleID:   GoroutineLifecycleRuleID,
				Category: domain.CategoryConcurrency,
				Fact:     "新增 goroutine 未见生命周期或取消信号，需确认退出条件",
				Evidence: []domain.Evidence{{
					File:      file.NewPath,
					StartLine: line.line,
					EndLine:   line.line,
					Side:      domain.SideRight,
					Fact:      "新增 go 语句未见 ctx、停止通道或 WaitGroup 生命周期信号",
				}},
				Source:     domain.SourceDeterministic,
				Confidence: 0.65,
				Weight:     20,
			})
		}
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return goroutineSignalKey(signals[i]) < goroutineSignalKey(signals[j])
	})
	return signals, nil
}

func isGoroutineLaunch(line string) bool {
	return goLaunchPattern.MatchString(goCodeLine(line))
}

func goroutineHasVisibleLifecycle(lines []apiPatchLine, start int) bool {
	launch := goCodeLine(lines[start].text)
	if lifecycleArgumentPattern.MatchString(launch) {
		return true
	}
	if !anonymousGoLaunchPattern.MatchString(launch) {
		return false
	}
	return anonymousClosureHasVisibleLifecycle(lines, start)
}

func anonymousClosureHasVisibleLifecycle(lines []apiPatchLine, start int) bool {
	braceDepth := 0
	opened := false
	previousLine := lines[start].line
	for index := start; index < len(lines) && index < start+32; index++ {
		line := lines[index]
		if line.side != domain.SideRight {
			continue
		}
		if index > start && opened && line.line > previousLine+8 {
			return false
		}
		code := goCodeLine(line.text)
		if lifecycleArgumentPattern.MatchString(code) || lifecycleBodyPattern.MatchString(code) {
			return true
		}
		openBraces := strings.Count(code, "{")
		closeBraces := strings.Count(code, "}")
		if openBraces > 0 {
			opened = true
		}
		braceDepth += openBraces - closeBraces
		if opened && braceDepth <= 0 {
			return false
		}
		previousLine = line.line
	}
	return false
}

func goroutineSignalKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.RuleID)
}
