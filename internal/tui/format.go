package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

// truncateMiddle (004 US5) shortens a path-like value by cutting from the
// middle, keeping the leading root and — with priority — the trailing filename
// around a single ellipsis. When the final path segment fits within the budget
// it is kept whole, so a file/edit row never loses the one identifying part
// (the filename) the way a head-anchored cut would. Plain (ANSI-free) input.
func truncateMiddle(value string, width int, ellipsis string) string {
	if width <= 0 || displayWidth(value) <= width {
		return value
	}
	ellWidth := displayWidth(ellipsis)
	if width <= ellWidth {
		// No room for content around the ellipsis; right-anchor so the filename
		// tail is what survives.
		return truncateLeft(value, width, ellipsis)
	}
	avail := width - ellWidth
	tailBudget := avail - avail/3 // ~2/3 of the room goes to the filename tail
	// Keep the final path segment whole when it fits within the available room.
	if sep := strings.LastIndexAny(value, "/\\"); sep >= 0 {
		if fileWidth := displayWidth(value[sep+1:]); fileWidth <= avail && fileWidth > tailBudget {
			tailBudget = fileWidth
		}
	}
	headBudget := avail - tailBudget
	clusters, widths := graphemeClusters(value)
	tailUsed, tailStart := 0, len(clusters)
	for i := len(clusters) - 1; i >= 0; i-- {
		w := widths[i]
		if tailUsed+w > tailBudget {
			break
		}
		tailUsed += w
		tailStart = i
	}
	headUsed, headEnd := 0, 0
	for i := 0; i < tailStart; i++ {
		w := widths[i]
		if headUsed+w > headBudget {
			break
		}
		headUsed += w
		headEnd = i + 1
	}
	return strings.Join(clusters[:headEnd], "") + ellipsis + strings.Join(clusters[tailStart:], "")
}

// truncateLeft keeps the rightmost characters of value, prefixing an ellipsis
// when it must cut, so the most-specific path segments stay visible.
func truncateLeft(value string, width int, ellipsis string) string {
	if width <= 0 || displayWidth(value) <= width {
		return value
	}
	clusters, widths := graphemeClusters(value)
	ellWidth := displayWidth(ellipsis)
	target := max(0, width-ellWidth)
	used := 0
	cut := len(clusters)
	for i := len(clusters) - 1; i >= 0; i-- {
		w := widths[i]
		if used+w > target {
			break
		}
		used += w
		cut = i
	}
	return ellipsis + strings.Join(clusters[cut:], "")
}

// taskSummaryLine renders the calm three-metric task summary (contract
// task-summary.md §3): credits · tokens · cache %, with a dimmed "interrupted"
// marker for early-ended tasks. Credits are the whole-SESSION total
// (stats.CreditsUSD now scans every session record — main, subagent, aux —
// converted via contract.USDToCredits, 2,500 credits per $25) and are omitted
// when unavailable; the token figure and cache tag are per-task and come from
// the SAME headline helper as the live activity line (008 T019,
// usage-display.md UD-1/UD-5) so the two views can never disagree.
func taskSummaryLine(stats contract.TaskStats, colors palette) string {
	var parts []string
	if stats.CreditsUSD != nil {
		prefix := ""
		if stats.CreditsEstimated {
			prefix = "~"
		}
		// Credits are the whole-session total (main + subagents + every priced
		// request); the "(session)" tag keeps that distinct from the per-task token
		// figure that follows.
		parts = append(parts, fmt.Sprintf("%scredits %.2f (session)", prefix, contract.USDToCredits(*stats.CreditsUSD)))
	}
	tokens, tag := headlineTokens(stats.Usage)
	parts = append(parts, contract.FullTokens(tokens)+" tokens", tag)
	// The harness-friction marker left with /errors (013 FR-013). Engine telemetry
	// still records friction (TaskStats.HarnessEvents, benchmark consumers); the
	// interface simply no longer reports on its own internals.
	line := colors.muted.Render(strings.Join(parts, " · "))
	if stats.StopCause != "" {
		// The interrupted marker is a state signal, not metadata — warning color
		// keeps it legible on every terminal theme.
		line += "  " + colors.warning.Render("interrupted")
	}
	return line
}

// headlineTokens derives the headline token figure and its cache tag
// (008 T019, usage-display.md §1 UD-1..3; amended by 013 FR-019).
//
// The figure is the task's COMPLETE token count — cache-read + uncached +
// completion. It previously showed only the billed work (uncached+completion),
// which answers "what did this cost" but not "how much did this task consume",
// and on a well-cached session the two differ by an order of magnitude. The
// cache tag alongside it still carries the cost story: percentage-only
// ("cache N%", or "cache n/a" on a zero denominator, which contract.HitRate
// reports as nil rather than 0/NaN). Without cache metrics the figure falls back
// to provider-reported TotalTokens with an explicit "cache unavailable" tag —
// never a synthesized split (UD-3, Principle VI).
//
// Both renderActivity and taskSummaryLine call this one helper, which is what
// makes the live line and the summary agree by construction (UD-1/UD-5, FR-024).
func headlineTokens(usage contract.Usage) (int, string) {
	if usage.CacheReadTokens != nil && usage.CacheMissTokens != nil {
		tokens := *usage.CacheReadTokens + *usage.CacheMissTokens + usage.CompletionTokens
		if rate := contract.HitRate(usage.CacheReadTokens, usage.CacheMissTokens); rate != nil {
			return tokens, fmt.Sprintf("cache %.0f%%", *rate*100)
		}
		return tokens, "cache n/a"
	}
	return usage.TotalTokens, "cache unavailable"
}

// modelDisplay went with the model chip: v1.1.0 stopped showing the active
// model, because users no longer choose or manage one.

// fitLine truncates a possibly styled line to the given visible width. It is
// ANSI-aware so escape sequences are measured as zero width and never cut in
// half, which keeps styled status/hint lines from truncating their visible
// text prematurely on narrow terminals.
func fitLine(value string, width int) string {
	if ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, max(1, width-1), "…")
}

// splitLineParts renders a two-column status line: leftParts joined by sep on the
// left, right flush against the right edge, the space between them padded out to
// width (Experience Overhaul A1 footer). Everything may be ANSI-styled — widths
// are measured ANSI-aware. When the whole line overflows, trailing left parts are
// dropped one at a time (the right cluster has priority) until it fits; if not
// even one left part fits alongside right, only the right is shown, flush-right,
// and truncated as a last resort. sep is the already-styled separator between left
// parts.
func splitLineParts(leftParts []string, right, sep string, width int) string {
	if width <= 0 {
		return ""
	}
	rightW := ansi.StringWidth(right)
	for n := len(leftParts); n >= 1; n-- {
		left := strings.Join(leftParts[:n], sep)
		leftW := ansi.StringWidth(left)
		if leftW+1+rightW <= width {
			return left + strings.Repeat(" ", width-leftW-rightW) + right
		}
	}
	if rightW <= width {
		return strings.Repeat(" ", width-rightW) + right
	}
	return fitLine(right, width)
}

func oneLine(value string, width int) string {
	// Collapse whitespace, then cluster-safe truncate by display width (uniseg), so
	// vocalized Arabic is not over-measured by per-rune width (006 T011).
	return truncateToWidth(strings.Join(strings.Fields(value), " "), width)
}

// oneLineEllipsis is oneLine with a visible truncation marker: a cut label or
// description ends in "…" instead of stopping mid-word with no signal.
func oneLineEllipsis(value string, width int) string {
	collapsed := strings.Join(strings.Fields(value), " ")
	if displayWidth(collapsed) <= width {
		return collapsed
	}
	return truncateToWidth(collapsed, max(1, width-1)) + "…"
}

func lineCount(value string) int {
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}

// formatUsage renders the /usage modal: the account plan's credit allowance as
// a progress bar per budget window (total / used / remaining / percentage), plus
// any extra credit balance. Everything is shown in credits — never dollars —
// converted at the plan rate (2,500 credits = $25, contract.USDToCredits). The
// modal is account-level only; session usage lives in /context.
func formatUsage(data *UsageData) string {
	lines := []string{}
	if data.PlanName != "" {
		lines = append(lines, "Plan: "+data.PlanName, "")
	}
	for i := range data.Windows {
		w := data.Windows[i]
		total := contract.USDToCredits(w.BudgetUSD)
		used := contract.USDToCredits(w.CurrentSpentUSD)
		reset := w.ResetTime
		if t, err := time.Parse(time.RFC3339, w.ResetTime); err == nil {
			reset = t.Local().Format("Jan 2 15:04")
		}
		lines = append(lines, contract.TitleWords(w.Name))
		lines = append(lines, creditMeterLines(used, total)...)
		if reset != "" {
			lines = append(lines, "  Resets "+reset)
		}
		lines = append(lines, "")
	}
	if data.ExtraTotal > 0 || data.ExtraRemaining > 0 {
		lines = append(lines, "Extra credits")
		lines = append(lines, creditMeterLines(data.ExtraTotal-data.ExtraRemaining, data.ExtraTotal)...)
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		lines = append(lines, "No plan credit data was returned by the gateway.")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// creditMeterLines renders one credit allowance as a progress bar plus the
// total / used / remaining / percentage breakdown, all in credits.
func creditMeterLines(used, total float64) []string {
	if used < 0 {
		used = 0
	}
	if used > total {
		used = total
	}
	remaining := total - used
	percent := 0.0
	if total > 0 {
		percent = used / total * 100
	}
	return []string{
		fmt.Sprintf("  %s %5.1f%% used", renderBar(used, total, 24), percent),
		fmt.Sprintf("  Total:     %s credits", formatCredits(total)),
		fmt.Sprintf("  Used:      %s credits", formatCredits(used)),
		fmt.Sprintf("  Remaining: %s credits", formatCredits(remaining)),
	}
}

// renderBar draws a fixed-width block-character progress bar for used/total.
func renderBar(used, total float64, width int) string {
	fraction := 0.0
	if total > 0 {
		fraction = used / total
	}
	fraction = min(1, max(0, fraction))
	filled := int(fraction*float64(width) + 0.5)
	// A non-zero balance always shows at least one filled cell, and a bar only
	// fills completely when the allowance is truly exhausted.
	if used > 0 && filled == 0 {
		filled = 1
	}
	if fraction < 1 && filled == width {
		filled = width - 1
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// The /errors harness-friction panel was removed with the command (013 FR-013).
// The engine still records friction for telemetry and benchmarks; the interface
// simply stopped reporting on its own internals to the user.

// formatCredits renders a credit amount: whole credits without decimals,
// fractional amounts with two.
func formatCredits(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

// formatContextReport renders the context modal (008 T026; replaced by 013
// FR-023 with the essentials card).
//
// It used to render nine diagnostic sections — per-model tables, per-pairing
// cache rows, an estimated category split, invalidation history, pressure
// internals, API/active time, lines±. That is engineering telemetry, and it
// buried the four things a user actually opens this modal to learn: how full
// the context is, what the session has consumed, how well the cache is working,
// and what it cost. Those four, plus the models in play, are all that remains.
//
// Every figure whose source is absent renders "unavailable" — never a stand-in
// zero (UD-8, Constitution VI). Token counts are complete and comma-grouped
// (FR-019/FR-020); the hit rate spans every stream in the session (FR-021).
func formatContextReport(report orchestrator.ContextReport, sessionModel string) string {
	aggregate := report.UsageAggregate
	free := max(0, report.ContextLimit-report.HistoryTokens)
	lines := []string{
		"Context",
		fmt.Sprintf("  In use:  %s of %s tokens (%.1f%%)", contract.FullTokens(report.HistoryTokens), contract.FullTokens(report.ContextLimit), report.Percent),
		fmt.Sprintf("  Free:    %s tokens", contract.FullTokens(free)),
		"Session",
		fmt.Sprintf("  Tokens:    %s in / %s out", contract.FullTokens(aggregate.SumPrompt), contract.FullTokens(aggregate.SumCompletion)),
	}
	// The cache line is omitted entirely rather than shown as "unavailable" when
	// no request ever reported cache fields: an absent line reads as "not
	// applicable here", while an unavailable one implies something went missing.
	if aggregate.CacheAvailable > 0 {
		lines = append(lines, fmt.Sprintf("  Cache:     %s read / %s uncached", contract.FullTokens(aggregate.SumCacheRead), contract.FullTokens(aggregate.SumCacheMiss)))
	}
	// FR-021: every stream, main and aux alike, so this describes the session the
	// user actually ran and not just its main conversation.
	lines = append(lines, "  Hit rate:  "+formatRate(aggregate.AllStreamHitRate))
	if report.SessionCreditsUSD != nil {
		prefix := ""
		if report.SessionCreditsEstimated {
			prefix = "~"
		}
		lines = append(lines, fmt.Sprintf("  Cost:      %s%s credits", prefix, formatCredits(contract.USDToCredits(*report.SessionCreditsUSD))))
	} else if report.SessionCreditsEligible > 0 {
		lines = append(lines, fmt.Sprintf("  Cost:      unavailable (%d of %d requests priced)", report.SessionCreditsPriced, report.SessionCreditsEligible))
	}
	if sessionModel != "" {
		lines = append(lines, "Model", "  Running:   "+sessionModel)
		// Warm models are the ones whose provider-side prefix cache this session
		// has already paid to build. Switching to one of them is nearly free;
		// switching to any other model starts from cold. It is the single most
		// useful cache fact a user can see before asking for something new.
		if warm := report.WarmModels; len(warm) > 1 {
			lines = append(lines, "  Warm:      "+strings.Join(warm, ", "))
		}
		// Which machine behind the router is holding this conversation's cache.
		// Omitted entirely on a direct connection, where there is no routing
		// layer and the row would be a permanent blank.
		if report.Upstream != "" {
			lines = append(lines, "  Served by: "+report.Upstream+" (pinned)")
		}
	}
	return strings.Join(lines, "\n")
}

func formatRate(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}
