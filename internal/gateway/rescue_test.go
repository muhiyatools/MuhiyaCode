package gateway

import (
	"testing"
)

func TestRescueJSONList(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"Array input", `[{"name":"read_file","arguments":{"path":"a"}}]`, 1},
		{"tool_calls envelope", `{"tool_calls":[{"name":"read_file","arguments":{"path":"a"}}]}`, 1},
		{"Empty array", `[]`, 0},
		{"More than 10 items", `[{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}},{"name":"read_file","arguments":{}}]`, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rescued, _ := RescueToolCalls(tt.content, []string{"read_file"})
			if len(rescued) != tt.want {
				t.Errorf("expected %d rescued, got %d", tt.want, len(rescued))
			}
		})
	}
}
