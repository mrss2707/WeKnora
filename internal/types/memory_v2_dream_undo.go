package types

// DreamRunKey is the context key carrying the id of the Dreamer pass that
// makes a change; the audit entries of that pass record it as
// details.dream_run so the pass can be undone as a whole.
type DreamRunKey struct{}

// DreamUndoKey is the context key carrying the id of the Dreamer pass being
// undone; the reverting audit entries record it as details.undo_dream_run.
type DreamUndoKey struct{}

// DreamRunFromContext returns the Dreamer pass id of ctx, if any.
func DreamRunFromContext(ctx interface{ Value(any) any }) string {
	id, _ := ctx.Value(DreamRunKey{}).(string)
	return id
}

// Reasons a change of a Dreamer pass is left in place by an undo.
const (
	DreamUndoSkipChanged     = "changed_after_pass"
	DreamUndoSkipAlreadyDone = "already_undone"
	DreamUndoSkipUnsupported = "not_reversible"
	DreamUndoSkipFailed      = "failed"
)

// DreamUndoSkip is one change of a pass that was not reverted.
type DreamUndoSkip struct {
	MemoryID string `json:"memory_id"`
	Action   string `json:"action"`
	Reason   string `json:"reason"`
}

// DreamUndoResult reports what undoing a Dreamer pass reverted.
type DreamUndoResult struct {
	RunID    string          `json:"run_id"`
	Reverted int             `json:"reverted"`
	Skipped  []DreamUndoSkip `json:"skipped"`
}
