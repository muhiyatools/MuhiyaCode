package orchestrator

import "testing"

// TestSystemPromptArabicCacheNeutral (feature 006 T027; Constitution III/VI/X) is
// the cache-neutrality gate for Arabic support: the stable prompt prefix is
// byte-identical across constructions even when the workspace path is Arabic, and
// it never contains Arabic presentation forms — proving that RTL display shaping
// is TUI-presentation only and never reaches the cached prefix. Arabic content
// rides the user message (dynamic), so it cannot invalidate the prefix.
func TestSystemPromptArabicCacheNeutral(t *testing.T) {
	ctx := PromptContext{Workspace: "/عرب/مشروع", OS: "linux", Shell: "bash", Model: "m", ProjectMemory: true}
	a, b := SystemPrompt(ctx), SystemPrompt(ctx)
	if a != b {
		t.Fatal("system prompt with an Arabic workspace path is not deterministic")
	}
	for _, r := range a {
		if (r >= 0xFB50 && r <= 0xFDFF) || (r >= 0xFE70 && r <= 0xFEFF) {
			t.Fatalf("Arabic presentation form leaked into the stable prefix: %U", r)
		}
	}
}
