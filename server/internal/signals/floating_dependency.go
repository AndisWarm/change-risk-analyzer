// floating_dependency.go 实现 CR-SC-001：新增的 Action、镜像和 Go 依赖
// 浮动版本引用线索。
//
// 这是有限范围的词法候选规则：只报告「版本引用会自动漂移」的事实，
// 不证明该依赖必然被劫持，也不决定最终严重度或门禁状态。
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

// FloatingReferenceRuleID 是浮动依赖引用规则的稳定标识。
const FloatingReferenceRuleID = "CR-SC-001"

// FloatingReferenceAnalyzer 检测新增 patch 行中的浮动版本引用：
// GitHub Action 的分支或大版本标签、容器的 latest/无标签镜像，
// 以及 go get/go install 的 latest 或分支引用。
type FloatingReferenceAnalyzer struct{}

var _ Analyzer = FloatingReferenceAnalyzer{}

var (
	actionUsesPattern = regexp.MustCompile(`^\s*(?:-\s+)*uses:\s*(\S+)`)
	imageKeyPattern   = regexp.MustCompile(`^\s*(?:-\s+)*image:\s*(\S+)`)
	dockerFromPattern = regexp.MustCompile(`(?i)^\s*FROM\s+(?:--platform=\S+\s+)?(\S+)`)
	goModuleScan      = regexp.MustCompile(`\bgo\s+(?:get|install)\b\s*(.*)$`)

	floatingBranchRefPattern = regexp.MustCompile(`(?i)^(master|main|latest|develop|dev)$`)
	majorTagRefPattern       = regexp.MustCompile(`^v\d+$`)
	minorTagRefPattern       = regexp.MustCompile(`^v\d+\.\d+$`)
	fullCommitSHAPattern     = regexp.MustCompile(`(?i)^([0-9a-f]{40}|[0-9a-f]{64})$`)
	exactVersionRefPattern   = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	imageDigestRefPattern    = regexp.MustCompile(`(?i)(@[0-9a-f]{64}|@sha256:[0-9a-f]{64}|@sha512:[0-9a-f]{128})$`)
)

// Analyze 对新增行输出按文件合并的浮动引用 signal。
func (FloatingReferenceAnalyzer) Analyze(ctx context.Context, changeSet domain.ChangeSet) ([]domain.RiskSignal, error) {
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
		yamlContext := isYAMLConfigPath(file.NewPath)
		dockerContext := isDockerfilePath(file.NewPath)
		scriptContext := isScriptPath(file.NewPath)
		if !yamlContext && !dockerContext && !scriptContext {
			continue
		}
		lines, err := apiPatchLines(*file.Patch)
		if err != nil {
			return nil, fmt.Errorf("parse floating reference patch for %s: %w", file.NewPath, err)
		}
		evidence := floatingEvidenceForFile(file.NewPath, lines, yamlContext, dockerContext)
		if len(evidence) == 0 {
			continue
		}
		signals = append(signals, domain.RiskSignal{
			RuleID:     FloatingReferenceRuleID,
			Category:   domain.CategorySupplyChain,
			Fact:       "新增了未固定版本的 Action、镜像或 Go 依赖引用",
			Evidence:   evidence,
			Source:     domain.SourceDeterministic,
			Confidence: 0.8,
			Weight:     20,
		})
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return firstEvidenceKey(signals[i]) < firstEvidenceKey(signals[j])
	})
	return signals, nil
}

func floatingEvidenceForFile(filePath string, lines []apiPatchLine, yamlContext, dockerContext bool) []domain.Evidence {
	evidence := make([]domain.Evidence, 0)
	seen := make(map[int]struct{})
	for _, patchLine := range lines {
		if patchLine.side != domain.SideRight || patchLine.line < 1 {
			continue
		}
		text := stripInlineComment(patchLine.text)
		if text == "" {
			continue
		}
		reason, kind := "", ""
		switch {
		case yamlContext:
			reason, kind = floatingReferenceInYAML(text)
		case dockerContext:
			reason, kind = floatingReferenceInDockerfile(text)
		default:
			if reason = floatingGoModuleReason(text); reason != "" {
				kind = "Go 依赖安装命令"
			}
		}
		if reason == "" {
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
			Fact:      fmt.Sprintf("新增%s: %s", kind, reason),
		})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].StartLine < evidence[j].StartLine })
	return evidence
}

// floatingReferenceInYAML 处理 YAML 文件（Workflow、compose 等）中的三类形态。
func floatingReferenceInYAML(text string) (string, string) {
	if match := actionUsesPattern.FindStringSubmatch(text); match != nil {
		if reason := floatingActionReason(match[1]); reason != "" {
			return reason, "GitHub Action 引用"
		}
		return "", ""
	}
	if match := imageKeyPattern.FindStringSubmatch(text); match != nil {
		if reason := floatingImageReason(strings.Trim(match[1], `"'`), true); reason != "" {
			return reason, "容器镜像引用"
		}
		return "", ""
	}
	if reason := floatingGoModuleReason(text); reason != "" {
		return reason, "Go 依赖安装命令"
	}
	return "", ""
}

// floatingReferenceInDockerfile 处理 Dockerfile 的 FROM 行与行内 Go 安装命令。
func floatingReferenceInDockerfile(text string) (string, string) {
	if match := dockerFromPattern.FindStringSubmatch(text); match != nil {
		if reason := floatingImageReason(match[1], false); reason != "" {
			return reason, "容器镜像引用"
		}
		return "", ""
	}
	if reason := floatingGoModuleReason(text); reason != "" {
		return reason, "Go 依赖安装命令"
	}
	return "", ""
}

// floatingActionReason 判定 Action 引用是否为浮动版本。
// 固定判定：完整 40/64 位 commit SHA、三段式精确版本（如 v1.2.3）。
// 浮动判定：master/main/latest 等分支词，以及 v1、v1.2 这类可移动标签。
// 其余形态不判断，控制误报。
func floatingActionReason(usesValue string) string {
	at := strings.LastIndex(usesValue, "@")
	if at < 0 || at == len(usesValue)-1 {
		return "" // 本地路径 action 或缺失 ref，不在本规则范围
	}
	ref := usesValue[at+1:]
	name := usesValue[:at]
	switch {
	case fullCommitSHAPattern.MatchString(ref), exactVersionRefPattern.MatchString(ref):
		return ""
	case floatingBranchRefPattern.MatchString(ref):
		return fmt.Sprintf("%s 使用分支/浮动标签 @%s", name, ref)
	case majorTagRefPattern.MatchString(ref), minorTagRefPattern.MatchString(ref):
		return fmt.Sprintf("%s 使用可移动大版本标签 @%s", name, ref)
	default:
		return ""
	}
}

// floatingImageReason 判定镜像引用是否为浮动版本。
// digest 摘要与显式非 latest 标签视为固定；无 tag 视为浮动，
// 但 Dockerfile 中无 tag 的裸单词名可能是构建阶段引用，跳过以避免误报。
func floatingImageReason(ref string, yamlContext bool) string {
	if ref == "" || strings.EqualFold(ref, "scratch") {
		return ""
	}
	if imageDigestRefPattern.MatchString(ref) {
		return ""
	}
	name, tag := splitImageTag(ref)
	if strings.EqualFold(tag, "latest") {
		return fmt.Sprintf("镜像使用 latest 标签: %s", ref)
	}
	if tag != "" {
		return "" // 显式固定 tag
	}
	if yamlContext || strings.ContainsAny(name, "/.") {
		return fmt.Sprintf("镜像未指定标签（隐式 latest）: %s", ref)
	}
	return ""
}

func splitImageTag(ref string) (name, tag string) {
	slash := strings.LastIndex(ref, "/")
	colon := strings.LastIndex(ref, ":")
	if colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}

// floatingGoModuleReason 检查行内 go get/go install 命令是否引用了
// @latest 或 @master/@main 这类会自动漂移的版本。
func floatingGoModuleReason(text string) string {
	match := goModuleScan.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	for _, token := range strings.FieldsFunc(match[1], func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\\' || r == '(' || r == ')' || r == ';'
	}) {
		token = strings.Trim(token, `"'`)
		at := strings.LastIndex(token, "@")
		if at <= 0 || at == len(token)-1 {
			continue
		}
		ref := token[at+1:]
		if strings.EqualFold(ref, "latest") || floatingBranchRefPattern.MatchString(ref) {
			return fmt.Sprintf("%s 引用了未固定版本 @%s", token[:at], ref)
		}
	}
	return ""
}

// stripInlineComment 去掉行内注释；纯注释行返回空字符串。
// 与既有规则一致采用简单的 " #" 切分方式，不解析引号嵌套。
func stripInlineComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	if comment := strings.Index(line, " #"); comment >= 0 {
		line = line[:comment]
	}
	return strings.TrimSpace(line)
}

// isYAMLConfigPath 匹配所有 .yml/.yaml 文件（含 Workflow 与 compose）。
func isYAMLConfigPath(filePath string) bool {
	normalized, ok := normalizeRelativePath(filePath)
	if !ok {
		return false
	}
	ext := path.Ext(normalized)
	return ext == ".yml" || ext == ".yaml"
}

// isDockerfilePath 匹配名为 Dockerfile 或 Dockerfile.* 的文件。
func isDockerfilePath(filePath string) bool {
	normalized, ok := normalizeRelativePath(filePath)
	if !ok {
		return false
	}
	base := path.Base(normalized)
	return base == "dockerfile" || strings.HasPrefix(base, "dockerfile.")
}

// isScriptPath 匹配 shell 脚本与 Makefile 类文件。
func isScriptPath(filePath string) bool {
	normalized, ok := normalizeRelativePath(filePath)
	if !ok {
		return false
	}
	base := path.Base(normalized)
	switch {
	case strings.HasSuffix(normalized, ".sh"):
		return true
	case base == "makefile" || strings.HasSuffix(base, ".mk"):
		return true
	default:
		return false
	}
}

// normalizeRelativePath 清理路径并拒绝绝对路径与穿越路径。
func normalizeRelativePath(filePath string) (string, bool) {
	normalized := strings.ToLower(strings.TrimPrefix(path.Clean(filePath), "./"))
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") ||
		strings.HasPrefix(normalized, "/") || strings.Contains(normalized, "\\") {
		return "", false
	}
	return normalized, true
}
