package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

type BackgroundProcess = contract.BackgroundProcess

type managedProcess struct {
	info      BackgroundProcess
	cmd       *exec.Cmd
	recovered bool
}

// ProcessManager owns background process-tree handles until explicit stop,
// session switch, or application shutdown.
type ProcessManager struct {
	mu           sync.Mutex
	processes    map[string]managedProcess
	registryPath string
}

func newProcessManager(paths ...string) *ProcessManager {
	manager := &ProcessManager{processes: make(map[string]managedProcess)}
	if len(paths) > 0 {
		manager.registryPath = paths[0]
		manager.load()
	}
	return manager
}

func (manager *ProcessManager) adopt(cmd *exec.Cmd, label string) (string, error) {
	if manager == nil || cmd == nil || cmd.Process == nil {
		return "", fmt.Errorf("cannot adopt an unstarted process")
	}
	id, err := checkpointID()
	if err != nil {
		return "", err
	}
	process := managedProcess{
		info: BackgroundProcess{
			ID:        id,
			Label:     label,
			PID:       cmd.Process.Pid,
			StartedAt: time.Now().UTC(),
		},
		cmd: cmd,
	}
	manager.mu.Lock()
	manager.processes[id] = process
	err = manager.persistLocked()
	if err != nil {
		delete(manager.processes, id)
	}
	manager.mu.Unlock()
	if err != nil {
		killProcessTree(cmd)
		return "", fmt.Errorf("persist process ownership: %w", err)
	}
	return id, nil
}

func (manager *ProcessManager) List() []BackgroundProcess {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	result := make([]BackgroundProcess, 0, len(manager.processes))
	for _, process := range manager.processes {
		result = append(result, process.info)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].StartedAt.Before(result[right].StartedAt)
	})
	return result
}

func (manager *ProcessManager) Stop(id string) error {
	if manager == nil {
		return fmt.Errorf("background process %q not found", id)
	}
	manager.mu.Lock()
	process, ok := manager.processes[id]
	if ok {
		delete(manager.processes, id)
	}
	persistErr := manager.persistLocked()
	manager.mu.Unlock()
	if !ok {
		return fmt.Errorf("background process %q not found", id)
	}
	var stopErr error
	if process.recovered {
		stopErr = stopRecoveredProcessTree(process.info.PID)
	} else {
		killProcessTree(process.cmd)
	}
	return errors.Join(persistErr, stopErr)
}

func (manager *ProcessManager) Close() error {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	processes := make([]managedProcess, 0, len(manager.processes))
	for _, process := range manager.processes {
		processes = append(processes, process)
	}
	manager.processes = make(map[string]managedProcess)
	persistErr := manager.persistLocked()
	manager.mu.Unlock()
	for _, process := range processes {
		if process.recovered {
			if err := stopRecoveredProcessTree(process.info.PID); err != nil && persistErr == nil {
				persistErr = err
			}
		} else {
			killProcessTree(process.cmd)
		}
	}
	return persistErr
}

func (manager *ProcessManager) load() {
	if manager.registryPath == "" {
		return
	}
	payload, err := os.ReadFile(manager.registryPath)
	if err != nil || len(payload) > 1<<20 {
		return
	}
	var records []BackgroundProcess
	if json.Unmarshal(payload, &records) != nil {
		return
	}
	for _, record := range records {
		if record.ID == "" || record.PID <= 0 || !recoverableProcessTree(record) {
			continue
		}
		process, err := os.FindProcess(record.PID)
		if err != nil || process == nil {
			process = &os.Process{Pid: record.PID}
		}
		manager.processes[record.ID] = managedProcess{
			info: record, cmd: &exec.Cmd{Process: process}, recovered: true,
		}
	}
	_ = manager.persistLocked()
}

func (manager *ProcessManager) persistLocked() error {
	if manager.registryPath == "" {
		return nil
	}
	records := make([]BackgroundProcess, 0, len(manager.processes))
	for _, process := range manager.processes {
		records = append(records, process.info)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	payload, err := json.Marshal(records)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(manager.registryPath), 0o700); err != nil {
		return err
	}
	return writeAtomic(manager.registryPath, payload, 0o600)
}

func ListProcessRegistry(path string) ([]BackgroundProcess, error) {
	manager := newProcessManager(path)
	return manager.List(), nil
}

func StopRegisteredProcess(path, id string) error {
	manager := newProcessManager(path)
	return manager.Stop(id)
}
