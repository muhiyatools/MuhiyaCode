package taskboard

import (
	"encoding/json"
	"fmt"
	"os"
)

type Status string

const (
	StatusTodo  Status = "todo"
	StatusDoing Status = "doing"
	StatusDone  Status = "done"
)

type Task struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status Status `json:"status"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Load() ([]Task, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read tasks: %w", err)
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("decode tasks: %w", err)
	}
	if err := Validate(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func Validate(tasks []Task) error {
	seen := make(map[int]struct{}, len(tasks))
	for _, task := range tasks {
		if task.ID <= 0 {
			return fmt.Errorf("task id must be positive: %d", task.ID)
		}
		if _, exists := seen[task.ID]; exists {
			return fmt.Errorf("duplicate task id: %d", task.ID)
		}
		seen[task.ID] = struct{}{}
		switch task.Status {
		case StatusTodo, StatusDoing, StatusDone:
		default:
			return fmt.Errorf("task %d has invalid status %q", task.ID, task.Status)
		}
	}
	return nil
}

func CountByStatus(tasks []Task) map[Status]int {
	counts := map[Status]int{
		StatusTodo:  0,
		StatusDoing: 0,
		StatusDone:  0,
	}
	for _, task := range tasks {
		counts[task.Status]++
	}
	return counts
}
