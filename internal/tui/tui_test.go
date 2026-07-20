package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

type inertProvider struct{}

func (inertProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	return contract.ChatResponse{Content: "ok"}, nil
}
func (inertProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }

func testRuntime(t testing.TB) Runtime {
	t.Helper()
	settings := &contract.Settings{Version: 1, PermissionMode: contract.PermissionNormal, Effort: contract.EffortMedium}
	settings.Provider.Type = "openai-compatible"
	settings.Provider.ActiveModelID = "main"
	settings.Provider.SubagentModelID = "fast"
	settings.Provider.Models = []contract.Model{{ID: "main", Name: "Main", ContextLimit: 64000}, {ID: "fast", Name: "Fast", ContextLimit: 32000}}
	settings.RTL.Mode = "auto"
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{Settings: settings, Provider: inertProvider{}, Registry: orchestrator.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	return Runtime{Engine: engine, Settings: settings, Session: contract.Session{ID: "session", WorkspacePath: `C:\work\muhiya`, Title: "test"}}
}

func TestModelRendersAtMinimumTerminalSize(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	// 003 (T017): 40×14 is below the 60×20 render floor → the clean too-small pane.
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	model = updated.(*Model)
	if !strings.Contains(model.View().Content, "terminal too small") {
		t.Fatalf("below-floor view did not show the too-small message:\n%s", model.View().Content)
	}
	// At the design baseline the full UI renders.
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)
	model.items = append(model.items,
		item{kind: "user", content: "Please inspect the project."},
		item{kind: "assistant", content: "## Result\nReady."},
		item{kind: "tool", tool: &toolView{name: "read_file", target: "main.go", state: "ok", output: "Read main.go.", summary: "Read main.go."}},
	)
	model.refreshViewport(true)
	view := model.View()
	if !strings.Contains(view.Content, "MuhiyaCode") {
		t.Fatalf("baseline view omitted core UI:\n%s", view.Content)
	}
	if model.viewport.Width() != 80 || model.viewport.Height() < 3 {
		t.Fatalf("viewport size = %dx%d", model.viewport.Width(), model.viewport.Height())
	}
}

// TestTypingAndIdleTickDoNotReRenderTranscript is the US1/US2 hot-path guard:
// the O(all items) transcript render must fire only when the transcript changes,
// so a keystroke or an idle 100ms tick never pays it, while a streaming chunk
// (which does change the transcript) still does.
func TestTypingAndIdleTickDoNotReRenderTranscript(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "assistant answer line filler content here")
	}
	model.items = append(model.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	model.refreshViewport(true)
	baseRenders := model.transcriptRenders
	baseView := model.viewport.View()

	updated, _ = model.Update(tickMsg(model.started))
	model = updated.(*Model)
	if model.transcriptRenders != baseRenders {
		t.Fatalf("idle tick re-rendered the transcript (%d → %d)", baseRenders, model.transcriptRenders)
	}

	updated, _ = model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	model = updated.(*Model)
	if model.transcriptRenders != baseRenders {
		t.Fatalf("keystroke re-rendered the transcript (%d → %d)", baseRenders, model.transcriptRenders)
	}
	if model.viewport.View() != baseView {
		t.Fatal("transcript view changed on a keystroke")
	}

	updated, _ = model.Update(streamMsg{text: "new streamed token"})
	model = updated.(*Model)
	if model.transcriptRenders <= baseRenders {
		t.Fatalf("a streaming chunk must re-render the transcript (still %d)", model.transcriptRenders)
	}
}

// TestIdleStopsTickingBusyRestarts is the US2 T030 guard: when nothing needs
// animating, an arriving tick stops the loop (no recurring idle work); entering
// an animated state restarts it.
func TestIdleStopsTickingBusyRestarts(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)

	model.ticking = true
	model.busy = false
	model.flash = noticeState{}
	model.mcpModalAtRest = false
	updated, _ = model.Update(tickMsg(model.started))
	model = updated.(*Model)
	if model.ticking {
		t.Fatal("an idle tick must stop the animation loop (ticking=false)")
	}

	model.busy = true
	if cmd := model.ensureTick(); cmd == nil || !model.ticking {
		t.Fatal("entering a busy state must restart the tick")
	}

	// A notice with an expiry also keeps the loop alive.
	model.busy = false
	model.ticking = false
	model.flash = noticeState{text: "hi", expires: model.started.Add(5 * time.Second)}
	if !model.needsAnimation() {
		t.Fatal("a notice pending expiry must require the tick")
	}
}

// TestTranscriptBlockCacheReusesUnchangedItems is the US1 render-memoization
// guard: during streaming, an unchanged transcript item reuses its cached block
// (renderTextBlock is skipped) while the growing draft re-renders — so a
// transcript re-render is O(changed items), not O(all items).
func TestTranscriptBlockCacheReusesUnchangedItems(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)
	model.items = append(model.items,
		item{kind: "assistant", content: "## First\nStable answer content here."},
		item{kind: "assistant_draft", content: "streaming"},
	)
	model.refreshViewport(true)
	if model.items[0].cachedWidth != model.contentWidth() || model.items[0].cachedBlock == "" {
		t.Fatalf("immutable item was not cached: %+v", model.items[0])
	}
	// Poison the immutable item's cache, then change ONLY the streaming draft and
	// re-render. If the poisoned block survives, the unchanged item was reused
	// (not re-rendered); if it were re-rendered it would overwrite the poison.
	model.items[0].cachedBlock = "POISONED"
	model.items[1].content += " more streamed tokens"
	model.refreshViewport(true)
	if model.items[0].cachedBlock != "POISONED" {
		t.Fatal("cache not reused: an unchanged item was re-rendered during streaming")
	}
	if model.items[1].cachedLen != len(model.items[1].content) {
		t.Fatal("the changed streaming draft was not re-rendered")
	}
}

// TestStreamingAccumulatesAndToolMap is the US1 T019 guard: the Builder-backed
// draft accumulates every streamed byte in order and resets between answers, and
// tool output/finish route through the O(1) active-tool map.
func TestStreamingAccumulatesAndToolMap(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)

	chunks := []string{"Hello ", "world", "! ", "streamed ", "answer."}
	want := ""
	for _, c := range chunks {
		want += c
		updated, _ = model.Update(streamMsg{text: c})
		model = updated.(*Model)
	}
	last := model.items[len(model.items)-1]
	if last.kind != "assistant_draft" || last.content != want {
		t.Fatalf("streamed content lost/reordered: %q want %q", last.content, want)
	}

	// Finish, then a new stream must start a fresh draft (no bleed).
	updated, _ = model.Update(resultMsg{answer: want})
	model = updated.(*Model)
	updated, _ = model.Update(streamMsg{text: "Next"})
	model = updated.(*Model)
	if got := model.items[len(model.items)-1].content; got != "Next" {
		t.Fatalf("draft bled across answers: %q", got)
	}

	// Tool output/finish route through the active-tool map.
	updated, _ = model.Update(toolStartMsg{name: "run_shell"})
	model = updated.(*Model)
	if model.activeTools["run_shell"] == nil {
		t.Fatal("toolStart did not register the active tool")
	}
	updated, _ = model.Update(toolOutputMsg{name: "run_shell", output: "line\n"})
	model = updated.(*Model)
	if model.activeTools["run_shell"].output != "line\n" {
		t.Fatalf("tool output not applied via map: %q", model.activeTools["run_shell"].output)
	}
	updated, _ = model.Update(toolEndMsg{name: "run_shell", output: "done"})
	model = updated.(*Model)
	if model.activeTools["run_shell"] != nil {
		t.Fatal("finishTool did not remove the tool from the active map")
	}
}

func TestScrollUpFromBottomSticks(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	model = updated.(*Model)
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, "line content number filler text here")
	}
	model.items = append(model.items, item{kind: "assistant", content: strings.Join(lines, "\n\n")})
	model.refreshViewport(true)
	if !model.viewport.AtBottom() {
		t.Fatal("expected to start at bottom")
	}
	before := model.viewport.YOffset()
	updated2, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	model = updated2.(*Model)
	if model.viewport.YOffset() == before {
		t.Fatal("PageUp had no effect: viewport snapped back to bottom in the same update")
	}
}

func TestSlashCommandResolvesHighlightedMatch(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model = updated.(*Model)

	// Bare "/" resolves to the first (highlighted) command, not to "/".
	model.input.SetValue("/")
	if got := model.resolveSlash("/"); got != commands[0].name {
		t.Fatalf("bare slash resolved to %q, want %q", got, commands[0].name)
	}

	// A partial command resolves to its unique match.
	model.input.SetValue("/mc")
	model.commandIndex = 0
	if got := model.resolveSlash("/mc"); got != "/mcp" {
		t.Fatalf("/mc resolved to %q, want /mcp", got)
	}

	// An exact command keeps its arguments verbatim.
	model.input.SetValue("/mcp add files npx")
	if got := model.resolveSlash("/mcp add files npx"); got != "/mcp add files npx" {
		t.Fatalf("exact command changed to %q", got)
	}
}

func TestCommandPaletteNeverOverflows(t *testing.T) {
	// 003 (T017): heights at/above the 20-row render floor.
	for _, height := range []int{20, 24, 40} {
		model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
		updated, _ := model.Update(tea.WindowSizeMsg{Width: 70, Height: height})
		model = updated.(*Model)
		model.input.SetValue("/mc")
		// A tick drives a normal layout pass, then render.
		updated2, _ := model.Update(tickMsg(model.started))
		model = updated2.(*Model)
		view := model.View()
		if lines := lineCount(view.Content); lines > height {
			t.Fatalf("height %d: rendered %d lines (overflow)", height, lines)
		}
		if !strings.Contains(view.Content, "/mcp") {
			t.Fatalf("height %d: filtered command palette not visible", height)
		}
	}
}

// TestHeaderIsModelFreeAndNoticeStaysOutOfTranscript: v1.1.0 removed model
// names from the ambient chrome — users do not manage models, so naming them
// on every frame was noise. The header keeps brand, version, and the context
// meter; /context remains the surface that discloses the current pairing.
func TestHeaderIsModelFreeAndNoticeStaysOutOfTranscript(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "1.0.0", Notice: "Set an API key with /login."})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	view := m.View().Content
	for _, want := range []string{"MuhiyaCode", "context", "Esc stop", "Set an API key"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q", want)
		}
	}
	// The fixture's model names ("Main", "Fast") must not appear anywhere in
	// the ambient chrome.
	for _, forbidden := range []string{"Main", "Fast"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("model name %q still rendered in the chrome", forbidden)
		}
	}
	if len(m.items) != 0 {
		t.Fatalf("startup notice leaked into transcript: %d items", len(m.items))
	}
	// A system notice must never append to the transcript.
	m.notify("Settings saved")
	if len(m.items) != 0 || m.flash.text != "Settings saved" {
		t.Fatalf("notify() touched the transcript (items=%d flash=%q)", len(m.items), m.flash.text)
	}
}

func TestMCPModalAndSkillsSelection(t *testing.T) {
	acts := Actions{
		MCP: MCPActions{List: func(context.Context) ([]MCPServerInfo, error) {
			return []MCPServerInfo{
				{Name: "filesystem", Transport: "stdio", Enabled: true, State: "connected", ToolCount: 4},
				{Name: "supabase", Transport: "http", Enabled: false, OAuth: true, State: "disabled"},
			}, nil
		}},
		ListSkills: func(context.Context) ([]Skill, error) {
			return []Skill{{Name: "pdf", Description: "PDF"}, {Name: "xlsx", Description: "Sheets"}}, nil
		},
	}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "1.0.0", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})

	// /mcp opens a modal listing both servers plus an add entry.
	if cmd := m.runSlash("/mcp"); cmd != nil {
		m = mustUpdate(t, m, cmd())
	}
	if m.modal == nil {
		t.Fatal("/mcp did not open a modal")
	}
	var labels string
	for _, c := range m.modal.choices {
		labels += c.Label + "|"
	}
	if !strings.Contains(labels, "filesystem") || !strings.Contains(labels, "supabase") || !strings.Contains(labels, "Add server") {
		t.Fatalf("MCP modal entries wrong: %s", labels)
	}
	m.closeModal(-1)

	// Skills multi-select, driven by real key events: space toggles the
	// highlighted entry (Bubble Tea reports the key as "space"), and Enter
	// records only the checked entries (regression: it used to select every
	// skill regardless of the checkbox, and space did nothing).
	if cmd := m.runSlash("/skills"); cmd != nil {
		m = mustUpdate(t, m, cmd())
	}
	if m.modal == nil || !m.modal.multi {
		t.Fatal("/skills did not open a multi-select modal")
	}
	m = mustUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})  // highlight the second skill
	m = mustUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace}) // toggle it on
	if m.modal == nil || !m.modal.checked[1] || m.modal.checked[0] {
		t.Fatalf("space did not toggle the highlighted skill: %+v", m.modal.checked)
	}
	m = mustUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.selectedSkills) != 1 {
		t.Fatalf("expected exactly 1 selected skill, got %d", len(m.selectedSkills))
	}
	if _, ok := m.selectedSkills["xlsx"]; !ok {
		t.Fatalf("wrong skill recorded: %v", m.selectedSkills)
	}
}

func TestMarkdownTableRendersAligned(t *testing.T) {
	source := "| Indicator | Status |\n|-----------|--------|\n| GDP Growth | ~3.5-4.5% |\n| Inflation | elevated |"
	rendered := RenderMarkdown(source, 60, "off", "auto", newPalette("dark"))
	if strings.Contains(rendered, "|---") {
		t.Fatalf("raw separator leaked into output:\n%s", rendered)
	}
	if !strings.Contains(rendered, "│") || !strings.Contains(rendered, "┼") {
		t.Fatalf("table not rendered with box borders:\n%s", rendered)
	}
	if !strings.Contains(rendered, "GDP Growth") || !strings.Contains(rendered, "Inflation") {
		t.Fatalf("table cells missing:\n%s", rendered)
	}
	// A narrow terminal must not overflow the width.
	for _, line := range strings.Split(RenderMarkdown(source, 30, "off", "auto", newPalette("dark")), "\n") {
		if w := ansiWidth(line); w > 30 {
			t.Fatalf("table line overflows narrow width (%d): %q", w, line)
		}
	}
}

func ansiWidth(line string) int {
	// Strip SGR sequences, then measure via the one uniseg width family
	// (feature 010 T036) — the same displayWidth production code uses.
	stripped := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(line, "")
	return displayWidth(stripped)
}

func TestPromptStyleActivityAndPaste(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	_ = m.Init() // focuses the input, as the Bubble Tea runtime does
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// 003 (T027/FR-015): user prompts render with the surface band alone — no
	// "> " prefix and no "YOU" header.
	m.items = append(m.items, item{kind: "user", content: "please fix the bug"})
	transcript := m.renderTranscript()
	if strings.Contains(transcript, "YOU") {
		t.Fatal("user prompt still shows the YOU header")
	}
	if strings.Contains(transcript, "> please fix") {
		t.Fatalf("user prompt still shows the removed '> ' prefix:\n%s", transcript)
	}
	if !strings.Contains(transcript, "please fix the bug") {
		t.Fatalf("user prompt content missing:\n%s", transcript)
	}

	// Idle: no standing "Ready" line above the input.
	if strings.Contains(m.View().Content, "Ready") {
		t.Fatal("idle view still shows the Ready line")
	}
	// Busy: the activity line shows the status; the to-do panel (a separate View
	// section) shows the checklist with the in-progress step highlighted (A3,
	// replacing the old "plan N/M" activity bar).
	m.busy, m.status = true, "Working…"
	// The panel reads the tasks.md checklist mirror; seed one open item so the
	// busy branch has something to render.
	m.plan = contract.Plan{Steps: todoSteps(contract.PlanCompleted, contract.PlanInProgress, contract.PlanPending)}
	activity := m.renderActivity()
	if !strings.Contains(activity, "Working…") {
		t.Fatalf("activity component missing status:\n%s", activity)
	}
	todos := m.renderTodos()
	if todos == "" || !strings.Contains(todos, m.glyphs.todoActive) {
		t.Fatalf("to-do panel missing the in-progress checklist row:\n%s", todos)
	}
	m.busy = false

	// Paste lands in the input field.
	m = mustUpdate(t, m, tea.PasteMsg{Content: "pasted content"})
	if !strings.Contains(m.input.Value(), "pasted content") {
		t.Fatalf("paste did not reach the input: %q", m.input.Value())
	}
}

// TestModalRenderNeverPanics renders choice modals of every size class —
// including one- and two-choice modals like /permissions, /model, and
// permission confirms, which previously crashed with index out of range —
// across terminal heights, driving selection to both ends.
func TestModalRenderNeverPanics(t *testing.T) {
	for _, choiceCount := range []int{1, 2, 3, 4, 12} {
		// 003 (T017): heights at/above the 20-row render floor.
		for _, height := range []int{20, 24, 40} {
			m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
			m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: height})
			choices := make([]contract.QuestionChoice, choiceCount)
			for i := range choices {
				choices[i] = contract.QuestionChoice{Label: fmt.Sprintf("choice %d", i+1), Description: "description"}
			}
			m.openChoice("Pick", "Choose one of the options below.", choices, func(int) tea.Cmd { return nil })
			view := m.View().Content
			if !strings.Contains(view, "choice 1") {
				t.Fatalf("count=%d height=%d: first choice not rendered", choiceCount, height)
			}
			// Walk the selection past both ends and render at each step.
			for range choiceCount + 2 {
				m = mustUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
				_ = m.View()
			}
			for range choiceCount + 2 {
				m = mustUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
				_ = m.View()
			}
			if lines := lineCount(m.View().Content); lines > height {
				t.Fatalf("count=%d height=%d: modal overflowed to %d lines", choiceCount, height, lines)
			}
		}
	}
}

func mustUpdate(t *testing.T, m *Model, msg tea.Msg) *Model {
	t.Helper()
	updated, _ := m.Update(msg)
	return updated.(*Model)
}

// pump feeds a command's message back into Update (following one chained
// command per round) and renders after every step, failing on overflow. It
// mirrors what the Bubble Tea runtime does, so any panic in Update or View
// fails the test the same way it would crash the app.
func pump(t *testing.T, m *Model, cmd tea.Cmd, height int) *Model {
	t.Helper()
	for i := 0; cmd != nil && i < 6; i++ {
		msg := cmd()
		if msg == nil {
			break
		}
		if _, quit := msg.(tea.QuitMsg); quit {
			break
		}
		updated, next := m.Update(msg)
		m = updated.(*Model)
		if lines := lineCount(m.View().Content); lines > height {
			t.Fatalf("view overflowed to %d lines (height %d)", lines, height)
		}
		if _, isTick := msg.(tickMsg); isTick {
			// The animation tick is a runtime-driven timer, not part of the logical
			// command flow under test. Deliver it once (so the model processes the
			// transition) but do not chase its reschedules — otherwise this
			// synchronous pump would block on real 100ms tea.Tick sleeps.
			break
		}
		cmd = next
	}
	return m
}

// TestEverySlashCommandFlowIsCrashFree drives each palette command through the
// real Enter key path with working fake actions, then walks any modal it opens
// (down, space, enter), rendering after every step. This is the regression net
// for the class of crash where a command opened a small modal and the renderer
// indexed past its choices.
func TestEverySlashCommandFlowIsCrashFree(t *testing.T) {
	acts := Actions{
		SaveSettings:  func(context.Context, *contract.Settings) error { return nil },
		SetPermission: func(context.Context, contract.PermissionMode) error { return nil },
		Rewind:        func(context.Context) (string, error) { return "Restored checkpoint.", nil },
		ListSessions: func(context.Context) ([]contract.Session, error) {
			return []contract.Session{{ID: "s1", Title: "First session"}, {ID: "s2", Title: "Second session"}}, nil
		},
		SetAPIKey: func(context.Context, string) error { return nil },
		Logout:    func(context.Context) error { return nil },
		MCP: MCPActions{
			List: func(context.Context) ([]MCPServerInfo, error) { return nil, nil }, // empty: 2-choice modal
		},
		ListSkills: func(context.Context) ([]Skill, error) {
			return []Skill{{Name: "pdf", Description: "PDF"}}, nil
		},
	}
	for _, height := range []int{16, 30} {
		for _, command := range commands {
			m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
			m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 70, Height: height})
			m.input.SetValue(command.name)
			cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			_ = m.View()
			m = pump(t, m, cmd, height)
			// Walk any modal the command opened: move, toggle, confirm.
			for _, key := range []tea.KeyPressMsg{{Code: tea.KeyDown}, {Code: tea.KeySpace}, {Code: tea.KeyEnter}} {
				if m.modal == nil {
					break
				}
				var next tea.Cmd
				updated, next := m.Update(key)
				m = updated.(*Model)
				_ = m.View()
				m = pump(t, m, next, height)
			}
			_ = m.View()
		}
	}
}

func TestBridgeFallbackAndToolStreaming(t *testing.T) {
	bridge := NewBridge()
	bridge.SetFallback(func(request modalRequest) int { return 1 })
	callbacks := bridge.Callbacks()
	allowed, err := callbacks.Confirm(context.Background(), "allow?")
	if err != nil || allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	answers, err := callbacks.Ask(context.Background(), []contract.Question{{Question: "Pick", Choices: []contract.QuestionChoice{{Label: "A"}, {Label: "B"}}}})
	if err != nil || len(answers) != 1 || answers[0].Choice.Label != "B" {
		t.Fatalf("answers=%+v err=%v", answers, err)
	}

	model := NewModel(Options{Runtime: testRuntime(t)})
	model.items = append(model.items, item{kind: "tool", tool: &toolView{name: "run_shell", state: "running"}})
	model.appendToolOutput("run_shell", strings.Repeat("x", 9000))
	output := model.items[len(model.items)-1].tool.output
	if len(output) > 8_000 || !strings.Contains(output, "truncated") {
		t.Fatalf("stream cap failed: len=%d prefix=%q", len(output), output[:min(40, len(output))])
	}
	model.finishTool("run_shell", "exit code: 0\nok")
	if model.items[len(model.items)-1].tool.state != "ok" {
		t.Fatal("tool did not transition to complete")
	}
}

func TestMCPModalRefreshesUntilTerminal(t *testing.T) {
	calls := 0
	states := [][]MCPServerInfo{
		{{Name: "remote", Transport: "http", Enabled: true, State: "connecting"}},
		{{Name: "remote", Transport: "http", Enabled: true, State: "connecting", Status: "still connecting"}},
		{{Name: "remote", Transport: "http", Enabled: true, State: "connected", ToolCount: 3}},
	}
	acts := Actions{MCP: MCPActions{List: func(context.Context) ([]MCPServerInfo, error) {
		index := calls
		if index >= len(states) {
			index = len(states) - 1
		}
		calls++
		return states[index], nil
	}}}
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test", Actions: acts})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	// Drive the /mcp command which fires the first mcp-list action.
	pump(t, m, m.runSlash("/mcp"), 30)
	if m.modal == nil {
		t.Fatal("MCP modal did not open")
	}
	if !m.mcpModalAtRest {
		t.Fatal("MCP modal should be flagged at rest for live refresh")
	}
	if m.modal.choices[0].Label != "remote (connecting…)" {
		t.Fatalf("first snapshot label = %q", m.modal.choices[0].Label)
	}
	// Tick the simulation: the action chain produces a new mcp-list,
	// which we pump through Update. Two ticks get us from connecting
	// through connected.
	for i := 0; i < 3; i++ {
		updated, cmd := m.Update(mcpRefreshTickMsg{})
		m = updated.(*Model)
		if cmd == nil {
			break
		}
		pump(t, m, cmd, 30)
	}
	if calls < 3 {
		t.Fatalf("expected at least 3 List calls (initial + 2 refresh), got %d", calls)
	}
	foundConnected := false
	for _, choices := range []contract.QuestionChoice(nil) {
		_ = choices
	}
	for _, c := range m.modal.choices {
		if strings.Contains(c.Label, "● remote") || strings.Contains(c.Label, "remote") && !strings.Contains(c.Label, "connecting") {
			foundConnected = true
		}
	}
	if !foundConnected {
		t.Fatalf("modal choices did not advance to connected: %+v", m.modal.choices)
	}
	if m.mcpModalAtRest {
		t.Fatal("modal stays at rest after every server reached a terminal state")
	}
}

// TestTaskSummaryEntryHasThreeMetricsOnly (003 US2) verifies the post-task
// summary is a transcript entry with exactly credits/tokens/cache% — and none of
// the removed noise (duration, effort, task class, tool counts, session %).
func TestTaskSummaryEntryHasThreeMetricsOnly(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.busy = true
	sessionRate := 0.78
	credits := 0.0242
	stats := contract.TaskStats{Effort: contract.EffortLow, DurationMS: 1234, Usage: contract.Usage{TotalTokens: 5000, CacheReadTokens: ptrInt(3500), CacheMissTokens: ptrInt(1500)}, ToolCalls: 4, TaskClass: "build", SessionHitRate: &sessionRate, CreditsUSD: &credits}
	updated, _ := m.Update(statsMsg(stats))
	m = updated.(*Model)
	updated, _ = m.Update(resultMsg{answer: "done"})
	m = updated.(*Model)
	view := m.View().Content
	// Present: the three metrics (credits = 0.0242 USD × 100 = 2.42; cache 70%).
	for _, want := range []string{"credits 2.42", "tokens", "cache 70%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("summary missing %q\n%s", want, view)
		}
	}
	// Absent: all the removed noise.
	for _, gone := range []string{"1.2s", "build", "billed", "4 tools", "session 78%", "thought for"} {
		if strings.Contains(view, gone) {
			t.Fatalf("summary still shows removed field %q\n%s", gone, view)
		}
	}
	// The summary is a persistent transcript entry.
	found := false
	for _, it := range m.items {
		if it.kind == "summary" {
			found = true
		}
	}
	if !found {
		t.Fatal("no summary transcript entry was appended")
	}
}

// TestTaskSummaryOmitsUnavailableAndMarksInterrupted (003 FR-012/FR-013a).
func TestTaskSummaryOmitsUnavailableAndMarksInterrupted(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.busy = true
	// No credits → credits omitted; no cache fields → TotalTokens fallback with
	// the explicit "cache unavailable" tag (008 UD-3); interrupted marker present.
	stats := contract.TaskStats{Usage: contract.Usage{TotalTokens: 1200}, StopCause: contract.StopCauseUserStop}
	updated, _ := m.Update(statsMsg(stats))
	m = updated.(*Model)
	updated, _ = m.Update(resultMsg{answer: "partial"})
	m = updated.(*Model)
	view := m.View().Content
	if strings.Contains(view, "credits") {
		t.Fatalf("summary showed unavailable credits:\n%s", view)
	}
	// 008 T019 (UD-3/UD-8): a task without cache metrics renders the explicit
	// "cache unavailable" state — never an omitted tag or a synthesized rate.
	if !strings.Contains(view, "cache unavailable") || strings.Contains(view, "cache 0%") {
		t.Fatalf("summary lost the explicit cache-unavailable state:\n%s", view)
	}
	if !strings.Contains(view, "interrupted") {
		t.Fatalf("interrupted task not marked:\n%s", view)
	}
}

// TestThinkingTextNeverRenders pins Fix R3: raw model thinking never appears on
// screen. The bridge leaves ReasoningToken unwired (TestBridgeNeverStreamsThinking)
// and streamMsg carries answer text only, so a busy frame is spinner + status —
// with no reasoning tail line and none of the removed thinking language.
func TestThinkingTextNeverRenders(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	m.busy, m.status = true, "Working…"
	view := m.View().Content
	if !strings.Contains(view, "Working…") {
		t.Fatalf("busy frame missing the status verb:\n%s", view)
	}
	for _, banned := range []string{"thinking…", "Thinking…", "thought for "} {
		if strings.Contains(view, banned) {
			t.Fatalf("removed thinking language %q re-rendered:\n%s", banned, view)
		}
	}
	// Completion stays clean too: no thinking residue, no idle "Ready" line.
	updated, _ := m.Update(resultMsg{answer: "Hello"})
	m = updated.(*Model)
	view = m.View().Content
	if strings.Contains(view, "thought for ") {
		t.Fatalf("post-task thinking residue leaked into the view:\n%s", view)
	}
	if strings.Contains(view, "Ready") {
		t.Fatal("idle 'Ready' line leaked into the view")
	}
}

// TestTerminatedReasonSurfacesAsWarn (H5) verifies that a task-complete stats
// message carrying TerminatedReason surfaces as a TUI warn notice so the user
// knows why the task stopped short of the turn ceiling.
func TestTerminatedReasonSurfacesAsWarn(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	updated, _ := m.Update(statsMsg(contract.TaskStats{TerminatedReason: "repeated tool failures — 8 failures in the last 6 turns; stopping to report"}))
	m = updated.(*Model)
	if m.flash.level != "warn" || !strings.Contains(m.flash.text, "Task terminated") || !strings.Contains(m.flash.text, "repeated tool failures") {
		t.Fatalf("TerminatedReason did not surface as a warn: level=%q flash=%q", m.flash.level, m.flash.text)
	}
}

func ptrInt(v int) *int { return &v }

func TestRTLAndWrappingAreStable(t *testing.T) {
	source := `راجع workspace: F:\MuhiyaCode Agent\dist ثم نفّذ الاختبارات.`
	if got := RenderRTL(source, "native"); got != source {
		t.Fatalf("native mode changed input: %q", got)
	}
	visual := RenderRTL(source, "visual")
	if visual == source || !utf8.ValidString(visual) || !strings.Contains(visual, "workspace") {
		t.Fatalf("visual RTL output is invalid: %q", visual)
	}
	hasPresentationForm := false
	for _, value := range visual {
		if value >= '\uFB50' && value <= '\uFEFF' {
			hasPresentationForm = true
			break
		}
	}
	if !hasPresentationForm {
		t.Fatalf("Arabic was not shaped: %q", visual)
	}
	for _, line := range wrapPlain("alpha beta gamma delta epsilon", 10) {
		if displayWidth(line) > 10 {
			t.Fatalf("wrapped line exceeds width: %q", line)
		}
	}
	markdown := RenderMarkdown("# Result\n\n- one\n- `two`\n```go\nfmt.Println(1)\n```", 24, "off", "auto", newPalette("dark"))
	if strings.Contains(markdown, "```") || !strings.Contains(markdown, "Result") || !strings.Contains(markdown, "fmt.Println") {
		t.Fatalf("markdown render = %q", markdown)
	}
}

// TestUpdateAvailableSegment (v1.1.0 chrome): the header names a newer
// published version when one exists, and renders nothing at all otherwise —
// including when the check never completed (empty latest) or the installed
// build is ahead of the registry.
func TestUpdateAvailableSegment(t *testing.T) {
	frame := func(latest string) string {
		m := NewModel(Options{Runtime: testRuntime(t), Version: "1.1.0", LatestVersion: latest})
		m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
		return ansi.Strip(m.View().Content)
	}
	available := frame("1.2.0")
	if !strings.Contains(available, "Update available 1.2.0") {
		t.Fatalf("newer version not surfaced:\n%s", available)
	}
	if !strings.Contains(available, "you have 1.1.0") {
		t.Fatalf("installed version not shown alongside:\n%s", available)
	}
	for _, quiet := range []string{"", "1.1.0", "1.0.9"} {
		if got := frame(quiet); strings.Contains(got, "Update available") {
			t.Fatalf("update segment rendered for latest=%q:\n%s", quiet, got)
		}
	}
}

// TestHeaderLayoutOrder pins the v1.1.0 field-test layout: line 1 runs
// brand+version, then the workspace path, then the context meter. The path is
// the flexible segment — a long path truncates rather than pushing the context
// meter off the line.
func TestHeaderLayoutOrder(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "1.1.0"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	line1 := strings.Split(ansi.Strip(m.View().Content), "\n")[1]
	versionAt := strings.Index(line1, "v1.1.0")
	pathAt := strings.Index(line1, "muhiya")
	contextAt := strings.Index(line1, "context")
	if versionAt < 0 || pathAt < 0 || contextAt < 0 {
		t.Fatalf("header line 1 missing a segment (version=%d path=%d context=%d):\n%q", versionAt, pathAt, contextAt, line1)
	}
	if !(versionAt < pathAt && pathAt < contextAt) {
		t.Fatalf("header order must be version → path → context:\n%q", line1)
	}
}

func TestHeaderKeepsContextMeterOnNarrowTerminals(t *testing.T) {
	runtime := testRuntime(t)
	runtime.Session.WorkspacePath = `C:\a\very\deeply\nested\workspace\path\that\keeps\going\and\going\project`
	m := NewModel(Options{Runtime: runtime, Version: "1.1.0"})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 62, Height: 24})
	line1 := strings.Split(ansi.Strip(m.View().Content), "\n")[1]
	if !strings.Contains(line1, "context") {
		t.Fatalf("a long path pushed the context meter off line 1:\n%q", line1)
	}
	if !strings.Contains(line1, "project") {
		t.Fatalf("path truncation dropped the most-specific segment:\n%q", line1)
	}
}

// TestFormatDurationSpacesMinutesAndSeconds pins the requested "3m 07s" shape.
func TestFormatDurationSpacesMinutesAndSeconds(t *testing.T) {
	for _, row := range []struct {
		seconds int
		want    string
	}{
		{42, "42.0s"},
		{61, "1m 01s"},
		{90, "1m 30s"},
		{187, "3m 07s"},
		{3599, "59m 59s"},
	} {
		if got := formatDuration(time.Duration(row.seconds) * time.Second); got != row.want {
			t.Errorf("formatDuration(%ds) = %q, want %q", row.seconds, got, row.want)
		}
	}
}
