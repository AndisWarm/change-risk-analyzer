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

// DestructiveMigrationRuleID 是破坏性迁移线索的稳定标识。
const DestructiveMigrationRuleID = "CR-DATA-001"

// DestructiveMigrationAnalyzer 检测迁移 SQL 中新增的不可逆数据操作。
// 规则只提供候选事实，不判断是否存在回滚、备份或双写策略。
type DestructiveMigrationAnalyzer struct{}

var _ Analyzer = DestructiveMigrationAnalyzer{}

var (
	dropTablePattern  = regexp.MustCompile(`(?i)\bdrop\s+table\b`)
	dropColumnPattern = regexp.MustCompile(`(?i)\bdrop\s+column\b`)
	truncatePattern   = regexp.MustCompile(`(?i)\btruncate(?:\s+table)?\b`)
	deletePattern     = regexp.MustCompile(`(?i)\bdelete\s+from\b`)
	whereAlwaysTrue   = regexp.MustCompile(`(?i)\bwhere\s+(?:1\s*=\s*1|true)\b`)
)

type destructiveOperation struct {
	name string
	line apiPatchLine
}

// Analyze 检查迁移 SQL 新增行中的 DROP/TRUNCATE/无界 DELETE 操作。
func (DestructiveMigrationAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	signals := make([]domain.RiskSignal, 0)
	for _, file := range changeSet.Files {
		if !isMigrationSQLPath(file.NewPath) || file.IsBinary || file.Patch == nil || *file.Patch == "" {
			continue
		}
		lines, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse migration patch for %s: %w", file.NewPath, err)
		}
		for _, operation := range destructiveOperations(lines) {
			signals = append(signals, domain.RiskSignal{
				RuleID:   DestructiveMigrationRuleID,
				Category: domain.CategoryData,
				Fact:     fmt.Sprintf("迁移新增破坏性数据操作: %s", operation.name),
				Evidence: []domain.Evidence{{
					File:      file.NewPath,
					StartLine: operation.line.line,
					EndLine:   operation.line.line,
					Side:      domain.SideRight,
					Fact:      fmt.Sprintf("新增迁移操作: %s", operation.name),
				}},
				Source:     domain.SourceDeterministic,
				Confidence: 0.85,
				Weight:     35,
			})
		}
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return migrationSignalKey(signals[i]) < migrationSignalKey(signals[j])
	})
	return signals, nil
}

func destructiveOperations(lines []apiPatchLine) []destructiveOperation {
	operations := make([]destructiveOperation, 0)
	for _, line := range lines {
		if line.side != domain.SideRight {
			continue
		}
		text := strings.TrimSpace(line.text)
		if text == "" || strings.HasPrefix(text, "--") || strings.HasPrefix(text, "/*") || strings.HasPrefix(text, "*") {
			continue
		}
		text = strings.TrimSpace(stripSQLLineComment(maskQuotedStrings(text)))
		if text == "" {
			continue
		}
		switch {
		case dropTablePattern.MatchString(text):
			operations = append(operations, destructiveOperation{name: "DROP TABLE", line: line})
		case dropColumnPattern.MatchString(text):
			operations = append(operations, destructiveOperation{name: "DROP COLUMN", line: line})
		case truncatePattern.MatchString(text):
			operations = append(operations, destructiveOperation{name: "TRUNCATE", line: line})
		case deletePattern.MatchString(text) && (strings.Contains(text, ";") || whereAlwaysTrue.MatchString(text)) && !hasScopedWhere(text):
			operations = append(operations, destructiveOperation{name: "UNBOUNDED DELETE", line: line})
		}
	}
	return operations
}

func stripSQLLineComment(text string) string {
	if index := strings.Index(text, "--"); index >= 0 {
		return text[:index]
	}
	return text
}

func hasScopedWhere(text string) bool {
	where := strings.Index(strings.ToLower(text), "where")
	if where < 0 {
		return false
	}
	condition := strings.TrimSpace(text[where+len("where"):])
	return condition != "" && !whereAlwaysTrue.MatchString(text)
}

func isMigrationSQLPath(filePath string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(path.Clean(filePath), "./"))
	if normalized == ".." || strings.HasPrefix(normalized, "../") || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\\") {
		return false
	}
	if path.Ext(normalized) != ".sql" {
		return false
	}
	segments := strings.Split(normalized, "/")
	for _, segment := range segments[:len(segments)-1] {
		if segment == "migration" || segment == "migrations" || segment == "migrate" {
			return true
		}
	}
	return false
}

func migrationSignalKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.RuleID)
}
