package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ExportedAPIChangeRuleID 是公共 Go API 删除或签名替换规则的稳定标识。
const ExportedAPIChangeRuleID = "CR-API-001"

// ExportedAPIAnalyzer 检测非 internal、非测试 Go 文件中的导出声明删除或替换。
// 新增而未替换的导出声明不在本规则范围内，以避免把兼容性扩展误报为破坏性变化。
type ExportedAPIAnalyzer struct{}

var _ Analyzer = ExportedAPIAnalyzer{}

var (
	apiHunkHeaderPattern    = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	funcDeclarationPattern  = regexp.MustCompile(`^func\s+(?:\(([^)]*)\)\s*)?([A-Z][A-Za-z0-9_]*)(?:\[[^\]]+\])?\s*\(`)
	typeDeclarationPattern  = regexp.MustCompile(`^type\s+([A-Z][A-Za-z0-9_]*)\b`)
	varDeclarationPattern   = regexp.MustCompile(`^var\s+([A-Z][A-Za-z0-9_]*)\b`)
	constDeclarationPattern = regexp.MustCompile(`^const\s+([A-Z][A-Za-z0-9_]*)\b`)
)

type apiDeclaration struct {
	kind     string
	name     string
	identity string
}

type apiPatchLine struct {
	side domain.EvidenceSide
	line int
	text string
}

// Analyze 返回导出声明删除或签名替换的候选 signal。
func (ExportedAPIAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := changeSet.Validate(); err != nil {
		return nil, fmt.Errorf("invalid change set: %w", err)
	}

	signals := make([]domain.RiskSignal, 0)
	for _, file := range changeSet.Files {
		if !isPublicGoPath(file.NewPath) || file.IsBinary || file.Patch == nil || *file.Patch == "" {
			continue
		}
		changes, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse API patch for %s: %w", file.NewPath, err)
		}
		fileSignals := apiSignalsForFile(file.NewPath, changes)
		signals = append(signals, fileSignals...)
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return apiSignalKey(signals[i]) < apiSignalKey(signals[j])
	})
	return signals, nil
}

func apiSignalsForFile(filePath string, changes []apiPatchLine) []domain.RiskSignal {
	removed := make(map[string][]apiPatchLine)
	added := make(map[string][]apiPatchLine)
	declarations := make(map[string]apiDeclaration)
	for _, change := range changes {
		declaration, ok := exportedDeclaration(change.text)
		if !ok {
			continue
		}
		declarations[declaration.identity] = declaration
		if change.side == domain.SideLeft {
			removed[declaration.identity] = append(removed[declaration.identity], change)
		} else if change.side == domain.SideRight {
			added[declaration.identity] = append(added[declaration.identity], change)
		}
	}

	identities := make([]string, 0, len(removed))
	for identity := range removed {
		identities = append(identities, identity)
	}
	sort.Strings(identities)

	signals := make([]domain.RiskSignal, 0, len(identities))
	for _, identity := range identities {
		left := removed[identity]
		right := added[identity]
		declaration := declarations[identity]
		evidence := make([]domain.Evidence, 0, len(left)+len(right))
		for _, line := range left {
			evidence = append(evidence, apiEvidence(filePath, line, declaration))
		}
		for _, line := range right {
			evidence = append(evidence, apiEvidence(filePath, line, declaration))
		}
		sort.SliceStable(evidence, func(i, j int) bool {
			if evidence[i].StartLine != evidence[j].StartLine {
				return evidence[i].StartLine < evidence[j].StartLine
			}
			return evidence[i].Side < evidence[j].Side
		})
		fact := fmt.Sprintf("删除导出 API 声明: %s %s", declaration.kind, declaration.name)
		if len(right) > 0 {
			fact = fmt.Sprintf("导出 API 签名发生变化: %s %s", declaration.kind, declaration.name)
		}
		signals = append(signals, domain.RiskSignal{
			RuleID:     ExportedAPIChangeRuleID,
			Category:   domain.CategoryAPI,
			Fact:       fact,
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 0.9,
			Weight:     25,
		})
	}
	return signals
}

func apiEvidence(filePath string, line apiPatchLine, declaration apiDeclaration) domain.Evidence {
	fact := fmt.Sprintf("%s %s %s", sideDescription(line.side), declaration.kind, declaration.name)
	return domain.Evidence{
		File:      filePath,
		StartLine: line.line,
		EndLine:   line.line,
		Side:      line.side,
		Fact:      fact,
	}
}

func sideDescription(side domain.EvidenceSide) string {
	if side == domain.SideLeft {
		return "删除"
	}
	return "新增"
}

func exportedDeclaration(text string) (apiDeclaration, bool) {
	text = strings.TrimSpace(text)
	if match := funcDeclarationPattern.FindStringSubmatch(text); match != nil {
		receiver := normalizeReceiver(match[1])
		identity := "func:" + receiver + ":" + match[2]
		return apiDeclaration{kind: "func", name: match[2], identity: identity}, true
	}
	for _, candidate := range []struct {
		kind string
		name string
	}{
		{kind: "type", name: firstCapture(typeDeclarationPattern, text)},
		{kind: "var", name: firstCapture(varDeclarationPattern, text)},
		{kind: "const", name: firstCapture(constDeclarationPattern, text)},
	} {
		if candidate.name != "" {
			return apiDeclaration{
				kind:     candidate.kind,
				name:     candidate.name,
				identity: candidate.kind + ":" + candidate.name,
			}, true
		}
	}
	return apiDeclaration{}, false
}

func firstCapture(pattern *regexp.Regexp, text string) string {
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func normalizeReceiver(receiver string) string {
	if receiver == "" {
		return ""
	}
	parts := strings.Fields(receiver)
	if len(parts) == 0 {
		return ""
	}
	typeName := parts[len(parts)-1]
	typeName = strings.TrimPrefix(typeName, "*")
	return typeName
}

func apiPatchLines(patch string) ([]apiPatchLine, error) {
	lines := strings.Split(patch, "\n")
	for len(lines) > 0 && strings.TrimSuffix(lines[len(lines)-1], "\r") == "" {
		lines = lines[:len(lines)-1]
	}
	oldLine, newLine := 0, 0
	inHunk := false
	changes := make([]apiPatchLine, 0)
	for index, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if match := apiHunkHeaderPattern.FindStringSubmatch(line); match != nil {
			var err error
			oldLine, err = strconv.Atoi(match[1])
			if err != nil {
				return nil, fmt.Errorf("invalid hunk old line at %d", index+1)
			}
			newLine, err = strconv.Atoi(match[3])
			if err != nil {
				return nil, fmt.Errorf("invalid hunk new line at %d", index+1)
			}
			inHunk = true
			continue
		}
		if !inHunk || line == `\ No newline at end of file` {
			continue
		}
		if line == "" {
			return nil, fmt.Errorf("missing hunk prefix at line %d", index+1)
		}
		switch line[0] {
		case '+':
			changes = append(changes, apiPatchLine{side: domain.SideRight, line: newLine, text: line[1:]})
			newLine++
		case '-':
			changes = append(changes, apiPatchLine{side: domain.SideLeft, line: oldLine, text: line[1:]})
			oldLine++
		case ' ':
			oldLine++
			newLine++
		default:
			return nil, fmt.Errorf("invalid hunk prefix at line %d", index+1)
		}
	}
	return changes, nil
}

func isPublicGoPath(filePath string) bool {
	normalized := strings.ToLower(strings.TrimPrefix(path.Clean(filePath), "./"))
	if normalized == ".." || strings.HasPrefix(normalized, "../") || strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\\") {
		return false
	}
	if !strings.HasSuffix(normalized, ".go") || strings.HasSuffix(normalized, "_test.go") {
		return false
	}
	return !strings.HasPrefix(normalized, "internal/") && !strings.HasPrefix(normalized, "vendor/")
}

func apiSignalKey(signal domain.RiskSignal) string {
	if len(signal.Evidence) == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%08d:%s", signal.Evidence[0].File, signal.Evidence[0].StartLine, signal.Fact)
}
