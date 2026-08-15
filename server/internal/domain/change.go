package domain

// FileStatus 描述文件在 Pull Request 中的变更状态。
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
	FileRenamed  FileStatus = "renamed"
	FileCopied   FileStatus = "copied"
	FileUnknown  FileStatus = "unknown"
)

// ChangedFile 是经过规范化的单个文件变更事实。
// Patch 用指针区分「无 patch」（nil）与「空 patch」（空字符串），
// 二进制文件和无 patch 文件为 nil。
type ChangedFile struct {
	OldPath        string     `json:"old_path,omitempty"`
	NewPath        string     `json:"new_path"`
	Status         FileStatus `json:"status"`
	Language       string     `json:"language,omitempty"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	Changes        int        `json:"changes"`
	Patch          *string    `json:"patch,omitempty"`
	IsBinary       bool       `json:"is_binary"`
	PatchTruncated bool       `json:"patch_truncated"`
}

// ChangeSet 是经过统一规范化的 Pull Request 文件变化集合。
type ChangeSet struct {
	Files             []ChangedFile `json:"files"`
	TotalFiles        int           `json:"total_files"`
	TotalAdditions    int           `json:"total_additions"`
	TotalDeletions    int           `json:"total_deletions"`
	BaseSHA           string        `json:"base_sha"`
	HeadSHA           string        `json:"head_sha"`
	Truncated         bool          `json:"truncated"`
	TruncationReasons []string      `json:"truncation_reasons,omitempty"`
}

// ChangeSummary 是报告协议中的变更统计摘要。
type ChangeSummary struct {
	FilesSeen         int      `json:"files_seen"`
	FilesAnalyzed     int      `json:"files_analyzed"`
	Additions         int      `json:"additions"`
	Deletions         int      `json:"deletions"`
	Truncated         bool     `json:"truncated"`
	TruncationReasons []string `json:"truncation_reasons,omitempty"`
}

func validFileStatus(s FileStatus) bool {
	switch s {
	case FileAdded, FileModified, FileDeleted, FileRenamed, FileCopied, FileUnknown:
		return true
	}
	return false
}

// Validate 校验文件字段：路径、状态枚举和非负计数。
func (f ChangedFile) Validate() error {
	v := &validator{}
	v.check(f.NewPath != "", "new_path 不能为空")
	v.check(validFileStatus(f.Status), "status %q 非法", f.Status)
	v.check(f.Additions >= 0, "additions 必须 >= 0，实际为 %d", f.Additions)
	v.check(f.Deletions >= 0, "deletions 必须 >= 0，实际为 %d", f.Deletions)
	v.check(f.Changes >= 0, "changes 必须 >= 0，实际为 %d", f.Changes)
	return v.err()
}

// Validate 校验集合内部一致性：汇总字段与文件列表求和一致，SHA 格式合法。
func (cs ChangeSet) Validate() error {
	v := &validator{}
	v.check(shaPattern.MatchString(cs.BaseSHA), "base_sha %q 不符合 ^[0-9a-fA-F]{7,64}$", cs.BaseSHA)
	v.check(shaPattern.MatchString(cs.HeadSHA), "head_sha %q 不符合 ^[0-9a-fA-F]{7,64}$", cs.HeadSHA)
	v.check(cs.TotalFiles == len(cs.Files), "total_files %d 与文件数 %d 不一致", cs.TotalFiles, len(cs.Files))

	var add, del int
	for i := range cs.Files {
		add += cs.Files[i].Additions
		del += cs.Files[i].Deletions
		if err := cs.Files[i].Validate(); err != nil {
			v.problems = append(v.problems, err.(*ValidationError).Problems...)
		}
	}
	v.check(cs.TotalAdditions == add, "total_additions %d 与求和 %d 不一致", cs.TotalAdditions, add)
	v.check(cs.TotalDeletions == del, "total_deletions %d 与求和 %d 不一致", cs.TotalDeletions, del)
	v.check(!cs.Truncated || len(cs.TruncationReasons) > 0, "truncated=true 时必须提供 truncation_reasons")
	return v.err()
}

// Validate 校验统计字段非负，且截断状态必须有原因。
func (s ChangeSummary) Validate() error {
	v := &validator{}
	v.check(s.FilesSeen >= 0, "files_seen 必须 >= 0，实际为 %d", s.FilesSeen)
	v.check(s.FilesAnalyzed >= 0, "files_analyzed 必须 >= 0，实际为 %d", s.FilesAnalyzed)
	v.check(s.Additions >= 0, "additions 必须 >= 0，实际为 %d", s.Additions)
	v.check(s.Deletions >= 0, "deletions 必须 >= 0，实际为 %d", s.Deletions)
	v.check(!s.Truncated || len(s.TruncationReasons) > 0, "truncated=true 时必须提供 truncation_reasons")
	return v.err()
}
