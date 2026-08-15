// Package signals 提供不依赖模型和外部系统的确定性风险线索分析器。
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

// WorkflowPermissionRuleID 是 Workflow 写权限规则的稳定标识。
const WorkflowPermissionRuleID = "CR-SEC-001"

// Analyzer 是确定性分析器的最小接口。
type Analyzer interface {
	Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error)
}

// WorkflowPermissionAnalyzer 检测 Workflow patch 中新增的写权限。
// 这是可复核的确定性线索，不直接计算最终风险分数或门禁状态。
type WorkflowPermissionAnalyzer struct{}

var _ Analyzer = WorkflowPermissionAnalyzer{}

// Analyze 检查每个 Workflow 文件新增的权限声明，并按文件合并证据。
func (WorkflowPermissionAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	signals := make([]domain.RiskSignal, 0)
	for _, file := range changeSet.Files {
		if !isWorkflowPath(file.NewPath) || file.IsBinary || file.Patch == nil || *file.Patch == "" {
			continue
		}
		evidence := workflowWriteEvidence(file.NewPath, *file.Patch)
		if len(evidence) == 0 {
			continue
		}
		signals = append(signals, domain.RiskSignal{
			RuleID:     WorkflowPermissionRuleID,
			Category:   domain.CategorySecurity,
			Fact:       "Workflow 新增了 GitHub Actions 写权限声明",
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 1,
			Weight:     30,
		})
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return firstEvidenceKey(signals[i]) < firstEvidenceKey(signals[j])
	})
	return signals, nil
}

var (
	hunkHeaderPattern       = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	permissionLinePattern   = regexp.MustCompile(`^\s*(contents|actions|checks|deployments|discussions|id-token|issues|packages|pull-requests|security-events|statuses)\s*:\s*["']?write["']?(?:\s+#.*)?\s*$`)
	permissionInlinePattern = regexp.MustCompile(`\b(contents|actions|checks|deployments|discussions|id-token|issues|packages|pull-requests|security-events|statuses)\s*:\s*["']?write["']?\b`)
	writeAllPattern         = regexp.MustCompile(`^\s*permissions\s*:\s*["']?write-all["']?(?:\s+#.*)?\s*$`)
)

type addedPatchLine struct {
	line int
	text string
}

func workflowWriteEvidence(filePath, patch string) []domain.Evidence {
	lines := addedPatchLines(patch)
	evidence := make([]domain.Evidence, 0, len(lines))
	seen := make(map[int]struct{}, len(lines))
	for _, line := range lines {
		permission := permissionName(line.text)
		if permission == "" {
			continue
		}
		if _, exists := seen[line.line]; exists {
			continue
		}
		seen[line.line] = struct{}{}
		evidence = append(evidence, domain.Evidence{
			File:      filePath,
			StartLine: line.line,
			EndLine:   line.line,
			Side:      domain.SideRight,
			Fact:      fmt.Sprintf("新增 Workflow 权限声明: %s", permission),
		})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].StartLine < evidence[j].StartLine })
	return evidence
}

func permissionName(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		return ""
	}
	if comment := strings.Index(line, " #"); comment >= 0 {
		line = strings.TrimSpace(line[:comment])
	}
	if writeAllPattern.MatchString(line) {
		return "write-all"
	}
	if match := permissionLinePattern.FindStringSubmatch(line); match != nil {
		return match[1] + ": write"
	}
	if strings.HasPrefix(line, "permissions:") {
		if match := permissionInlinePattern.FindStringSubmatch(line); match != nil {
			return match[1] + ": write"
		}
	}
	return ""
}

func addedPatchLines(patch string) []addedPatchLine {
	lines := strings.Split(patch, "\n")
	newLine := 0
	inHunk := false
	added := make([]addedPatchLine, 0)
	for _, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if match := hunkHeaderPattern.FindStringSubmatch(line); match != nil {
			fmt.Sscanf(match[1], "%d", &newLine)
			inHunk = true
			continue
		}
		if !inHunk || line == `\ No newline at end of file` || line == "" {
			continue
		}
		switch line[0] {
		case '+':
			added = append(added, addedPatchLine{line: newLine, text: line[1:]})
			newLine++
		case ' ':
			newLine++
		case '-':
		default:
			inHunk = false
		}
	}
	return added
}

func isWorkflowPath(filePath string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(path.Clean(filePath), "./"))
	if !strings.HasPrefix(normalized, ".github/workflows/") {
		return false
	}
	ext := path.Ext(normalized)
	return ext == ".yml" || ext == ".yaml"
}

func firstEvidenceKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.RuleID)
}
