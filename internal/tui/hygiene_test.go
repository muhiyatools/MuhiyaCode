package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// hexColorRE matches a #RRGGBB literal.
var hexColorRE = regexp.MustCompile(`#[0-9A-Fa-f]{6}`)

// TestNoHardcodedHexOutsidePalette (003 T040/visual-system.md §5) enforces that
// all color literals live in the palette table (render.go). Any hex elsewhere in
// internal/tui/*.go (non-test) is a hygiene violation — colors must be tokens.
func TestNoHardcodedHexOutsidePalette(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "render.go" {
			continue // render.go holds the palette table; tests are exempt
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if hexColorRE.MatchString(line) {
				t.Errorf("%s:%d hardcoded hex color outside the palette: %s", file, i+1, strings.TrimSpace(line))
			}
		}
	}
}
