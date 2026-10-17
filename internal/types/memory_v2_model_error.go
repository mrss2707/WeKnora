package types

// MemoryV2ModelUnavailable distinguishes upstream model failures from storage
// failures. HTTP handlers return a fixed message, never provider URLs or keys.
type MemoryV2ModelUnavailable struct{ Cause error }

func (e *MemoryV2ModelUnavailable) Error() string {
	if e.Cause == nil {
		return "memory embedding model unavailable"
	}
	return "memory embedding model unavailable: " + e.Cause.Error()
}
func (e *MemoryV2ModelUnavailable) Unwrap() error { return e.Cause }
