// runner.go 实现确定性分析器的统一运行器：按注册顺序执行全部分析器，
// 聚合、去重并稳定排序输出。它不计算分数与门禁，不访问网络，也不执行
// 仓库代码。
//
// 排序与去重约定（与 spec/07 第 6 节对应）：
//   - 去重键：RuleID + 全部 Evidence 的（文件、起止行、侧别、事实）签名，
//     完全相同的信号只保留一条；
//   - 全局排序键：类别（固定维度顺序）→ 首个证据文件路径 → 起始行 →
//     侧别 → 规则 ID → 事实；同一输入重复运行输出完全一致。
package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"sort"
	"strings"
)

// NamedAnalyzer 将稳定规则标识与分析器实例配对，供运行器注册。
type NamedAnalyzer struct {
	ID       string
	Analyzer Analyzer
}

// Runner 按注册顺序运行分析器。
type Runner struct {
	analyzers []NamedAnalyzer
}

// NewRunner 返回使用给定注册表的运行器。ID 不能为空或重复，
// Analyzer 不能为 nil。
func NewRunner(analyzers ...NamedAnalyzer) (*Runner, error) {
	seen := make(map[string]struct{}, len(analyzers))
	for _, named := range analyzers {
		if named.ID == "" {
			return nil, fmt.Errorf("signals: analyzer id must not be empty")
		}
		if named.Analyzer == nil {
			return nil, fmt.Errorf("signals: analyzer %s must not be nil", named.ID)
		}
		if _, duplicate := seen[named.ID]; duplicate {
			return nil, fmt.Errorf("signals: duplicate analyzer id %s", named.ID)
		}
		seen[named.ID] = struct{}{}
	}
	return &Runner{analyzers: append([]NamedAnalyzer(nil), analyzers...)}, nil
}

// DefaultRunner 返回注册了全部已实现确定性规则的运行器。
// 注册顺序与 spec/07 第 4 节的规则顺序一致。
func DefaultRunner() *Runner {
	return &Runner{analyzers: []NamedAnalyzer{
		{WorkflowPermissionRuleID, WorkflowPermissionAnalyzer{}},
		{ExportedAPIChangeRuleID, ExportedAPIAnalyzer{}},
		{UntrustedCommandExecutionRuleID, UntrustedCommandExecutionAnalyzer{}},
		{DestructiveMigrationRuleID, DestructiveMigrationAnalyzer{}},
		{ExternalRequestWithoutTimeoutRuleID, ExternalRequestWithoutTimeoutAnalyzer{}},
		{GoroutineLifecycleRuleID, GoroutineLifecycleAnalyzer{}},
		{FloatingReferenceRuleID, FloatingReferenceAnalyzer{}},
		{SecretLiteralRuleID, SecretLiteralAnalyzer{}},
		{AuthorizationBoundaryRuleID, AuthorizationBoundaryAnalyzer{}},
		{TestEvidenceRuleID, TestEvidenceAnalyzer{}},
	}}
}

// Run 按注册顺序执行全部分析器，聚合、去重并稳定排序输出。
// 任一分析器失败或上下文取消时立即返回携带其规则 ID 的错误，
// 后续分析器不再执行。
func (r *Runner) Run(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("signals: run canceled: %w", err)
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	collected := make([]domain.RiskSignal, 0)
	for _, named := range r.analyzers {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("signals: run canceled before %s: %w", named.ID, err)
		}
		signals, err := named.Analyzer.Analyze(ctx, changeSet)
		if err != nil {
			return nil, fmt.Errorf("signals: analyzer %s failed: %w", named.ID, err)
		}
		collected = append(collected, signals...)
	}

	collected = dedupeRunnerSignals(collected)
	sortRunnerSignals(collected)
	return collected, nil
}

// dedupeRunnerSignals 移除完全相同的信号：RuleID 与全部 Evidence 签名
// 一致即视为重复，保留首个。
func dedupeRunnerSignals(signals []domain.RiskSignal) []domain.RiskSignal {
	seen := make(map[string]struct{}, len(signals))
	kept := signals[:0]
	for _, signal := range signals {
		key := runnerSignalKey(signal)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, signal)
	}
	return kept
}

func runnerSignalKey(signal domain.RiskSignal) string {
	var builder strings.Builder
	builder.WriteString(signal.RuleID)
	builder.WriteByte('|')
	builder.WriteString(signal.Fact)
	builder.WriteByte('|')
	for _, evidence := range signal.Evidence {
		fmt.Fprintf(&builder, "%s|%d|%d|%s|%s|", evidence.File, evidence.StartLine, evidence.EndLine, evidence.Side, evidence.Fact)
	}
	return builder.String()
}

// runnerCategoryRank 与 domain 包内固定维度顺序保持一致；
// 若 domain 维度顺序变化，此处必须同步。
var runnerCategoryRank = map[domain.RiskCategory]int{
	domain.CategorySecurity:    0,
	domain.CategoryData:        1,
	domain.CategoryAPI:         2,
	domain.CategoryReliability: 3,
	domain.CategoryConcurrency: 4,
	domain.CategoryPerformance: 5,
	domain.CategoryDelivery:    6,
	domain.CategorySupplyChain: 7,
	domain.CategoryTestability: 8,
}

func sortRunnerSignals(signals []domain.RiskSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		left, right := runnerSortKey(signals[i]), runnerSortKey(signals[j])
		if left.category != right.category {
			return left.category < right.category
		}
		if left.file != right.file {
			return left.file < right.file
		}
		if left.line != right.line {
			return left.line < right.line
		}
		if left.side != right.side {
			return left.side < right.side
		}
		if left.ruleID != right.ruleID {
			return left.ruleID < right.ruleID
		}
		return left.fact < right.fact
	})
}

type runnerSortKeyStruct struct {
	category int
	file     string
	line     int
	side     string
	ruleID   string
	fact     string
}

func runnerSortKey(signal domain.RiskSignal) runnerSortKeyStruct {
	category, ok := runnerCategoryRank[signal.Category]
	if !ok {
		category = len(runnerCategoryRank)
	}
	file, line, side := "", 0, ""
	if len(signal.Evidence) > 0 {
		file = signal.Evidence[0].File
		line = signal.Evidence[0].StartLine
		side = string(signal.Evidence[0].Side)
	}
	return runnerSortKeyStruct{
		category: category,
		file:     file,
		line:     line,
		side:     side,
		ruleID:   signal.RuleID,
		fact:     signal.Fact,
	}
}
