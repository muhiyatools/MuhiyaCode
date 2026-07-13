package contract

// Project-context value types shared by the state, orchestrator, and command
// layers (features 005/006). Kept here so the sidecar snapshot and the
// orchestrator↔state probe cross the layer boundary using only contract types
// (the orchestrator must not import state). Feature 006 replaced the append-only
// memory ledger with a plain MEMORY.md file, so memory identity is a content hash
// exactly like MUHIYA.md — no ledger sequences, events, or item projections.

// ProjectContextVersion is the current project_context.json sidecar schema
// version. A sidecar written by an older schema (feature 005's ledger-based
// memory) is discarded on read so the boot block is recomputed once for the
// current file-based format.
const ProjectContextVersion = 2

// ProjectContextSnapshot is the typed per-session project_context.json sidecar
// (data-model §5.2). RenderedBootContext is restored verbatim on resume so the
// cached prefix is never recompiled from mutable workspace state. The *Hash fields
// are the content hashes of MUHIYA.md / MEMORY.md at boot; the Applied* fields are
// the hashes the engine has already surfaced (so a mid-session edit emits exactly
// one update block).
type ProjectContextSnapshot struct {
	Version                int      `json:"version"`
	WorkspaceKey           string   `json:"workspaceKey"`
	RenderedBootContext    string   `json:"renderedBootContext"`
	InstructionsHash       string   `json:"instructionsHash"`
	InstructionsState      string   `json:"instructionsState"`
	MemoryHash             string   `json:"memoryHash"`
	MemoryState            string   `json:"memoryState"`
	SkillsSnapshot         []string `json:"skillsSnapshot"`
	AppliedInstructionHash string   `json:"appliedInstructionHash"`
	AppliedMemoryHash      string   `json:"appliedMemoryHash"`
}

// ProjectContextProbe is a submit-boundary read of the current workspace
// instructions + memory files, carried across the orchestrator↔state boundary
// using only contract types. The engine compares each file's hash against its
// applied hash to decide whether to emit a one-shot mid-session update block
// (project-context contract §5).
type ProjectContextProbe struct {
	InstructionsHash    string
	InstructionsContent string
	MemoryHash          string
	MemoryContent       string
}
