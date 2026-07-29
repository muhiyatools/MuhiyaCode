package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const taskGraphProjection = "task-graph"

func graphFromPlan(plan contract.Plan, source string, previous contract.TaskGraph) contract.TaskGraph {
	evidence := make(map[string][]contract.TaskEvidence, len(previous.Nodes))
	for _, node := range previous.Nodes {
		evidence[node.ID] = append([]contract.TaskEvidence(nil), node.Evidence...)
	}
	graph := contract.TaskGraph{
		Version: contract.TaskGraphVersion, Revision: previous.Revision + 1,
		Source: source, UpdatedAt: time.Now().UTC(), Approval: cloneApproval(previous.Approval),
	}
	var prior string
	for index, step := range plan.Steps {
		id := taskNodeID(index, step.Title)
		status := contract.TaskNodePending
		switch step.Status {
		case contract.PlanCompleted:
			status = contract.TaskNodeCompleted
		case contract.PlanInProgress:
			status = contract.TaskNodeInProgress
		}
		node := contract.TaskNode{
			ID: id, Title: step.Title, Status: status,
			Evidence: append([]contract.TaskEvidence(nil), evidence[id]...),
		}
		if prior != "" {
			node.Dependencies = []string{prior}
		}
		graph.Nodes = append(graph.Nodes, node)
		prior = id
	}
	return graph
}

func taskNodeID(index int, title string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", index, strings.TrimSpace(title))))
	return "task-" + hex.EncodeToString(sum[:6])
}

func cloneTaskGraph(graph contract.TaskGraph) contract.TaskGraph {
	cloned := graph
	cloned.Nodes = make([]contract.TaskNode, len(graph.Nodes))
	for index, node := range graph.Nodes {
		cloned.Nodes[index] = node
		cloned.Nodes[index].Dependencies = append([]string(nil), node.Dependencies...)
		cloned.Nodes[index].Evidence = append([]contract.TaskEvidence(nil), node.Evidence...)
	}
	cloned.Approval = cloneApproval(graph.Approval)
	return cloned
}

func cloneApproval(approval *contract.PlanApproval) *contract.PlanApproval {
	if approval == nil {
		return nil
	}
	cloned := *approval
	cloned.Scope = append([]string(nil), approval.Scope...)
	return &cloned
}

func (e *Engine) seedChecklistFromGraph() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.taskGraph.Version != contract.TaskGraphVersion {
		e.taskGraph = contract.TaskGraph{Version: contract.TaskGraphVersion}
	}
	e.checklist = planFromGraph(e.taskGraph)
}

func planFromGraph(graph contract.TaskGraph) contract.Plan {
	plan := contract.Plan{}
	for _, node := range graph.Nodes {
		status := contract.PlanPending
		switch node.Status {
		case contract.TaskNodeCompleted:
			status = contract.PlanCompleted
		case contract.TaskNodeInProgress:
			status = contract.PlanInProgress
		}
		plan.Steps = append(plan.Steps, contract.PlanStep{Title: node.Title, Status: status})
	}
	return plan
}

func (e *Engine) persistTaskGraph(ctx context.Context, graph contract.TaskGraph) error {
	payload, err := json.Marshal(graph)
	if err != nil {
		return err
	}
	taskID := e.executionTaskID()
	if taskID == "" {
		taskID = "session:" + e.session.ID + ":task-graph"
	}
	checkpoint := contract.ProjectionCheckpoint{
		Name: taskGraphProjection, Version: contract.TaskGraphVersion,
		Payload: payload, Checksum: contract.ProjectionChecksum(payload),
		CreatedAt: time.Now().UTC(),
	}
	return e.appendExecutionEvent(ctx, contract.ExecutionEvent{
		Version: contract.ExecutionEventVersion, TaskID: taskID,
		Kind: contract.ExecutionProjectionSaved, Checkpoint: &checkpoint,
	}, "persist task graph")
}

func (e *Engine) recordTaskEvidence(call contract.ToolCall) {
	if !e.callMayMutate(call) {
		return
	}
	reference := hashExecutionValue(call.ToolName() + "\x00" + call.ArgumentsJSON())[:24]
	evidence := contract.TaskEvidence{
		Kind: "mutation", Reference: reference,
		Summary:   strings.Join(contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON())), ", "),
		CreatedAt: time.Now().UTC(),
	}
	e.mu.Lock()
	graph := cloneTaskGraph(e.taskGraph)
	recorded := false
	for index := range graph.Nodes {
		if graph.Nodes[index].Status == contract.TaskNodeInProgress || graph.Nodes[index].Status == contract.TaskNodePending {
			graph.Nodes[index].Evidence = append(graph.Nodes[index].Evidence, evidence)
			recorded = true
			break
		}
	}
	if !recorded {
		e.mu.Unlock()
		return
	}
	graph.Revision++
	graph.UpdatedAt = time.Now().UTC()
	e.taskGraph = graph
	e.mu.Unlock()
	if err := e.persistTaskGraph(context.Background(), graph); err != nil {
		e.taskMu.Lock()
		e.taskEvidenceError = err.Error()
		e.taskMu.Unlock()
		e.callbacks.EmitNotice("Task-graph evidence could not be persisted: " + err.Error())
	}
}

func normalizeApprovalPaths(workspace string, values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.Clean(strings.TrimSpace(value))
		if value == "" || value == "." {
			continue
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(workspace, value)
		}
		result = append(result, filepath.Clean(value))
	}
	return result
}

func (e *Engine) setPlanApproval(ctx context.Context, approved bool, summary string, paths []string) error {
	approval := &contract.PlanApproval{
		Approved: approved, Scope: normalizeApprovalPaths(e.session.WorkspacePath, paths),
		Summary: summary, At: time.Now().UTC(),
	}
	e.mu.Lock()
	graph := cloneTaskGraph(e.taskGraph)
	graph.Approval = approval
	graph.Revision++
	graph.UpdatedAt = time.Now().UTC()
	e.taskGraph = graph
	e.mu.Unlock()
	if err := e.persistTaskGraph(ctx, graph); err != nil {
		return fmt.Errorf("persist plan approval: %w", err)
	}
	return nil
}

func (e *Engine) planScopeAllows(call contract.ToolCall) (bool, string) {
	e.mu.Lock()
	approval := cloneApproval(e.taskGraph.Approval)
	e.mu.Unlock()
	if approval == nil {
		return true, ""
	}
	if !approval.Approved {
		return false, "the latest change proposal was rejected"
	}
	targets := contract.ToolTargetPaths(call.ToolName(), []byte(call.ArgumentsJSON()))
	if len(targets) == 0 {
		return false, "the approved proposal does not authorize unscoped shell or external mutations"
	}
	for _, target := range targets {
		if !filepath.IsAbs(target) {
			target = filepath.Join(e.session.WorkspacePath, target)
		}
		target = filepath.Clean(target)
		if !pathMatchesApproval(target, approval.Scope) {
			return false, target + " is outside the approved file scope"
		}
	}
	return true, ""
}

func pathMatchesApproval(target string, scope []string) bool {
	for _, approved := range scope {
		if approved == "*" {
			return true
		}
		relative, err := filepath.Rel(approved, target)
		if err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))) {
			return true
		}
	}
	return false
}
