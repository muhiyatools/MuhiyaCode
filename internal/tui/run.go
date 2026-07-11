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
)

func Run(options Options) error {
	if options.Bridge == nil {
		options.Bridge = NewBridge()
	}
	if options.Context == nil {
		options.Context = context.Background()
	}
	if options.Simple || os.Getenv("MUHIYA_SIMPLE_TUI") == "1" || !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return RunLine(options, os.Stdin, os.Stdout)
	}
	model := NewModel(options)
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
		answer, _, err := options.Runtime.Engine.Run(options.Context, prompt)
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
		case "", "/exit", "/quit":
			if line != "" || err == io.EOF {
				return nil
			}
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
