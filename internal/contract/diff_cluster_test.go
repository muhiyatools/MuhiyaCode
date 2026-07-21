package contract

import "testing"

// E-1: a hunk body that deletes "-- deprecated" (rendered "--- deprecated") or
// adds "++ current" (rendered "+++ current") must be counted. The old space
// heuristic skipped both as file headers, under-reporting the change.
func TestDiffCountsCountsDoubleDashBodyLines(t *testing.T) {
	output := "edited\n--- diff ---\n" +
		"--- a/q.sql\n" +
		"+++ b/q.sql\n" +
		"@@ -1,3 +1,3 @@\n" +
		" SELECT 1;\n" +
		"--- deprecated\n" +
		"+++ current\n" +
		" SELECT 2;\n"
	add, remove, ok := DiffCounts(output)
	if !ok {
		t.Fatal("diff not detected")
	}
	if add != 1 || remove != 1 {
		t.Fatalf("add=%d remove=%d, want 1/1 — hunk-body ---/+++ lines must be counted, not skipped as headers", add, remove)
	}
}

// E-3: an adjacent "--- old"/"+++ new" pair INSIDE a hunk body (deleting
// "-- old", adding "++ new") must not be mistaken for a file header, or it
// injects a phantom target file.
func TestPatchTargetFilesIgnoresBodyDashPairs(t *testing.T) {
	patch := "--- a/q.sql\n+++ b/q.sql\n@@ -1,3 +1,3 @@\n SELECT 1;\n--- deprecated\n+++ current\n SELECT 2;\n"
	files := PatchTargetFiles(patch)
	if len(files) != 1 || files[0] != "q.sql" {
		t.Fatalf("PatchTargetFiles = %v, want [q.sql] — a hunk-body ---/+++ pair must not inject a phantom file", files)
	}
}
