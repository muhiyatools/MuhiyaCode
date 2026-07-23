package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muhiya/muhiyacode/internal/contract"
)

// TM02: the field session that motivated the token-efficiency work — "build a
// snake game in a subdirectory". It is the plan's before/after yardstick, and
// the scenario each later workstream re-runs.
//
// The script is the HAPPY path (one plan, one dispatch, one write, one report):
// it measures the floor cost of the standard build-something flow. The failure
// shapes (truncated write, dead first dispatch) get their own scenarios once
// their fixes land.

const snakeGameBody = `<!doctype html><html><body><canvas id=g></canvas><script>/* snake */</script></body></html>`

// TestFieldTestSnakeGameBaseline records the wire cost of the standard flow and
// asserts workflow correctness: the checklist and the game land in the TARGET
// directory, and the session does the work itself in one unbroken conversation.
//
// The split-era version of this scenario needed seven requests: plan, delegate,
// then the executor's own read/write/report, then a tick and a wrap-up. The
// unified session needs no handoff and no report, so the same work is fewer
// turns of one warm conversation.
func TestFieldTestSnakeGameBaseline(t *testing.T) {
	engine, provider, dir := fieldTestEngine(t,
		// Plan: the checklist goes in the directory the work targets.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("p1", "write_file", `{"path":"games/snake/tasks.md","content":"# Snake\n\n- [ ] Build games/snake/snake.html with movement, food, score, game over\n\nDONE = the file opens and the snake moves.\n"}`),
		}},
		// Build it.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("w1", "write_file", `{"path":"games/snake/snake.html","content":`+jsonString(snakeGameBody)+`}`),
		}},
		// Tick the item now that its check passed.
		contract.ChatResponse{ToolCalls: []contract.ToolCall{
			contract.NewToolCall("p2", "write_file", `{"path":"games/snake/tasks.md","content":"# Snake\n\n- [x] Build games/snake/snake.html with movement, food, score, game over\n\nDONE = the file opens and the snake moves.\n"}`),
		}},
		contract.ChatResponse{Content: "Snake game created at games/snake/snake.html and verified."},
	)

	answer, stats, err := engine.Run(context.Background(), "Build a snake game in games/snake")
	if err != nil {
		t.Fatalf("snake task failed: %v", err)
	}
	if stats.ToolCalls != 3 {
		t.Fatalf("expected the session to do the work in three tool calls, got %d: %q", stats.ToolCalls, answer)
	}
	// The game landed in the TARGET directory.
	body, err := os.ReadFile(filepath.Join(dir, "games", "snake", "snake.html"))
	if err != nil || !strings.Contains(string(body), "canvas") {
		t.Fatalf("the game did not land in the target directory: err=%v body=%q", err, string(body))
	}
	// The checklist lives beside it, not at the workspace root.
	if _, err := os.Stat(filepath.Join(dir, "games", "snake", "tasks.md")); err != nil {
		t.Fatalf("tasks.md is not in the target directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.md")); err == nil {
		t.Fatal("a stray tasks.md was created at the workspace root")
	}

	logAccount(t, "snake-baseline", requestBytes(provider))
}

// jsonString quotes a Go string as a JSON string literal for scripted tool
// arguments (the game body contains quotes and slashes).
func jsonString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}
