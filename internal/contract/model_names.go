package contract

import "strings"

// IsMiniMaxM3Name reports whether a model id/name denotes the MiniMax-M3
// family (any case; bare "M3" included). It lives in the foundation package as
// the SINGLE M3 detector so the fresh-install default pairing (internal/state)
// and the gateway capability profile can never disagree about what counts as
// M3 — and so `state` need not import `gateway` for it (feature 010 MS-6).
func IsMiniMaxM3Name(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(lower, "minimax-m3") ||
		strings.Contains(lower, "minimax m3") ||
		lower == "m3" ||
		strings.HasPrefix(lower, "m3 ")
}
