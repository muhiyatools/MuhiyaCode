package command

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/muhiya/muhiyacode/internal/contract"
)

const taskGraphProjectionName = "task-graph"

func (a *Application) loadTaskGraph(ctx context.Context, sessionID string) (contract.TaskGraph, error) {
	event, exists, err := a.db.LatestProjectionCheckpoint(ctx, sessionID, taskGraphProjectionName)
	if err != nil {
		return contract.TaskGraph{}, err
	}
	if !exists {
		return contract.TaskGraph{Version: contract.TaskGraphVersion}, nil
	}
	var graph contract.TaskGraph
	if err := json.Unmarshal(event.Checkpoint.Payload, &graph); err != nil {
		return contract.TaskGraph{}, fmt.Errorf("decode task graph: %w", err)
	}
	if err := graph.Validate(); err != nil {
		return contract.TaskGraph{}, err
	}
	return graph, nil
}
