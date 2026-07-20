package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/gateway"
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
	parts = append(parts, contract.HumanTokens(tokens)+" tokens", tag)
	// T013: mark harness friction (a blocked gate / failed tool) so a task that hit
	// it says so quietly. Zero events add nothing. /errors shows the detail.
	if stats.HarnessEvents > 0 {
		parts = append(parts, fmt.Sprintf("⚠ %d harness", stats.HarnessEvents))
	}
	// Feature 012 US5: one calm roll-up of subagent context reuse — how many
	// dispatches continued a predecessor's stream, and the verified cache share
	// when the provider reported it. Detail per dispatch lives in the link
	// notices and the bench record; zero links add nothing.
	if summary := linkSummaryPart(stats.Links); summary != "" {
		parts = append(parts, summary)
	}
	line := colors.muted.Render(strings.Join(parts, " · "))
	if stats.StopCause != "" {
		// The interrupted marker is a state signal, not metadata — warning color
		// keeps it legible on every terminal theme.
		line += "  " + colors.warning.Render("interrupted")
	}
	return line
}

// linkSummaryPart renders the feature-012 reuse roll-up for the task summary:
// "links 2/3 continued (cache 85%)" — continued count over linkable dispatches,
// with the mean verified share across reporting continuations. Shares come
// only from provider-reported fields; none reported → the share is omitted,
// never estimated (Constitution VI).
func linkSummaryPart(links []contract.LinkOutcome) string {
	if len(links) == 0 {
		return ""
	}
	continued, reported := 0, 0
	shareSum := 0.0
	for _, link := range links {
		if link.Decision == "continued" {
			continued++
			if link.CacheShare != nil {
				reported++
				shareSum += *link.CacheShare
			}
		}
	}
	if continued == 0 {
		return ""
	}
	part := fmt.Sprintf("links %d/%d continued", continued, len(links))
	if reported > 0 {
		part += fmt.Sprintf(" (cache %d%%)", int(shareSum/float64(reported)*100))
	}
	return part
}

// headlineTokens derives the honest headline token figure and its cache tag
// (008 T019, usage-display.md §1 UD-1..3). With per-task cache metrics the
// figure is the billed work — CacheMissTokens + CompletionTokens — and the tag
// is percentage-only ("cache N%", or "cache n/a" on a zero denominator, which
// contract.HitRate reports as nil rather than 0/NaN). Without cache metrics it
// falls back to the provider-reported TotalTokens with an explicit
// "cache unavailable" tag — never a synthesized split (UD-3, Principle VI).
// Both renderActivity and taskSummaryLine call this one helper (UD-1/UD-5).
func headlineTokens(usage contract.Usage) (int, string) {
	if usage.CacheReadTokens != nil && usage.CacheMissTokens != nil {
		tokens := *usage.CacheMissTokens + usage.CompletionTokens
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

// formatHarnessEvents renders the /errors panel (T012): a count-by-class header
// then the most recent 20 harness-friction events, oldest-of-those first. An
// empty ring reads as a clean run — the good case is stated, not blank.
func formatHarnessEvents(events []contract.HarnessEvent) string {
	if len(events) == 0 {
		return "No harness events this session — clean run."
	}
	counts := map[contract.HarnessEventClass]int{}
	for _, event := range events {
		counts[event.Class]++
	}
	var header []string
	for _, class := range []contract.HarnessEventClass{contract.HarnessGate, contract.HarnessTool, contract.HarnessProvider, contract.HarnessRecovery, contract.HarnessUI} {
		if counts[class] > 0 {
			header = append(header, fmt.Sprintf("%s %d", class, counts[class]))
		}
	}
	lines := []string{fmt.Sprintf("%d event(s) this session — %s", len(events), strings.Join(header, " · ")), ""}
	start := 0
	if len(events) > 20 {
		start = len(events) - 20
	}
	for _, event := range events[start:] {
		lines = append(lines, fmt.Sprintf("%s · %s/%s · %s", event.At.Local().Format("15:04:05"), event.Class, event.Code, contract.Digest(event.Detail, 80)))
	}
	return strings.Join(lines, "\n")
}

// formatCredits renders a credit amount: whole credits without decimals,
// fractional amounts with two.
func formatCredits(value float64) string {
	if value == float64(int64(value)) {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.2f", value)
}

// formatContextReport renders the context modal (008 T026, UD-6..UD-16): the
// session usage panel first (so short-terminal clipping drops detail, not
// headlines — UD-14), then the context window, the per-model usage table, the
// estimated category split, streams, and cache health. Every metric whose
// source is absent renders "unavailable" — never a stand-in zero (UD-8). Lines
// stay within the modal's 72 usable columns in the 2-space-indent label style
// (UD-13); variable-width model names are clamped in formatByModelRows.
func formatContextReport(report orchestrator.ContextReport) string {
	aggregate := report.UsageAggregate
	lines := []string{"This session"}
	// Session credits (contract.USDToCredits) under the member-set honesty rule
	// with the priced/eligible fallback (UD-6).
	if report.SessionCreditsUSD != nil {
		prefix := ""
		if report.SessionCreditsEstimated {
			prefix = "~"
		}
		lines = append(lines, fmt.Sprintf("  Credits used: %s%.2f", prefix, contract.USDToCredits(*report.SessionCreditsUSD)))
	} else if report.SessionCreditsEligible > 0 {
		lines = append(lines, fmt.Sprintf("  Credits used: unavailable (%d of %d requests priced)", report.SessionCreditsPriced, report.SessionCreditsEligible))
	} else {
		lines = append(lines, "  Credits used: unavailable")
	}
	// API time sums the persisted per-request durations; zero recorded time
	// means the source is absent, so it renders unavailable, not 0 (UD-8).
	apiTime := "unavailable"
	if report.APITimeMS > 0 {
		apiTime = formatDuration(time.Duration(report.APITimeMS) * time.Millisecond)
	}
	// Active time restarts at zero on resume and stays session-scoped (UD-9),
	// so zero is a true value there, not an unavailable state.
	lines = append(lines,
		"  API time:    "+apiTime,
		"  Active time: "+formatDuration(time.Duration(report.ActiveMS)*time.Millisecond)+" (this session)",
	)
	if report.LinesAdded > 0 || report.LinesRemoved > 0 {
		lines = append(lines, fmt.Sprintf("  Lines: +%d −%d", report.LinesAdded, report.LinesRemoved))
	} else {
		lines = append(lines, "  Lines: none")
	}
	lines = append(lines, fmt.Sprintf("  Prompt / output tokens: %s / %s", contract.HumanTokens(aggregate.SumPrompt), contract.HumanTokens(aggregate.SumCompletion)))
	if aggregate.CacheAvailable > 0 {
		lines = append(lines, fmt.Sprintf("  Cache read / uncached: %s / %s", contract.HumanTokens(aggregate.SumCacheRead), contract.HumanTokens(aggregate.SumCacheMiss)))
	} else {
		lines = append(lines, "  Cache read / uncached: unavailable")
	}
	lines = append(lines,
		"  Session hit rate:      "+formatRate(aggregate.SessionHitRate),
		"  Steady-state hit rate: "+formatRate(aggregate.SteadyStateHitRate),
	)
	// Context window: bar / in-use / free / pressure, unchanged from 003 T032.
	pressureSource := "provider-reported"
	if report.PressureEstimated {
		pressureSource = "estimated bootstrap"
	}
	free := max(0, report.ContextLimit-report.HistoryTokens)
	lines = append(lines,
		"",
		"Context window",
		fmt.Sprintf("  %s %5.1f%% full", renderBar(float64(report.HistoryTokens), float64(report.ContextLimit), 24), report.Percent),
		fmt.Sprintf("  In use:   %s of %s tokens", contract.HumanTokens(report.HistoryTokens), contract.HumanTokens(report.ContextLimit)),
		fmt.Sprintf("  Free:     %s tokens", contract.HumanTokens(free)),
		fmt.Sprintf("  Pressure: %s tokens (%.1f%%, %s)", contract.HumanTokens(report.PressureTokens), report.PressurePercent, pressureSource),
	)
	if len(report.ByModel) > 0 {
		lines = append(lines, "", "By model")
		lines = append(lines, formatByModelRows(report.ByModel)...)
	}
	if len(report.Categories) > 0 {
		// The single "(estimated)" marker lives in the header (UD-10).
		lines = append(lines, "", "Context by category (estimated)")
		lines = append(lines, formatCategoryRows(report.Categories)...)
	}
	lines = append(lines,
		"",
		"Streams",
		fmt.Sprintf("  Requests: main %d · aux %d · subagent %d", aggregate.MainRequests, aggregate.AuxRequests, aggregate.SubagentRequests),
	)
	if aggregate.UnavailableRequests > 0 {
		lines = append(lines, fmt.Sprintf("  Cache metrics unavailable: %d request(s)", aggregate.UnavailableRequests))
	}
	// Session and steady-state hit rates moved into the This session panel
	// (UD-6); Cache health keeps the cache-specific diagnostics so no figure
	// renders twice.
	lines = append(lines,
		"",
		"Cache health",
		"  Prefix stability rate: "+formatRate(aggregate.PrefixStabilityRate),
	)
	if report.MaintenanceLatched {
		lines = append(lines, "  Automatic maintenance: paused by anti-thrash latch")
	}
	if len(report.Invalidations) > 0 {
		lines = append(lines, "  Recent cache invalidations:")
		start := max(0, len(report.Invalidations)-5)
		for _, event := range report.Invalidations[start:] {
			lines = append(lines, fmt.Sprintf("    - %s: %s (%s)", event.Cause, event.Scope, event.At.Local().Format("15:04:05")))
		}
	}
	return strings.Join(lines, "\n")
}

// formatByModelRows renders the per-model usage table (UD-7): uncached input,
// output, cache read, and cost per model observed in the session, plus a Total
// row summing the columns. Cost follows the member-set rule — a nil row cost
// renders "unavailable" and the Total cost sums only when EVERY row has one
// (UD-8). Model names are padded into a column and clamped so every line fits
// the modal's 72 usable columns (UD-13).
func formatByModelRows(rows []contract.ModelUsageRow) []string {
	const maxLineWidth = 72
	costText := func(cost *float64, estimated int) string {
		if cost == nil {
			return "unavailable"
		}
		prefix := ""
		if estimated > 0 {
			prefix = "~"
		}
		return prefix + formatCredits(contract.USDToCredits(*cost))
	}
	type entry struct{ name, metrics string }
	metricsFor := func(row contract.ModelUsageRow) string {
		uncached, cacheRead := "unavailable", "unavailable"
		if row.CacheAvailable {
			uncached, cacheRead = contract.HumanTokens(row.UncachedIn), contract.HumanTokens(row.CacheRead)
		}
		return fmt.Sprintf("in %s · out %s · read %s · cost %s",
			uncached, contract.HumanTokens(row.Output), cacheRead, costText(row.CostUSD, row.Estimated))
	}
	total := contract.ModelUsageRow{Model: "Total", CacheAvailable: true, CostUSD: new(float64)}
	entries := make([]entry, 0, len(rows)+1)
	for _, row := range rows {
		entries = append(entries, entry{row.Model, metricsFor(row)})
		total.Requests += row.Requests
		total.UncachedIn += row.UncachedIn
		total.Output += row.Output
		total.CacheRead += row.CacheRead
		total.CacheAvailable = total.CacheAvailable && row.CacheAvailable
		total.Estimated += row.Estimated
		if row.CostUSD == nil {
			total.CostUSD = nil
		} else if total.CostUSD != nil {
			*total.CostUSD += *row.CostUSD
		}
	}
	entries = append(entries, entry{total.Model, metricsFor(total)})
	nameWidth, metricsWidth := 0, 0
	for _, e := range entries {
		nameWidth = max(nameWidth, displayWidth(e.name))
		metricsWidth = max(metricsWidth, displayWidth(e.metrics))
	}
	// "  " indent + name column + "  " gap + metrics must fit maxLineWidth.
	nameWidth = min(nameWidth, max(1, maxLineWidth-4-metricsWidth))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.name
		if displayWidth(name) > nameWidth {
			name = truncateToWidth(name, nameWidth-1) + "…"
		}
		pad := strings.Repeat(" ", max(0, nameWidth-displayWidth(name)))
		out = append(out, "  "+name+pad+"  "+e.metrics)
	}
	return out
}

// formatCategoryRows renders the estimated context-category split (UD-10):
// name, estimated tokens, and percent of the window, space-padded into columns
// (UD-13). The "(estimated)" label is carried once by the section header.
func formatCategoryRows(categories []orchestrator.ContextCategory) []string {
	nameWidth := 0
	for _, category := range categories {
		nameWidth = max(nameWidth, displayWidth(category.Name))
	}
	out := make([]string, 0, len(categories))
	for _, category := range categories {
		pad := strings.Repeat(" ", max(0, nameWidth-displayWidth(category.Name)))
		out = append(out, fmt.Sprintf("  %s%s  %7s  %5.1f%%", category.Name, pad, contract.HumanTokens(category.Tokens), category.Percent))
	}
	return out
}

// formatCapabilityProfile renders the active provider capability profile read-only
// in the /context modal (feature 007 T031, contracts/capability-profile.md §3): the
// documented limits, the operational budget, and each beta feature's adoption status,
// so a user can see exactly what MuhiyaCode will and will not send to the provider.
func formatCapabilityProfile(profile gateway.ModelProfile) string {
	lines := []string{
		"Provider capability (" + profile.Family + ")",
	}
	if profile.ContextWindowLimit > 0 {
		lines = append(lines, fmt.Sprintf("  Context window: %s documented · %s operational budget", contract.HumanTokens(profile.ContextWindowLimit), contract.HumanTokens(profile.DefaultContextWindow)))
	}
	if profile.OutputTokenLimit > 0 {
		lines = append(lines, fmt.Sprintf("  Max output:     %s documented · %s default", contract.HumanTokens(profile.OutputTokenLimit), contract.HumanTokens(profile.MaxOutputTokens)))
	}
	if len(profile.DeprecatedParams) > 0 {
		lines = append(lines, "  Never sent (deprecated): "+strings.Join(profile.DeprecatedParams, ", "))
	}
	for _, feature := range profile.BetaFeatures {
		lines = append(lines, fmt.Sprintf("  Beta %s: %s", feature.Name, feature.Status))
	}
	return strings.Join(lines, "\n")
}

func formatRate(value *float64) string {
	if value == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%.2f%%", *value*100)
}
