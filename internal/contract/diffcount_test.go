package contract

import "testing"

func TestDiffCountsSkipsOnlyRealHeaders(t *testing.T) {
	output := "edited\n--- diff ---\n" +
		"--- a/main.c\n" +
		"+++ b/main.c\n" +
		"@@ -1,3 +1,4 @@\n" +
		" int f() {\n" +
		"+++counter;\n" + // an ADDED line whose content is "++counter;"
		"---i;\n" + // a REMOVED line whose content was "--i;"
		"+done();\n" +
		"-old();\n"
	add, remove, ok := DiffCounts(output)
	if !ok {
		t.Fatal("diff not detected")
	}
	// Headers ("--- a/…", "+++ b/…") are excluded; the ++/-- code lines count.
	if add != 2 || remove != 2 {
		t.Fatalf("add=%d remove=%d, want 2/2 — code lines starting with ++/-- must not be skipped as headers", add, remove)
	}
}
