package tui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/muhiya/muhiyacode/internal/app"
)

func Run(options Options) error {
	if options.Bridge == nil {
		options.Bridge = NewBridge()
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Simple || os.Getenv("MUHIYA_SIMPLE_TUI") == "1" || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		// T021: line mode runs the engine synchronously, so a two-stage launch must
		// hydrate the runtime before delegating (the loading shell is TUI-only).
		if options.Runtime.Engine == nil && options.Hydrate != nil {
			result, err := options.Hydrate(options.Context)
			if err != nil {
				return err
			}
			options.Runtime, options.Actions, options.Recent = result.Runtime, result.Actions, result.Recent
			if options.Notice == "" {
				options.Notice = result.Notice
			}
		}
		return RunLine(options, os.Stdin, os.Stdout)
	}
	model := NewModel(options)
	// 006 T033: negotiate terminal BiDi. In auto/visual the app owns BiDi, so emit
	// BDSM-explicit (CSI 8 l) to stop a BiDi-capable terminal from double-reversing
	// the already-visual output; restore terminal-owned BiDi on exit. Ignored by
	// BiDi-agnostic terminals, so it is safe everywhere (research R7).
	if options.Runtime.Settings != nil {
		if seq := TerminalBiDiControl(options.Runtime.Settings.RTL.Mode); seq != "" {
			fmt.Fprint(os.Stdout, seq)
			defer fmt.Fprint(os.Stdout, "\x1b[8h")
		}
	}
	program := tea.NewProgram(model, tea.WithContext(options.Context))
	options.Bridge.Attach(program)
	finalModel, err := program.Run()
	if current, ok := finalModel.(*Model); ok && current.runtime.Engine != nil {
		current.runtime.Engine.Cancel()
		waitCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_ = current.runtime.Engine.WaitIdle(waitCtx)
		cancel()
	}
	return err
}

func RunLine(options Options, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	interactive := isTerminal(os.Stdin) && isTerminal(os.Stdout)
	options.Bridge.SetFallback(func(request modalRequest) int {
		if !interactive {
			return -1
		}
		fmt.Fprintf(output, "\n%s\n%s\n", request.title, request.message)
		for index, choice := range request.choices {
			marker := ""
			if choice.Recommended {
				marker = " (recommended)"
			}
			fmt.Fprintf(output, "  %d) %s%s\n", index+1, choice.Label, marker)
		}
		fmt.Fprint(output, "> ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		for index := range request.choices {
			if line == fmt.Sprintf("%d", index+1) {
				return index
			}
		}
		return recommendedChoice(request.choices)
	})
	fmt.Fprintf(output, "MuhiyaCode v%s · %s\n", options.Version, options.Runtime.Session.WorkspacePath)
	runPrompt := func(prompt string) error {
		// 006 (T030, FR-013): the model receives normalized logical Unicode, same as
		// the TUI submit path.
		answer, _, err := options.Runtime.Engine.Run(options.Context, app.AssemblePrompt(options.Context, prompt, nil, nil, nil))
		if err != nil {
			return err
		}
		fmt.Fprintln(output, answer)
		return nil
	}
	if strings.TrimSpace(options.InitialPrompt) != "" {
		if err := runPrompt(strings.TrimSpace(options.InitialPrompt)); err != nil {
			return err
		}
		if !interactive {
			return nil
		}
	}
	for {
		if interactive {
			fmt.Fprint(output, "\nmuhiya> ")
		}
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if err != nil && err != io.EOF {
			return err
		}
		if line == "" && err == io.EOF {
			return nil
		}
		switch line {
		case "":
			// Blank line: re-prompt. A blank line at EOF already returned above; end
			// the line-mode REPL with EOF (Ctrl+D) or by closing the input.
		case "/context":
			report := options.Runtime.Engine.ContextReport()
			fmt.Fprintf(output, "context %d/%d tokens (%.1f%%)\n", report.HistoryTokens, report.ContextLimit, report.Percent)
		case "/compact":
			message, compactErr := options.Runtime.Engine.Compact(options.Context)
			if compactErr != nil {
				fmt.Fprintln(output, "error:", compactErr)
			} else {
				fmt.Fprintln(output, message)
			}
		default:
			if strings.HasPrefix(line, "/") {
				fmt.Fprintln(output, "This command requires the full TUI or CLI subcommand.")
			} else if runErr := runPrompt(line); runErr != nil {
				fmt.Fprintln(output, "error:", runErr)
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
