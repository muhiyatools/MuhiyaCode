package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const (
	verificationConfigPath = ".muhiya/verification.json"
	maxVerificationConfig  = 64 << 10
)

type VerificationCommand struct {
	Name       string   `json:"name"`
	Command    string   `json:"command"`
	Extensions []string `json:"extensions,omitempty"`
	Required   bool     `json:"required"`
}

type VerificationConfig struct {
	Version  int                   `json:"version"`
	Commands []VerificationCommand `json:"commands"`
}

func DiscoverTestCommand(workspace string) string {
	commands, _ := DiscoverVerificationCommands(workspace, nil)
	if len(commands) == 0 {
		return ""
	}
	return commands[0].Command
}

func DiscoverVerificationCommands(workspace string, changed []string) ([]VerificationCommand, error) {
	if configured, exists, err := loadVerificationConfig(workspace); err != nil {
		return nil, err
	} else if exists {
		return filterVerificationCommands(configured.Commands, changed), nil
	}
	return discoverManifestCommandsForChanges(workspace, changed)
}

func loadVerificationConfig(workspace string) (VerificationConfig, bool, error) {
	path := filepath.Join(workspace, filepath.FromSlash(verificationConfigPath))
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return VerificationConfig{}, false, nil
	}
	if err != nil {
		return VerificationConfig{}, false, err
	}
	defer file.Close()
	payload, err := ioReadBounded(file, maxVerificationConfig)
	if err != nil {
		return VerificationConfig{}, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var config VerificationConfig
	if err := decoder.Decode(&config); err != nil {
		return VerificationConfig{}, false, fmt.Errorf("decode %s: %w", verificationConfigPath, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return VerificationConfig{}, false, fmt.Errorf("decode %s: %w", verificationConfigPath, err)
	}
	if config.Version != 1 || len(config.Commands) == 0 || len(config.Commands) > 10 {
		return VerificationConfig{}, false, fmt.Errorf("%s requires version 1 and 1-10 commands", verificationConfigPath)
	}
	for index := range config.Commands {
		command := &config.Commands[index]
		command.Name = strings.TrimSpace(command.Name)
		command.Command = strings.TrimSpace(command.Command)
		if command.Name == "" || command.Command == "" || len(command.Command) > 2_000 {
			return VerificationConfig{}, false, fmt.Errorf("%s command %d is invalid", verificationConfigPath, index+1)
		}
	}
	return config, true, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func ioReadBounded(file *os.File, limit int64) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d-byte limit", limit)
	}
	payload, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("file exceeds %d-byte limit", limit)
	}
	return payload, nil
}

func filterVerificationCommands(commands []VerificationCommand, changed []string) []VerificationCommand {
	var result []VerificationCommand
	for _, command := range commands {
		if len(command.Extensions) == 0 || changedMatchesExtensions(changed, command.Extensions) {
			result = append(result, command)
		}
	}
	return result
}

func changedMatchesExtensions(changed, extensions []string) bool {
	for _, path := range changed {
		extension := strings.ToLower(filepath.Ext(path))
		for _, candidate := range extensions {
			if extension == strings.ToLower(candidate) {
				return true
			}
		}
	}
	return false
}

func discoverManifestCommands(workspace string) ([]VerificationCommand, error) {
	return discoverManifestCommandsForChanges(workspace, nil)
}

// discoverManifestCommandsForChanges selects only manifests that own a changed
// file. It never recursively scans sibling projects. With no changed paths it
// considers only root manifests, which preserves the explicit "verify this
// workspace" API without turning every nested fixture into a required suite.
func discoverManifestCommandsForChanges(workspace string, changed []string) ([]VerificationCommand, error) {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	manifests := make(map[string]bool)
	if len(changed) == 0 {
		for _, name := range verificationManifestNames {
			path := filepath.Join(root, name)
			if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
				manifests[path] = true
			}
		}
	} else {
		for _, changedPath := range changed {
			path := changedPath
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, filepath.FromSlash(path))
			}
			path = filepath.Clean(path)
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
			directory := filepath.Dir(path)
			for {
				found := false
				for _, name := range verificationManifestNames {
					if !manifestOwnsChange(name, path) {
						continue
					}
					manifest := filepath.Join(directory, name)
					if info, statErr := os.Stat(manifest); statErr == nil && !info.IsDir() {
						manifests[manifest] = true
						found = true
					}
				}
				if found || directory == root {
					break
				}
				parent := filepath.Dir(directory)
				if parent == directory {
					break
				}
				directory = parent
			}
		}
	}
	ordered := make([]string, 0, len(manifests))
	for manifest := range manifests {
		ordered = append(ordered, manifest)
	}
	sort.Strings(ordered)
	var commands []VerificationCommand
	for _, manifest := range ordered {
		commands = append(commands, commandsForManifest(workspace, manifest)...)
	}
	return deduplicateVerificationCommands(commands), nil
}

var verificationManifestNames = []string{"go.mod", "package.json", "Cargo.toml", "pyproject.toml"}

func manifestOwnsChange(manifest, changedPath string) bool {
	base := filepath.Base(changedPath)
	extension := strings.ToLower(filepath.Ext(base))
	switch manifest {
	case "go.mod":
		return extension == ".go" || base == "go.mod" || base == "go.sum"
	case "package.json":
		switch extension {
		case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".json", ".css", ".scss", ".html", ".vue", ".svelte":
			return true
		}
		return base == "package-lock.json" || base == "pnpm-lock.yaml" || base == "yarn.lock"
	case "Cargo.toml":
		return extension == ".rs" || base == "Cargo.toml" || base == "Cargo.lock"
	case "pyproject.toml":
		return extension == ".py" || base == "pyproject.toml" || strings.HasPrefix(base, "requirements")
	default:
		return false
	}
}

func commandsForManifest(workspace, manifest string) []VerificationCommand {
	directory := filepath.Dir(manifest)
	relative, _ := filepath.Rel(workspace, directory)
	quoted := strconv.Quote(relative)
	switch filepath.Base(manifest) {
	case "go.mod":
		if relative == "." {
			return []VerificationCommand{{Name: "go test", Command: "go test ./...", Required: true}}
		}
		return []VerificationCommand{{Name: "go test", Command: "go -C " + quoted + " test ./...", Required: true}}
	case "Cargo.toml":
		return []VerificationCommand{{Name: "cargo test", Command: "cargo test --manifest-path " + strconv.Quote(manifest), Required: true}}
	case "pyproject.toml":
		return []VerificationCommand{{Name: "pytest", Command: "python -m pytest " + quoted, Required: true}}
	case "package.json":
		return packageCommands(manifest, quoted)
	default:
		return nil
	}
}

func packageCommands(manifest, quotedDirectory string) []VerificationCommand {
	file, err := os.Open(manifest)
	if err != nil {
		return nil
	}
	defer file.Close()
	payload, err := ioReadBounded(file, maxVerificationConfig)
	if err != nil {
		return nil
	}
	var packageFile struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(payload, &packageFile) != nil {
		return nil
	}
	var commands []VerificationCommand
	for _, script := range []string{"test", "lint", "typecheck"} {
		if strings.TrimSpace(packageFile.Scripts[script]) == "" {
			continue
		}
		commands = append(commands, VerificationCommand{
			Name: script, Command: "npm --prefix " + quotedDirectory + " run " + script,
			Required: true,
		})
	}
	if len(commands) == 0 {
		commands = append(commands, VerificationCommand{
			Name: "npm test", Command: "npm --prefix " + quotedDirectory + " test",
			Required: true,
		})
	}
	return commands
}

func deduplicateVerificationCommands(commands []VerificationCommand) []VerificationCommand {
	seen := make(map[string]bool)
	result := commands[:0]
	for _, command := range commands {
		if seen[command.Command] {
			continue
		}
		seen[command.Command] = true
		result = append(result, command)
	}
	return result
}

func VerifyWorkspace(ctx context.Context, workspace string, runCmd func(context.Context, string) contract.ToolResult) contract.VerificationResult {
	return VerifyWorkspaceChanges(ctx, workspace, nil, runCmd)
}

func VerifyWorkspaceChanges(
	ctx context.Context,
	workspace string,
	changed []string,
	runCmd func(context.Context, string) contract.ToolResult,
) contract.VerificationResult {
	commands, err := DiscoverVerificationCommands(workspace, changed)
	if err != nil {
		return contract.VerificationResult{Ran: true, Result: "failure", OutputTruncated: err.Error()}
	}
	if len(commands) == 0 {
		return contract.VerificationResult{Ran: false}
	}
	var outputs []string
	var executed []string
	result := "success"
	for _, command := range commands {
		if ctx.Err() != nil {
			result = "failure"
			outputs = append(outputs, ctx.Err().Error())
			break
		}
		toolResult := runCmd(ctx, command.Command)
		executed = append(executed, command.Command)
		outputs = append(outputs, verificationOutput(command.Name, toolResult))
		if toolResult.Status != contract.ToolOutcomeSucceeded && command.Required {
			result = "failure"
			break
		}
	}
	output := strings.Join(outputs, "\n")
	if len(output) > 1_500 {
		output = output[len(output)-1_500:]
	}
	return contract.VerificationResult{
		Ran: true, Command: strings.Join(executed, " && "),
		Result: result, OutputTruncated: output,
	}
}

func verificationOutput(name string, result contract.ToolResult) string {
	output := result.Output
	if result.Err != nil && !strings.Contains(output, result.Err.Error()) {
		if output != "" {
			output += "\n"
		}
		output += result.Err.Error()
	}
	return "[" + name + "]\n" + output
}
