package gateway

import "strings"

// Upstream affinity (OpenRouter). A model slug on OpenRouter is served by
// several upstream providers — minimax-m3 lists nine — and each keeps its OWN
// prompt cache. The session learns who served it from the response's provider
// field (sse.go), pins later requests back to that upstream via
// contract.ChatRequest.PinUpstream (the provider.order body field, built in
// chatOnce), and the two helpers here keep that pin honest: the candidate
// renderer defeats the slug/display-name mismatch, and the rejection latch
// guarantees the pin can never fail a task.

func (p *OpenAICompatible) upstreamPinRejected() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pinRejected
}

func (p *OpenAICompatible) rejectUpstreamPin() {
	p.mu.Lock()
	p.pinRejected = true
	p.mu.Unlock()
}

// upstreamOrderCandidates renders every plausible slug spelling of a reported
// upstream name for OpenRouter's provider.order array.
//
// The mismatch this exists for: order accepts lowercase SLUGS ("novita",
// "deepinfra", "atlas-cloud"), the response's provider field reports DISPLAY
// names ("Novita", "DeepInfra", "AtlasCloud"), and an entry that matches no
// slug is skipped silently — so passing the display name verbatim pins nothing
// and reports no error. The slug scheme is not one rule either: DeepInfra's
// slug is "deepinfra" but AtlasCloud's is "atlas-cloud", so neither plain
// lowercasing nor camel-hyphenation alone covers the catalog.
//
// Unmatched entries cost nothing, so we send every spelling and let OpenRouter
// keep the one that exists: base slug first (matches any variant of the
// provider), the camel-hyphenated form, then the reported value verbatim (in
// case the response was already a slug, possibly with a "/fp8"-style variant
// suffix worth targeting exactly).
func upstreamOrderCandidates(reported string) []string {
	trimmed := strings.TrimSpace(reported)
	lower := strings.ToLower(trimmed)
	hyphenated := strings.ToLower(camelToHyphen(trimmed))
	ordered := []string{
		strings.SplitN(lower, "/", 2)[0],
		strings.SplitN(hyphenated, "/", 2)[0],
		lower,
		hyphenated,
		trimmed,
	}
	seen := make(map[string]bool, len(ordered))
	candidates := ordered[:0]
	for _, candidate := range ordered {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}
	return candidates
}

// camelToHyphen inserts a hyphen at each lower→upper camel boundary:
// "AtlasCloud" → "Atlas-Cloud". Boundaries inside an acronym run ("GMICloud")
// are left alone, which matches that provider's real slug ("gmicloud").
func camelToHyphen(value string) string {
	var out strings.Builder
	out.Grow(len(value) + 2)
	previousLower := false
	for _, r := range value {
		upper := r >= 'A' && r <= 'Z'
		if upper && previousLower {
			out.WriteByte('-')
		}
		out.WriteRune(r)
		previousLower = r >= 'a' && r <= 'z'
	}
	return out.String()
}
