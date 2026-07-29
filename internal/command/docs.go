package command

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muhiya/muhiyacode/internal/workspace"
	"github.com/spf13/cobra"
)

const generatedCapabilitiesPath = "docs/generated/CAPABILITIES.md"

func newDocsCommand() *cobra.Command {
	command := &cobra.Command{Use: "docs", Short: "Generate or check runtime capability documentation"}
	command.AddCommand(
		&cobra.Command{
			Use: "check", Short: "Fail when generated runtime documentation is stale", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				rendered, err := renderRuntimeDocs()
				if err != nil {
					return err
				}
				existing, err := os.ReadFile(generatedCapabilitiesPath)
				if err != nil {
					return err
				}
				if !bytes.Equal(existing, rendered) {
					return errors.New("generated capability documentation is stale; run `muhiyacode docs generate`")
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "Generated runtime documentation is current.")
				return err
			},
		},
		&cobra.Command{
			Use: "generate", Short: "Regenerate runtime capability documentation", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				rendered, err := renderRuntimeDocs()
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(generatedCapabilitiesPath), 0o755); err != nil {
					return err
				}
				temp, err := os.CreateTemp(filepath.Dir(generatedCapabilitiesPath), ".capabilities-*.tmp")
				if err != nil {
					return err
				}
				tempName := temp.Name()
				defer os.Remove(tempName)
				if _, err = temp.Write(rendered); err == nil {
					err = temp.Sync()
				}
				if closeErr := temp.Close(); err == nil {
					err = closeErr
				}
				if err == nil {
					err = os.Rename(tempName, generatedCapabilitiesPath)
				}
				return err
			},
		},
	)
	return command
}

func renderRuntimeDocs() ([]byte, error) {
	var output strings.Builder
	output.WriteString("# Generated MuhiyaCode Runtime Capabilities\n\n")
	output.WriteString("Generated from the command, tool, sandbox, and language registries. Do not edit by hand.\n\n")
	renderCommandTable(&output)
	if err := renderToolTable(&output); err != nil {
		return nil, err
	}
	renderLanguageTable(&output)
	renderSandboxTable(&output)
	return []byte(output.String()), nil
}

func renderCommandTable(output *strings.Builder) {
	output.WriteString("## CLI commands\n\n| Command | Description |\n|---|---|\n")
	root := NewRootCommand()
	var rows [][2]string
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, command := range parent.Commands() {
			if command.Hidden || command.Name() == "completion" || command.Name() == "help" {
				continue
			}
			rows = append(rows, [2]string{command.CommandPath(), command.Short})
			visit(command)
		}
	}
	visit(root)
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	for _, row := range rows {
		fmt.Fprintf(output, "| `%s` | %s |\n", row[0], escapeMarkdown(row[1]))
	}
	output.WriteString("\n")
}

func renderToolTable(output *strings.Builder) error {
	root, err := os.MkdirTemp("", "muhiyacode-docs-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	service, err := workspace.New(root, workspace.Options{})
	if err != nil {
		return err
	}
	defer service.Close()
	output.WriteString("## Built-in tools\n\n| Tool | Description |\n|---|---|\n")
	definitions := make([][2]string, 0)
	for _, tool := range service.Tools() {
		definition := tool.Definition().Function
		definitions = append(definitions, [2]string{definition.Name, definition.Description})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i][0] < definitions[j][0] })
	for _, definition := range definitions {
		fmt.Fprintf(output, "| `%s` | %s |\n", definition[0], escapeMarkdown(definition[1]))
	}
	output.WriteString("\n")
	return nil
}

func renderLanguageTable(output *strings.Builder) {
	output.WriteString("## Code intelligence\n\n| Language | Outline | Definition | References |\n|---|---|---|---|\n")
	seen := make(map[string]workspace.CodeIntelligenceCapability)
	for extension := range workspace.SupportedPolyglotExtensions {
		capability, _ := workspace.CodeIntelligenceFor("file" + extension)
		seen[capability.Language] = capability
	}
	languages := make([]string, 0, len(seen))
	for language := range seen {
		languages = append(languages, language)
	}
	sort.Strings(languages)
	for _, language := range languages {
		capability := seen[language]
		fmt.Fprintf(output, "| %s | %s | %s | %s |\n",
			language, capability.Outline, capability.Definition, capability.References)
	}
	output.WriteString("\n")
}

func renderSandboxTable(output *strings.Builder) {
	output.WriteString("## Sandbox backends\n\n| OS | Backend | Runtime contract |\n|---|---|---|\n")
	for _, support := range workspace.SandboxSupportMatrix() {
		fmt.Fprintf(output, "| %s | %s | %s |\n", support.OS, support.Backend, support.Status)
	}
	output.WriteString("\n")
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}
