package contract

import (
	"fmt"
	"time"
)

const TaskGraphVersion = 1

type TaskNodeStatus string

const (
	TaskNodePending    TaskNodeStatus = "pending"
	TaskNodeInProgress TaskNodeStatus = "in_progress"
	TaskNodeCompleted  TaskNodeStatus = "completed"
	TaskNodeBlocked    TaskNodeStatus = "blocked"
)

type TaskEvidence struct {
	Kind      string    `json:"kind"`
	Reference string    `json:"reference"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type TaskNode struct {
	ID           string         `json:"id"`
	Title        string         `json:"title"`
	Status       TaskNodeStatus `json:"status"`
	Dependencies []string       `json:"dependencies,omitempty"`
	Evidence     []TaskEvidence `json:"evidence,omitempty"`
}

type PlanApproval struct {
	Approved bool      `json:"approved"`
	Scope    []string  `json:"scope"`
	Summary  string    `json:"summary,omitempty"`
	At       time.Time `json:"at"`
}

type TaskGraph struct {
	Version   int           `json:"version"`
	Revision  int           `json:"revision"`
	Source    string        `json:"source,omitempty"`
	Nodes     []TaskNode    `json:"nodes"`
	Approval  *PlanApproval `json:"approval,omitempty"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

func (graph TaskGraph) Validate() error {
	if graph.Version != TaskGraphVersion {
		return fmt.Errorf("unsupported task graph version %d", graph.Version)
	}
	known := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.ID == "" || node.Title == "" {
			return fmt.Errorf("task graph node is incomplete")
		}
		switch node.Status {
		case TaskNodePending, TaskNodeInProgress, TaskNodeCompleted, TaskNodeBlocked:
		default:
			return fmt.Errorf("task graph node %s has invalid status %q", node.ID, node.Status)
		}
		if known[node.ID] {
			return fmt.Errorf("duplicate task graph node %s", node.ID)
		}
		known[node.ID] = true
	}
	for _, node := range graph.Nodes {
		for _, dependency := range node.Dependencies {
			if !known[dependency] || dependency == node.ID {
				return fmt.Errorf("task graph node %s has invalid dependency %s", node.ID, dependency)
			}
		}
	}
	visiting := make(map[string]bool, len(graph.Nodes))
	visited := make(map[string]bool, len(graph.Nodes))
	dependencies := make(map[string][]string, len(graph.Nodes))
	for _, node := range graph.Nodes {
		dependencies[node.ID] = node.Dependencies
	}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("task graph contains a dependency cycle at %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range dependencies[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		return nil
	}
	for _, node := range graph.Nodes {
		if err := visit(node.ID); err != nil {
			return err
		}
	}
	return nil
}
