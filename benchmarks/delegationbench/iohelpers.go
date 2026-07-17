package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/muhiya/muhiyacode/internal/gateway"
	"github.com/muhiya/muhiyacode/internal/state"
)

type rawLogger struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
}

func newRawLogger(path string) (*rawLogger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &rawLogger{file: file, writer: bufio.NewWriterSize(file, 64*1024)}, nil
}

func (l *rawLogger) Append(payload gateway.RawUsagePayload) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	line, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = l.writer.Write(append(line, '\n'))
	return err
}

func (l *rawLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	flushErr := l.writer.Flush()
	return errors.Join(flushErr, l.file.Close())
}

func setMuhiyaHome(home string) func() {
	previous, existed := os.LookupEnv(state.HomeEnvironment)
	_ = os.Setenv(state.HomeEnvironment, home)
	return func() {
		if existed {
			_ = os.Setenv(state.HomeEnvironment, previous)
		} else {
			_ = os.Unsetenv(state.HomeEnvironment)
		}
	}
}

func copyTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyOptionalFile(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return copyFile(source, destination)
}

func copyFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	return errors.Join(copyErr, closeErr)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
