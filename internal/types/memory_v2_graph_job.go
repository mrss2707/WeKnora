package types

// MemoryGraphJob is a leased row of the module-owned extraction_queue. The
// generation fences acknowledgements from an older version of the same job.
type MemoryGraphJob struct {
	ID          string `gorm:"column:id"`
	TenantID    string `gorm:"column:tenant_id"`
	KbID        string `gorm:"column:kb_id"`
	MemoryID    string `gorm:"column:memory_uuid"`
	Generation  int64  `gorm:"column:generation"`
	LockedBy    string `gorm:"column:locked_by"`
	Attempts    int    `gorm:"column:attempts"`
	MaxAttempts int    `gorm:"column:max_attempts"`
	Status      string `gorm:"column:status"`
}
