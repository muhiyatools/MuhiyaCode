package command

import (
	"testing"
)

func TestRootCommandEconomyFlags(t *testing.T) {
	cmd := NewRootCommand()
	if flag := cmd.Flags().Lookup("lean-prefix"); flag == nil {
		t.Fatal("missing --lean-prefix flag")
	}
	if flag := cmd.Flags().Lookup("economy"); flag == nil {
		t.Fatal("missing --economy flag")
	}
}
