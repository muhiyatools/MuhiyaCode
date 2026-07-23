package tui

import (
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// B-4: a payload that reports only cache fields (derived TotalTokens == 0) must
// still yield a real token figure and cache tag, so the live token+cache segment
// does not vanish. The render gates were extended to `TotalTokens > 0 ||
// CacheReadTokens != nil`; this pins the figure they then show.
func TestHeadlineTokensCacheOnlyPayload(t *testing.T) {
	read, miss := 8000, 2000
	usage := contract.Usage{TotalTokens: 0, CacheReadTokens: &read, CacheMissTokens: &miss}
	tokens, tag := headlineTokens(usage)
	if tokens <= 0 {
		t.Fatalf("cache-only payload must still yield a token figure, got %d", tokens)
	}
	if tag == "" {
		t.Fatal("cache-only payload must still yield a cache tag")
	}
}
