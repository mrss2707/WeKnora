package types

// MemoryVerdict tells whether a memory is endorsed, refuted, etc.
type MemoryVerdict string

const (
	VerdictNone     MemoryVerdict = "none"
	VerdictFixed    MemoryVerdict = "fixed"
	VerdictRefuted  MemoryVerdict = "refuted"
	VerdictDecision MemoryVerdict = "decision"
	VerdictGotcha   MemoryVerdict = "gotcha"
	VerdictWIP      MemoryVerdict = "wip"
)

// IsProtected returns true for verdicts that cannot be changed automatically by LLM.
func (v MemoryVerdict) IsProtected() bool {
	return v == VerdictDecision || v == VerdictFixed
}
