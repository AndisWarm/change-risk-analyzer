package domain

import "testing"

func stringPtr(s string) *string { return &s }

func validChangedFile() ChangedFile {
	return ChangedFile{
		OldPath:   "internal/old.go",
		NewPath:   "internal/new.go",
		Status:    FileRenamed,
		Language:  "go",
		Additions: 3,
		Deletions: 1,
		Changes:   4,
		Patch:     stringPtr("@@ -1,1 +1,3 @@"),
	}
}

func TestChangedFileValidate(t *testing.T) {
	if err := validChangedFile().Validate(); err != nil {
		t.Fatalf("valid changed file rejected: %v", err)
	}

	tests := []struct {
		name string
		edit func(*ChangedFile)
	}{
		{name: "missing path", edit: func(f *ChangedFile) { f.NewPath = "" }},
		{name: "invalid status", edit: func(f *ChangedFile) { f.Status = FileStatus("renamed-but-not-recorded") }},
		{name: "negative additions", edit: func(f *ChangedFile) { f.Additions = -1 }},
		{name: "negative deletions", edit: func(f *ChangedFile) { f.Deletions = -1 }},
		{name: "negative changes", edit: func(f *ChangedFile) { f.Changes = -1 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := validChangedFile()
			tc.edit(&file)
			if err := file.Validate(); err == nil {
				t.Fatalf("invalid file was accepted: %+v", file)
			}
		})
	}
}

func TestChangeSetValidate(t *testing.T) {
	file := validChangedFile()
	changeSet := ChangeSet{
		Files:          []ChangedFile{file},
		TotalFiles:     1,
		TotalAdditions: 3,
		TotalDeletions: 1,
		BaseSHA:        validReviewRequest().BaseSHA,
		HeadSHA:        validReviewRequest().HeadSHA,
	}
	if err := changeSet.Validate(); err != nil {
		t.Fatalf("valid change set rejected: %v", err)
	}

	t.Run("rejects inconsistent totals", func(t *testing.T) {
		invalid := changeSet
		invalid.TotalAdditions = 2
		if err := invalid.Validate(); err == nil {
			t.Fatal("inconsistent additions were accepted")
		}
	})

	t.Run("requires truncation reason", func(t *testing.T) {
		invalid := changeSet
		invalid.Truncated = true
		if err := invalid.Validate(); err == nil {
			t.Fatal("truncated change set without reason was accepted")
		}
	})
}

func TestChangeSummaryValidate(t *testing.T) {
	valid := ChangeSummary{FilesSeen: 2, FilesAnalyzed: 1, Additions: 4, Deletions: 2}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}

	invalid := valid
	invalid.FilesSeen = -1
	if err := invalid.Validate(); err == nil {
		t.Fatal("negative files_seen was accepted")
	}
}
