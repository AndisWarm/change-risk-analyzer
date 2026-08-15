package change

import (
	"change-risk-analyzer/internal/domain"
	"reflect"
	"strings"
	"testing"
)

const (
	testBaseSHA = "0123456789abcdef0123456789abcdef01234567"
	testHeadSHA = "abcdef0123456789abcdef0123456789abcdef01"
)

func testOptions() Options {
	return Options{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA}
}

func TestParseUnifiedDiffEmpty(t *testing.T) {
	result, err := ParseUnifiedDiff("", testOptions())
	if err != nil {
		t.Fatalf("empty diff rejected: %v", err)
	}
	if len(result.ChangeSet.Files) != 0 || result.ChangeSet.TotalFiles != 0 {
		t.Fatalf("unexpected empty result: %+v", result.ChangeSet)
	}
	if err := result.ChangeSet.Validate(); err != nil {
		t.Fatalf("empty result violates domain invariants: %v", err)
	}
}

func TestParseUnifiedDiffModifiedAndLineMapping(t *testing.T) {
	patch := `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,4 @@
 package main
+const version = "test"
 
 func main() {}
`
	result, err := ParseUnifiedDiff(patch, testOptions())
	if err != nil {
		t.Fatalf("modified diff rejected: %v", err)
	}
	if err := result.ChangeSet.Validate(); err != nil {
		t.Fatalf("parsed change set invalid: %v", err)
	}
	if len(result.ChangeSet.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(result.ChangeSet.Files))
	}
	file := result.ChangeSet.Files[0]
	if file.Status != domain.FileModified || file.OldPath != "main.go" || file.NewPath != "main.go" {
		t.Fatalf("unexpected file identity: %+v", file)
	}
	if file.Language != "go" || file.Additions != 1 || file.Deletions != 0 || file.Changes != 1 {
		t.Fatalf("unexpected file facts: %+v", file)
	}
	if file.Patch == nil || !strings.HasPrefix(*file.Patch, "--- a/main.go\n+++ b/main.go") {
		t.Fatalf("normalized patch missing file headers: %v", file.Patch)
	}
	wantLines := []AddedLine{{File: "main.go", Line: 2}}
	if !reflect.DeepEqual(result.AddedLines, wantLines) {
		t.Fatalf("added lines = %+v, want %+v", result.AddedLines, wantLines)
	}
}

func TestParseUnifiedDiffAddedDeletedAndNoNewlineMarker(t *testing.T) {
	patch := `diff --git a/new.txt b/new.txt
new file mode 100644
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+one
+two
\ No newline at end of file
diff --git a/old.txt b/old.txt
deleted file mode 100644
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-old one
-old two
diff --git a/no-mode.txt b/no-mode.txt
--- a/no-mode.txt
+++ /dev/null
@@ -1 +0,0 @@
-old
`
	result, err := ParseUnifiedDiff(patch, testOptions())
	if err != nil {
		t.Fatalf("added/deleted diff rejected: %v", err)
	}
	if result.ChangeSet.TotalAdditions != 2 || result.ChangeSet.TotalDeletions != 3 {
		t.Fatalf("unexpected totals: %+v", result.ChangeSet)
	}
	files := result.ChangeSet.Files
	byPath := make(map[string]domain.ChangedFile, len(files))
	for _, file := range files {
		byPath[file.NewPath] = file
	}
	if file := byPath["new.txt"]; file.Status != domain.FileAdded || file.OldPath != "" {
		t.Fatalf("unexpected added file: %+v", file)
	}
	if file := byPath["old.txt"]; file.Status != domain.FileDeleted || file.OldPath != "old.txt" {
		t.Fatalf("unexpected deleted file: %+v", file)
	}
	if file := byPath["no-mode.txt"]; file.Status != domain.FileDeleted || file.OldPath != "no-mode.txt" {
		t.Fatalf("unexpected mode-less deleted file: %+v", file)
	}
	if !reflect.DeepEqual(result.AddedLines, []AddedLine{{File: "new.txt", Line: 1}, {File: "new.txt", Line: 2}}) {
		t.Fatalf("unexpected added line map: %+v", result.AddedLines)
	}
}

func TestParseUnifiedDiffRenameCopyBinaryAndNoPatch(t *testing.T) {
	patch := `diff --git a/old name.md b/new name.md
similarity index 100%
rename from old name.md
rename to new name.md
diff --git a/source.txt b/copy.txt
similarity index 100%
copy from source.txt
copy to copy.txt
diff --git a/image.png b/image.png
new file mode 100644
index 0000000..1111111
Binary files /dev/null and b/image.png differ
diff --git a/mode.sh b/mode.sh
old mode 100644
new mode 100755
`
	result, err := ParseUnifiedDiff(patch, testOptions())
	if err != nil {
		t.Fatalf("metadata-only diff rejected: %v", err)
	}
	if len(result.ChangeSet.Files) != 4 {
		t.Fatalf("got %d files, want 4", len(result.ChangeSet.Files))
	}
	byPath := make(map[string]domain.ChangedFile, len(result.ChangeSet.Files))
	for _, file := range result.ChangeSet.Files {
		byPath[file.NewPath] = file
		if file.Patch != nil {
			t.Errorf("metadata-only file %s unexpectedly has patch %q", file.NewPath, *file.Patch)
		}
	}
	if got := byPath["new name.md"]; got.Status != domain.FileRenamed || got.OldPath != "old name.md" {
		t.Errorf("unexpected rename: %+v", got)
	}
	if got := byPath["copy.txt"]; got.Status != domain.FileCopied || got.OldPath != "source.txt" {
		t.Errorf("unexpected copy: %+v", got)
	}
	if got := byPath["image.png"]; !got.IsBinary || got.Status != domain.FileAdded {
		t.Errorf("unexpected binary file: %+v", got)
	}
	if got := byPath["mode.sh"]; got.Status != domain.FileModified || got.Language != "shell" {
		t.Errorf("unexpected mode-only file: %+v", got)
	}
}

func TestParseUnifiedDiffMultipleHunksAndStableOrdering(t *testing.T) {
	patch := `diff --git a/z.txt b/z.txt
--- a/z.txt
+++ b/z.txt
@@ -2,2 +2,3 @@
 old
+new
 next
diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
-before
+after
`
	first, err := ParseUnifiedDiff(patch, testOptions())
	if err != nil {
		t.Fatalf("multi-file diff rejected: %v", err)
	}
	second, err := ParseUnifiedDiff(patch, testOptions())
	if err != nil {
		t.Fatalf("second parse rejected: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced different results:\nfirst=%+v\nsecond=%+v", first, second)
	}
	if first.ChangeSet.Files[0].NewPath != "a.txt" || first.ChangeSet.Files[1].NewPath != "z.txt" {
		t.Fatalf("files are not stably sorted: %+v", first.ChangeSet.Files)
	}
	if !reflect.DeepEqual(first.AddedLines, []AddedLine{{File: "a.txt", Line: 1}, {File: "z.txt", Line: 3}}) {
		t.Fatalf("unexpected line mapping: %+v", first.AddedLines)
	}
}

func TestParseUnifiedDiffRejectsMalformedAndUnsafeInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "missing prefix",
			input: `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1 @@
not a diff line
`,
		},
		{
			name: "hunk count mismatch",
			input: `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,1 @@
-only one old line
`,
		},
		{
			name: "traversal path",
			input: `diff --git a/../secret b/../secret
--- a/../secret
+++ b/../secret
@@ -1 +1 @@
-old
+new
`,
		},
		{
			name: "windows traversal path",
			input: `diff --git a/..\secret b/..\secret
--- a/..\secret
+++ b/..\secret
@@ -1 +1 @@
-old
+new
`,
		},
		{
			name:  "nul byte",
			input: "diff --git a/a b/a\x00",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseUnifiedDiff(tc.input, testOptions()); err == nil {
				t.Fatal("malformed or unsafe input was accepted")
			}
		})
	}
}

func TestParseUnifiedDiffHonorsPatchLimits(t *testing.T) {
	patch := `diff --git a/large.txt b/large.txt
--- a/large.txt
+++ b/large.txt
@@ -1,1 +1,8 @@
-old
+one
+two
+three
+four
+five
+six
+seven
+eight
`
	result, err := ParseUnifiedDiff(patch, Options{
		BaseSHA:            testBaseSHA,
		HeadSHA:            testHeadSHA,
		MaxFilePatchBytes:  32,
		MaxTotalPatchBytes: 40,
	})
	if err != nil {
		t.Fatalf("limited diff rejected: %v", err)
	}
	file := result.ChangeSet.Files[0]
	if !result.ChangeSet.Truncated || !file.PatchTruncated {
		t.Fatalf("truncation flags not set: change=%+v file=%+v", result.ChangeSet, file)
	}
	if file.Patch == nil || len(*file.Patch) > 32 {
		t.Fatalf("stored patch exceeds file limit: %v", file.Patch)
	}
	if !reflect.DeepEqual(result.ChangeSet.TruncationReasons, []string{"max_file_patch_bytes"}) {
		t.Fatalf("unexpected truncation reasons: %v", result.ChangeSet.TruncationReasons)
	}
}

func TestParseUnifiedDiffHonorsTotalPatchLimit(t *testing.T) {
	patch := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1 +1,4 @@
-old
+one
+two
+three
+four
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1 +1,4 @@
-old
+one
+two
+three
+four
`
	result, err := ParseUnifiedDiff(patch, Options{
		BaseSHA:            testBaseSHA,
		HeadSHA:            testHeadSHA,
		MaxFilePatchBytes:  1024,
		MaxTotalPatchBytes: 60,
	})
	if err != nil {
		t.Fatalf("total-limited diff rejected: %v", err)
	}
	if !result.ChangeSet.Truncated {
		t.Fatalf("total truncation flag not set: %+v", result.ChangeSet)
	}
	if !reflect.DeepEqual(result.ChangeSet.TruncationReasons, []string{"max_total_patch_bytes"}) {
		t.Fatalf("unexpected truncation reasons: %v", result.ChangeSet.TruncationReasons)
	}
	var stored int
	for _, file := range result.ChangeSet.Files {
		if file.Patch != nil {
			stored += len(*file.Patch)
		}
	}
	if stored > 60 {
		t.Fatalf("stored patches exceed total limit: %d", stored)
	}
}

func TestParseUnifiedDiffRejectsInvalidOptions(t *testing.T) {
	if _, err := ParseUnifiedDiff("", Options{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA, MaxFilePatchBytes: -1}); err == nil {
		t.Fatal("negative file limit was accepted")
	}
	if _, err := ParseUnifiedDiff("", Options{BaseSHA: testBaseSHA, HeadSHA: testHeadSHA, MaxTotalPatchBytes: -1}); err == nil {
		t.Fatal("negative total limit was accepted")
	}
}
