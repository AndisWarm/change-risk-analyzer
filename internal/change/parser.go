// Package change 负责把 unified diff 规范化为领域层 ChangeSet。
// 该包只解析文本输入，不读取文件系统、不调用网络，也不执行 diff 中的内容。
package change

import (
	"change-risk-analyzer/internal/domain"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// DefaultMaxFilePatchBytes 是单文件 patch 的默认保留上限。
	DefaultMaxFilePatchBytes = 128 * 1024
	// DefaultMaxTotalPatchBytes 是全部文件 patch 的默认保留上限。
	DefaultMaxTotalPatchBytes = 1024 * 1024
)

// Options 控制一次 diff 解析的身份和资源边界。
// BaseSHA 和 HeadSHA 会写入 ChangeSet，并由领域校验保证格式合法。
type Options struct {
	BaseSHA            string
	HeadSHA            string
	MaxFilePatchBytes  int
	MaxTotalPatchBytes int
}

func (o Options) withDefaults() (Options, error) {
	if o.MaxFilePatchBytes == 0 {
		o.MaxFilePatchBytes = DefaultMaxFilePatchBytes
	}
	if o.MaxTotalPatchBytes == 0 {
		o.MaxTotalPatchBytes = DefaultMaxTotalPatchBytes
	}
	if o.MaxFilePatchBytes < 0 {
		return Options{}, fmt.Errorf("max file patch bytes must be non-negative")
	}
	if o.MaxTotalPatchBytes < 0 {
		return Options{}, fmt.Errorf("max total patch bytes must be non-negative")
	}
	return o, nil
}

// AddedLine 是一个可复核的新增右侧行号。
// File 使用规范化后的仓库相对路径，Line 从 1 开始。
type AddedLine struct {
	File string
	Line int
}

// ParseResult 是 unified diff 的确定性解析结果。
// AddedLines 不进入 RiskReport schema，仅供后续证据校验和行号映射使用。
type ParseResult struct {
	ChangeSet  domain.ChangeSet
	AddedLines []AddedLine
}

// ParseUnifiedDiff 解析一个或多个 git unified diff 文件段。
// 空输入表示没有文件变化；非空输入必须至少包含一个 "diff --git" 文件头。
func ParseUnifiedDiff(input string, options Options) (ParseResult, error) {
	var result ParseResult
	var err error
	options, err = options.withDefaults()
	if err != nil {
		return result, err
	}
	if strings.IndexByte(input, 0) >= 0 {
		return result, &ParseError{Reason: "diff contains NUL byte"}
	}

	lines := strings.Split(input, "\n")
	sections := splitSections(lines)
	if len(sections) == 0 {
		if strings.TrimSpace(input) != "" {
			return result, &ParseError{Reason: "missing diff --git file header"}
		}
		result.ChangeSet = domain.ChangeSet{
			Files:   []domain.ChangedFile{},
			BaseSHA: options.BaseSHA,
			HeadSHA: options.HeadSHA,
		}
		if err := result.ChangeSet.Validate(); err != nil {
			return ParseResult{}, err
		}
		return result, nil
	}

	parsed := make([]parsedFile, 0, len(sections))
	seenPaths := make(map[string]struct{}, len(sections))
	for _, section := range sections {
		file, err := parseSection(section)
		if err != nil {
			return ParseResult{}, err
		}
		if _, exists := seenPaths[file.changed.NewPath]; exists {
			return ParseResult{}, &ParseError{File: file.changed.NewPath, Reason: "duplicate file path"}
		}
		seenPaths[file.changed.NewPath] = struct{}{}
		parsed = append(parsed, file)
	}

	var storedTotal int
	truncationReasons := make(map[string]struct{})
	for i := range parsed {
		if parsed[i].patch == "" || parsed[i].changed.IsBinary || !parsed[i].hasHunk {
			continue
		}

		stored := parsed[i].patch
		truncated := false
		if options.MaxFilePatchBytes > 0 && len(stored) > options.MaxFilePatchBytes {
			stored = stored[:options.MaxFilePatchBytes]
			truncated = true
			truncationReasons["max_file_patch_bytes"] = struct{}{}
		}
		if options.MaxTotalPatchBytes > 0 {
			remaining := options.MaxTotalPatchBytes - storedTotal
			if remaining < 0 {
				remaining = 0
			}
			if len(stored) > remaining {
				stored = stored[:remaining]
				truncated = true
				truncationReasons["max_total_patch_bytes"] = struct{}{}
			}
		}
		storedTotal += len(stored)
		parsed[i].changed.Patch = stringPointer(stored)
		parsed[i].changed.PatchTruncated = truncated
	}

	result.ChangeSet = domain.ChangeSet{
		Files:             make([]domain.ChangedFile, 0, len(parsed)),
		TotalFiles:        len(parsed),
		BaseSHA:           options.BaseSHA,
		HeadSHA:           options.HeadSHA,
		Truncated:         len(truncationReasons) > 0,
		TruncationReasons: sortedKeys(truncationReasons),
	}
	for _, file := range parsed {
		result.ChangeSet.Files = append(result.ChangeSet.Files, file.changed)
		result.ChangeSet.TotalAdditions += file.changed.Additions
		result.ChangeSet.TotalDeletions += file.changed.Deletions
		result.AddedLines = append(result.AddedLines, file.addedLines...)
	}
	sort.Slice(result.ChangeSet.Files, func(i, j int) bool {
		if result.ChangeSet.Files[i].NewPath != result.ChangeSet.Files[j].NewPath {
			return result.ChangeSet.Files[i].NewPath < result.ChangeSet.Files[j].NewPath
		}
		return result.ChangeSet.Files[i].OldPath < result.ChangeSet.Files[j].OldPath
	})
	sort.Slice(result.AddedLines, func(i, j int) bool {
		if result.AddedLines[i].File != result.AddedLines[j].File {
			return result.AddedLines[i].File < result.AddedLines[j].File
		}
		return result.AddedLines[i].Line < result.AddedLines[j].Line
	})
	if err := result.ChangeSet.Validate(); err != nil {
		return ParseResult{}, err
	}
	return result, nil
}

type diffSection struct {
	lines []string
}

func splitSections(lines []string) []diffSection {
	starts := make([]int, 0)
	for i, raw := range lines {
		line := strings.TrimSuffix(raw, "\r")
		if strings.HasPrefix(line, "diff --git ") {
			starts = append(starts, i)
		}
	}
	sections := make([]diffSection, 0, len(starts))
	for i, start := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		sections = append(sections, diffSection{lines: lines[start:end]})
	}
	return sections
}

type parsedFile struct {
	changed    domain.ChangedFile
	patch      string
	hasHunk    bool
	addedLines []AddedLine
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

type hunkState struct {
	oldCount int
	newCount int
	oldSeen  int
	newSeen  int
	newLine  int
}

func parseSection(section diffSection) (parsedFile, error) {
	if len(section.lines) == 0 {
		return parsedFile{}, &ParseError{Reason: "empty diff section"}
	}
	for len(section.lines) > 1 && strings.TrimSuffix(section.lines[len(section.lines)-1], "\r") == "" {
		section.lines = section.lines[:len(section.lines)-1]
	}

	var oldPath, newPath string
	var oldSeen, newSeen bool
	var oldIsNull, newIsNull bool
	var renameFrom, renameTo, copyFrom, copyTo string
	var addedHint, deletedHint, renamedHint, copiedHint, binary bool
	var patchStart = -1
	var currentHunk *hunkState
	var additions, deletions int
	var addedLines []AddedLine

	for index, raw := range section.lines[1:] {
		lineNumber := index + 2
		line := strings.TrimSuffix(raw, "\r")
		if currentHunk == nil {
			switch {
			case strings.HasPrefix(line, "new file mode "):
				addedHint = true
			case strings.HasPrefix(line, "deleted file mode "):
				deletedHint = true
			case strings.HasPrefix(line, "rename from "):
				renamedHint = true
				var err error
				renameFrom, err = normalizePath(strings.TrimPrefix(line, "rename from "), "")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
			case strings.HasPrefix(line, "rename to "):
				var err error
				renameTo, err = normalizePath(strings.TrimPrefix(line, "rename to "), "")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
			case strings.HasPrefix(line, "copy from "):
				copiedHint = true
				var err error
				copyFrom, err = normalizePath(strings.TrimPrefix(line, "copy from "), "")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
			case strings.HasPrefix(line, "copy to "):
				var err error
				copyTo, err = normalizePath(strings.TrimPrefix(line, "copy to "), "")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
			case strings.HasPrefix(line, "Binary files "), strings.HasPrefix(line, "GIT binary patch"):
				binary = true
			case strings.HasPrefix(line, "--- "):
				var err error
				rawPath := diffPathValue(strings.TrimPrefix(line, "--- "))
				oldPath, err = normalizePath(rawPath, "a/")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
				oldIsNull = oldPath == ""
				oldSeen = true
				patchStart = firstPatchLine(patchStart, index+1)
			case strings.HasPrefix(line, "+++ "):
				var err error
				rawPath := diffPathValue(strings.TrimPrefix(line, "+++ "))
				newPath, err = normalizePath(rawPath, "b/")
				if err != nil {
					return parsedFile{}, &ParseError{Line: lineNumber, Reason: err.Error()}
				}
				newIsNull = newPath == ""
				newSeen = true
				patchStart = firstPatchLine(patchStart, index+1)
			}
		}

		if strings.HasPrefix(line, "@@ ") {
			if currentHunk != nil {
				if err := finishHunk(currentHunk, lineNumber); err != nil {
					return parsedFile{}, err
				}
			}
			match := hunkHeaderPattern.FindStringSubmatch(line)
			if match == nil {
				return parsedFile{}, &ParseError{Line: lineNumber, Reason: "invalid hunk header"}
			}
			oldStart, err := parseHunkNumber(match[1], match[2])
			if err != nil {
				return parsedFile{}, &ParseError{Line: lineNumber, Reason: "invalid old hunk range"}
			}
			newStart, err := parseHunkNumber(match[3], match[4])
			if err != nil {
				return parsedFile{}, &ParseError{Line: lineNumber, Reason: "invalid new hunk range"}
			}
			currentHunk = &hunkState{
				oldCount: oldStart.count,
				newCount: newStart.count,
				newLine:  newStart.start,
			}
			patchStart = firstPatchLine(patchStart, index+1)
			continue
		}

		if currentHunk == nil {
			continue
		}
		if line == `\ No newline at end of file` {
			continue
		}
		if line == "" {
			return parsedFile{}, &ParseError{Line: lineNumber, Reason: "hunk line is missing its prefix"}
		}
		switch line[0] {
		case ' ':
			currentHunk.oldSeen++
			currentHunk.newSeen++
			currentHunk.newLine++
		case '+':
			currentHunk.newSeen++
			addedLines = append(addedLines, AddedLine{Line: currentHunk.newLine})
			currentHunk.newLine++
			additions++
		case '-':
			currentHunk.oldSeen++
			deletions++
		default:
			return parsedFile{}, &ParseError{Line: lineNumber, Reason: "invalid hunk line prefix"}
		}
	}
	if currentHunk != nil {
		if err := finishHunk(currentHunk, len(section.lines)+1); err != nil {
			return parsedFile{}, err
		}
	}

	if !oldSeen && !newSeen {
		oldPath, newPath = parseGitHeaderPaths(section.lines[0])
		if oldPath != "" {
			oldSeen = true
		}
		if newPath != "" {
			newSeen = true
		}
	}
	if renamedHint {
		if renameFrom != "" {
			oldPath = renameFrom
			oldSeen = true
		}
		if renameTo != "" {
			newPath = renameTo
			newSeen = true
		}
	}
	if copiedHint {
		if copyFrom != "" {
			oldPath = copyFrom
			oldSeen = true
		}
		if copyTo != "" {
			newPath = copyTo
			newSeen = true
		}
	}
	if oldPath == "" && newPath == "" {
		return parsedFile{}, &ParseError{Reason: "diff section has no file path"}
	}
	if newPath == "" {
		newPath = oldPath
	}

	status := domain.FileModified
	switch {
	case addedHint || oldIsNull || oldPath == "":
		status = domain.FileAdded
	case deletedHint || newIsNull:
		status = domain.FileDeleted
	case renamedHint:
		status = domain.FileRenamed
	case copiedHint:
		status = domain.FileCopied
	case oldPath != newPath && oldPath != "":
		status = domain.FileRenamed
	}
	if deletedHint {
		status = domain.FileDeleted
	}
	if renamedHint {
		status = domain.FileRenamed
	}
	if copiedHint {
		status = domain.FileCopied
	}

	patch := ""
	if patchStart >= 0 {
		patch = strings.Join(normalizeSectionLines(section.lines[patchStart:]), "\n")
	}
	for i := range addedLines {
		addedLines[i].File = newPath
	}
	return parsedFile{
		changed: domain.ChangedFile{
			OldPath:   oldPath,
			NewPath:   newPath,
			Status:    status,
			Language:  languageForPath(newPath),
			Additions: additions,
			Deletions: deletions,
			Changes:   additions + deletions,
			Patch:     nil,
			IsBinary:  binary,
		},
		patch:      patch,
		hasHunk:    patchStart >= 0,
		addedLines: addedLines,
	}, nil
}

type hunkRange struct {
	start int
	count int
}

func parseHunkNumber(startText, countText string) (hunkRange, error) {
	start, err := strconv.Atoi(startText)
	if err != nil || start < 0 {
		return hunkRange{}, fmt.Errorf("invalid hunk start")
	}
	count := 1
	if countText != "" {
		count, err = strconv.Atoi(countText)
		if err != nil || count < 0 {
			return hunkRange{}, fmt.Errorf("invalid hunk count")
		}
	}
	return hunkRange{start: start, count: count}, nil
}

func finishHunk(hunk *hunkState, lineNumber int) error {
	if hunk.oldSeen != hunk.oldCount || hunk.newSeen != hunk.newCount {
		return &ParseError{
			Line:   lineNumber,
			Reason: fmt.Sprintf("hunk line count mismatch: old %d/%d, new %d/%d", hunk.oldSeen, hunk.oldCount, hunk.newSeen, hunk.newCount),
		}
	}
	return nil
}

func firstPatchLine(current, candidate int) int {
	if current >= 0 {
		return current
	}
	return candidate
}

func normalizeSectionLines(lines []string) []string {
	normalized := make([]string, len(lines))
	for i, line := range lines {
		normalized[i] = strings.TrimSuffix(line, "\r")
	}
	return normalized
}

func diffPathValue(value string) string {
	if tab := strings.IndexByte(value, '\t'); tab >= 0 {
		return value[:tab]
	}
	return value
}

func normalizePath(raw, prefix string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty path")
	}
	if raw == "/dev/null" {
		return "", nil
	}
	if strings.HasPrefix(raw, "\"") {
		unquoted, err := strconv.Unquote(raw)
		if err != nil {
			return "", fmt.Errorf("invalid quoted path")
		}
		raw = unquoted
	}
	if prefix != "" && strings.HasPrefix(raw, prefix) {
		raw = strings.TrimPrefix(raw, prefix)
	}
	if strings.IndexByte(raw, 0) >= 0 || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || (len(raw) >= 2 && raw[1] == ':') {
		return "", fmt.Errorf("unsafe absolute path")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("unsafe traversal path")
	}
	return cleaned, nil
}

func parseGitHeaderPaths(header string) (string, string) {
	rest := strings.TrimPrefix(strings.TrimSuffix(header, "\r"), "diff --git ")
	tokens := scanPathTokens(rest)
	if len(tokens) < 2 {
		return "", ""
	}
	oldPath, _ := normalizePath(tokens[0], "a/")
	newPath, _ := normalizePath(tokens[1], "b/")
	return oldPath, newPath
}

func scanPathTokens(value string) []string {
	var tokens []string
	for len(value) > 0 {
		value = strings.TrimLeft(value, " \t")
		if value == "" {
			break
		}
		if value[0] == '"' {
			end := 1
			escaped := false
			for end < len(value) {
				if !escaped && value[end] == '"' {
					end++
					break
				}
				if value[end] == '\\' && !escaped {
					escaped = true
				} else {
					escaped = false
				}
				end++
			}
			if end > len(value) {
				break
			}
			tokens = append(tokens, value[:end])
			value = value[end:]
			continue
		}
		end := strings.IndexAny(value, " \t")
		if end < 0 {
			tokens = append(tokens, value)
			break
		}
		tokens = append(tokens, value[:end])
		value = value[end:]
	}
	return tokens
}

func languageForPath(filePath string) string {
	lower := strings.ToLower(filePath)
	if path.Base(lower) == "dockerfile" {
		return "dockerfile"
	}
	ext := path.Ext(lower)
	known := map[string]string{
		".c": "c", ".cc": "cpp", ".cpp": "cpp", ".cs": "csharp", ".go": "go",
		".h": "c", ".hpp": "cpp", ".java": "java", ".js": "javascript", ".jsx": "javascript",
		".json": "json", ".md": "markdown", ".php": "php", ".py": "python", ".rb": "ruby",
		".rs": "rust", ".sh": "shell", ".sql": "sql", ".swift": "swift", ".ts": "typescript",
		".tsx": "typescript", ".xml": "xml", ".yaml": "yaml", ".yml": "yaml",
	}
	return known[ext]
}

func stringPointer(value string) *string { return &value }

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ParseError 描述不可信 diff 输入的结构错误，不包含原始 patch 内容。
type ParseError struct {
	File   string
	Line   int
	Reason string
}

func (e *ParseError) Error() string {
	if e.File != "" && e.Line > 0 {
		return fmt.Sprintf("diff parse error in %s at line %d: %s", e.File, e.Line, e.Reason)
	}
	if e.File != "" {
		return fmt.Sprintf("diff parse error in %s: %s", e.File, e.Reason)
	}
	return "diff parse error: " + e.Reason
}
