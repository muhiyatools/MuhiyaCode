package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"
	"github.com/muhiya/muhiyacode/internal/contract"
	"github.com/muhiya/muhiyacode/internal/orchestrator"
)

type inertProvider struct{}

func (inertProvider) Chat(context.Context, contract.ChatRequest) (contract.ChatResponse, error) {
	return contract.ChatResponse{Content: "ok"}, nil
}
func (inertProvider) ListModels(context.Context) ([]contract.Model, error) { return nil, nil }

func testRuntime(t *testing.T) Runtime {
	t.Helper()
	settings := &contract.Settings{Version: 1, PermissionMode: contract.PermissionNormal, Effort: contract.EffortMedium}
	settings.Provider.Type = "openai-compatible"
	settings.Provider.ActiveModelID = "main"
	settings.Provider.SubagentModelID = "fast"
	settings.Provider.Models = []contract.Model{{ID: "main", Name: "Main", ContextLimit: 64000}, {ID: "fast", Name: "Fast", ContextLimit: 32000}}
	settings.RTL.Mode = "auto"
	engine, err := orchestrator.NewEngine(orchestrator.EngineConfig{Settings: settings, Provider: inertProvider{}, Registry: orchestrator.NewRegistry(), InitialPlan: contract.Plan{Steps: []contract.PlanStep{{Title: "Inspect", Status: contract.PlanCompleted}, {Title: "Build", Status: contract.PlanInProgress}}}})
	if err != nil {
		t.Fatal(err)
	}
	return Runtime{Engine: engine, Settings: settings, Session: contract.Session{ID: "session", WorkspacePath: `C:\work\muhiya`, Title: "test"}}
}

func TestModelRendersAtMinimumTerminalSize(t *testing.T) {
	model := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 40, Height: 14})
	model = updated.(*Model)
	model.items = append(model.items,
		item{kind: "user", content: "Please inspect the project."},
		item{kind: "assistant", content: "## Result\nReady."},
		item{kind: "tool", tool: &toolView{name: "read_file", target: "main.go", state: "ok", output: "Read main.go.", summary: "Read main.go."}},
	)
	model.refreshViewport(true)
	view := model.View()
	if !strings.Contains(view.Content, "MuhiyaCode") || !strings.Contains(view.Content, "Enter send") {
		t.Fatalf("narrow view omitted core UI:\n%s", view.Content)
	}
	if model.viewport.Width() != 40 || model.viewport.Height() < 3 {
		t.Fatalf("viewport size = %dx%d", model.viewport.Width(), model.viewport.Height())
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
	for _, height := range []int{16, 20, 40} {
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

func TestHeaderShowsBothModelsAndNoticeStaysOutOfTranscript(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "1.0.0", Notice: "Set an API key with /login."})
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	view := m.View().Content
	for _, want := range []string{"Main", "Fast", "mode", "normal", "Set an API key"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q", want)
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
				{Name: "filesystem", Transport: "stdio", Enabled: true, State: "ready", ToolCount: 4},
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
	rendered := RenderMarkdown(source, 60, "off", newPalette())
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
	for _, line := range strings.Split(RenderMarkdown(source, 30, "off", newPalette()), "\n") {
		if w := ansiWidth(line); w > 30 {
			t.Fatalf("table line overflows narrow width (%d): %q", w, line)
		}
	}
}

func ansiWidth(line string) int {
	// Strip SGR sequences, then measure.
	stripped := regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(line, "")
	return runewidth.StringWidth(stripped)
}

func TestPromptStyleActivityAndPaste(t *testing.T) {
	m := NewModel(Options{Runtime: testRuntime(t), Version: "test"})
	_ = m.Init() // focuses the input, as the Bubble Tea runtime does
	m = mustUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// User prompts render with a "> " marker and no "YOU" header.
	m.items = append(m.items, item{kind: "user", content: "please fix the bug"})
	transcript := m.renderTranscript()
	if strings.Contains(transcript, "YOU") {
		t.Fatal("user prompt still shows the YOU header")
	}
	if !strings.Contains(transcript, ">") || !strings.Contains(transcript, "please fix the bug") {
		t.Fatalf("user prompt marker missing:\n%s", transcript)
	}

	// Idle: no standing "Ready" line above the input.
	if strings.Contains(m.View().Content, "Ready") {
		t.Fatal("idle view still shows the Ready line")
	}
	// Busy: the unified activity line shows status and plan progress.
	m.busy, m.status = true, "Thinking"
	m.reasoning = "checking the parser"
	activity := m.renderActivity()
	if !strings.Contains(activity, "Thinking") || !strings.Contains(activity, "plan 1/2") {
		t.Fatalf("activity component incomplete:\n%s", activity)
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
		for _, height := range []int{14, 16, 24, 40} {
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
			if command.name == "/exit" {
				continue // quits the program; nothing to render afterwards
			}
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
		if runewidth.StringWidth(line) > 10 {
			t.Fatalf("wrapped line exceeds width: %q", line)
		}
	}
	markdown := RenderMarkdown("# Result\n\n- one\n- `two`\n```go\nfmt.Println(1)\n```", 24, "off", newPalette())
	if strings.Contains(markdown, "```") || !strings.Contains(markdown, "Result") || !strings.Contains(markdown, "fmt.Println") {
		t.Fatalf("markdown render = %q", markdown)
	}
}
