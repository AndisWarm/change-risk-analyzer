package signals

import (
	"change-risk-analyzer/internal/domain"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestDestructiveMigrationAnalyzerDetectsDropAndTruncate(t *testing.T) {
	patch := `--- a/db/migrations/001_cleanup.sql
+++ b/db/migrations/001_cleanup.sql
@@ -1,1 +1,4 @@
 -- migration
+DROP TABLE users;
+ALTER TABLE accounts DROP COLUMN legacy_token;
+TRUNCATE TABLE sessions;
`
	signals, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowChangeSet("db/migrations/001_cleanup.sql", patch, 3, 0))
	if err != nil || len(signals) != 3 {
		t.Fatalf("expected three destructive migration signals, got signals=%+v err=%v", signals, err)
	}
	wantNames := []string{"DROP TABLE", "DROP COLUMN", "TRUNCATE"}
	for i, signal := range signals {
		if err := signal.Validate(); err != nil {
			t.Fatalf("signal[%d] violates domain invariants: %v", i, err)
		}
		if signal.RuleID != DestructiveMigrationRuleID || signal.Category != domain.CategoryData || signal.Source != domain.SourceDeterministic {
			t.Errorf("unexpected signal[%d] identity: %+v", i, signal)
		}
		if signal.Confidence != 0.85 || signal.Weight != 35 || !strings.Contains(signal.Fact, wantNames[i]) {
			t.Errorf("unexpected signal[%d]: %+v", i, signal)
		}
		if len(signal.Evidence) != 1 || signal.Evidence[0].Side != domain.SideRight || signal.Evidence[0].StartLine != i+2 {
			t.Errorf("unexpected signal[%d] evidence: %+v", i, signal.Evidence)
		}
	}
}

func TestDestructiveMigrationAnalyzerDetectsUnboundedDelete(t *testing.T) {
	patch := `--- a/database/migration/002_purge.sql
+++ b/database/migration/002_purge.sql
@@ -1,1 +1,4 @@
 -- purge old records
+DELETE FROM audit_log;
+DELETE FROM sessions WHERE 1 = 1;
+DELETE FROM users WHERE id = ?;
`
	signals, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowChangeSet("database/migration/002_purge.sql", patch, 3, 0))
	if err != nil || len(signals) != 2 {
		t.Fatalf("expected two unbounded delete signals, got signals=%+v err=%v", signals, err)
	}
	if !strings.Contains(signals[0].Fact, "UNBOUNDED DELETE") || !strings.Contains(signals[1].Fact, "UNBOUNDED DELETE") {
		t.Fatalf("unexpected delete facts: %+v", signals)
	}
}

func TestDestructiveMigrationAnalyzerIgnoresSafeAndCommentedSQL(t *testing.T) {
	patch := `--- a/db/migrations/003_safe.sql
+++ b/db/migrations/003_safe.sql
@@ -1,1 +1,7 @@
 -- migration
+ALTER TABLE users ADD COLUMN display_name TEXT;
+ALTER TABLE users ALTER COLUMN display_name TYPE TEXT;
+DELETE FROM users WHERE id = ?;
+DELETE FROM users
+WHERE id = ?;
+-- DROP TABLE users;
+SELECT 'DROP TABLE users';
`
	signals, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowChangeSet("db/migrations/003_safe.sql", patch, 6, 0))
	if err != nil || len(signals) != 0 {
		t.Fatalf("safe/commented SQL produced signals: signals=%+v err=%v", signals, err)
	}
}

func TestDestructiveMigrationAnalyzerRespectsMigrationPathBoundary(t *testing.T) {
	patch := `--- a/sql/drop.sql
+++ b/sql/drop.sql
@@ -1,1 +1,2 @@
 SELECT 1;
+DROP TABLE users;
`
	for _, filePath := range []string{"sql/drop.sql", "db/query.sql", "db/migrations/004_drop.sql", "migrations/005_drop.sql", "db/migrate/006_drop.sql"} {
		signals, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowChangeSet(filePath, patch, 1, 0))
		if err != nil {
			t.Fatalf("analyzer failed for %s: %v", filePath, err)
		}
		want := strings.Contains(filePath, "migration") || strings.Contains(filePath, "migrate")
		if (len(signals) > 0) != want {
			t.Errorf("path %s signals=%+v, want signal=%v", filePath, signals, want)
		}
	}
}

func TestDestructiveMigrationAnalyzerIgnoresDeletedOnlyAndBinary(t *testing.T) {
	patch := `--- a/db/migrations/007_remove.sql
+++ b/db/migrations/007_remove.sql
@@ -1,2 +1,1 @@
 -- migration
-DROP TABLE users;
`
	file := workflowChangeSet("db/migrations/007_remove.sql", patch, 0, 1).Files[0]
	for _, candidate := range []domain.ChangedFile{
		file,
		func() domain.ChangedFile {
			copy := file
			copy.IsBinary = true
			return copy
		}(),
		func() domain.ChangedFile {
			copy := file
			copy.Patch = nil
			return copy
		}(),
	} {
		signals, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowWithFile(candidate))
		if err != nil || len(signals) != 0 {
			t.Errorf("deleted/binary/unavailable patch produced signal: file=%+v signals=%+v err=%v", candidate, signals, err)
		}
	}
}

func TestDestructiveMigrationAnalyzerStableAndContextAware(t *testing.T) {
	patchA := `--- a/db/migrations/009_a.sql
+++ b/db/migrations/009_a.sql
@@ -1,1 +1,2 @@
 SELECT 1;
+DROP TABLE a;
`
	patchZ := `--- a/db/migrations/009_z.sql
+++ b/db/migrations/009_z.sql
@@ -1,1 +1,2 @@
 SELECT 1;
+DROP TABLE z;
`
	changeSet := domain.ChangeSet{
		Files: []domain.ChangedFile{
			workflowChangeSet("db/migrations/009_z.sql", patchZ, 1, 0).Files[0],
			workflowChangeSet("db/migrations/009_a.sql", patchA, 1, 0).Files[0],
		},
		TotalFiles:     2,
		TotalAdditions: 2,
		BaseSHA:        testBaseSHA,
		HeadSHA:        testHeadSHA,
	}
	first, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("first analysis failed: %v", err)
	}
	second, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), changeSet)
	if err != nil {
		t.Fatalf("second analysis failed: %v", err)
	}
	if !reflect.DeepEqual(first, second) || len(first) != 2 || first[0].Evidence[0].File != "db/migrations/009_a.sql" {
		t.Fatalf("analysis is not stable: first=%+v second=%+v", first, second)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (DestructiveMigrationAnalyzer{}).Analyze(canceled, changeSet); err == nil {
		t.Fatal("canceled context was ignored")
	}
}

func TestDestructiveMigrationAnalyzerRejectsMalformedPatch(t *testing.T) {
	patch := `--- a/db/migrations/010_bad.sql
+++ b/db/migrations/010_bad.sql
@@ -1 +1 @@
not a hunk line
`
	if _, err := (DestructiveMigrationAnalyzer{}).Analyze(context.Background(), workflowChangeSet("db/migrations/010_bad.sql", patch, 0, 0)); err == nil {
		t.Fatal("malformed patch was accepted")
	}
}
