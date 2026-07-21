package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Frame helpers shared by the mouse/selection/scroll tests. They lived in
// mouse_test.go, which was deleted with the agent-chip click handling; none of
// them has anything to do with agents, so they moved here.

// renderFrame drives one full View() pass so viewport geometry (height, scroll
// position, follow state) resolves the way a real frame resolves it. Tests that
// assert on scroll behaviour need the frame to have happened.
func renderFrame(m *Model) { _ = m.View() }

// screenRowContaining returns the screen row (0-indexed) whose drawn text
// contains needle, or -1. Styling is stripped first so a match is not defeated
// by the ANSI escapes around it.
func screenRowContaining(content, needle string) int {
	for row, line := range strings.Split(content, "\n") {
		if strings.Contains(ansi.Strip(line), needle) {
			return row
		}
	}
	return -1
}
