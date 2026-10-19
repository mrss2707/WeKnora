package types

// MemoryHealthCoverage describes a bounded scan, not a semantic health guarantee.
type MemoryHealthCoverage struct {
	Total    int64    `json:"total"`
	Scanned  int      `json:"scanned"`
	Limit    int      `json:"limit"`
	Complete bool     `json:"complete"`
	Reasons  []string `json:"reasons"`
}

// MemoryHealthCheck makes unsupported or unavailable checks visible to callers.
type MemoryHealthCheck struct {
	Status   string `json:"status"` // completed | partial | unavailable
	Reason   string `json:"reason,omitempty"`
	Examined int    `json:"examined"`
}
