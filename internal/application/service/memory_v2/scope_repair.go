package memory_v2

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// KBScopeRepairMode controls the startup repair of memories stored under the
// wrong tenant (see interfaces.MemoryV2KBScopeRepository).
type KBScopeRepairMode string

const (
	KBScopeRepairOn    KBScopeRepairMode = "on"    // detect and migrate (default)
	KBScopeRepairAudit KBScopeRepairMode = "audit" // detect and log only
	KBScopeRepairOff   KBScopeRepairMode = "off"   // do nothing
)

// KBScopeRepairModeFromEnv reads MEMORY_V2_KB_SCOPE_REPAIR; unknown values
// fall back to audit-only (detect and log, never mutate).
func KBScopeRepairModeFromEnv() KBScopeRepairMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MEMORY_V2_KB_SCOPE_REPAIR"))) {
	case "", "on", "true", "1":
		return KBScopeRepairOn
	case "off", "false", "0":
		return KBScopeRepairOff
	default:
		return KBScopeRepairAudit
	}
}

// KBScopeRepairReport summarizes one detection/repair pass.
type KBScopeRepairReport struct {
	Detected   int
	Moved      int
	Failed     int
	Relations  int64
	Dropped    int64
	Tenants    map[string]struct{}
	Migrations map[string]int // "old->new" tenant pair -> rows
}

const kbScopeRepairBatch = 500

// RepairKBTenantScope detects memories whose tenant differs from their
// knowledge base owner's tenant and, unless audit-only, moves them there. It is
// idempotent: once repaired a row no longer matches the detection query.
func RepairKBTenantScope(
	ctx context.Context,
	repo interfaces.MemoryRepositoryV2,
	mode KBScopeRepairMode,
) (*KBScopeRepairReport, error) {
	report := &KBScopeRepairReport{Tenants: map[string]struct{}{}, Migrations: map[string]int{}}
	if mode == KBScopeRepairOff {
		return report, nil
	}
	scoped, ok := repo.(interfaces.MemoryV2KBScopeRepository)
	if !ok {
		return report, nil
	}

	if mode == KBScopeRepairAudit {
		all, err := scoped.ListKBScopeMismatches(ctx, 0)
		if err != nil {
			return report, fmt.Errorf("detect kb scope mismatches: %w", err)
		}
		for _, m := range all {
			report.Detected++
			report.Migrations[m.CurrentTenant+"->"+m.OwnerTenant]++
		}
		return report, nil
	}

	var movedIDs []string
	for {
		batch, err := scoped.ListKBScopeMismatches(ctx, kbScopeRepairBatch)
		if err != nil {
			return report, fmt.Errorf("detect kb scope mismatches: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		progressed := false
		for _, m := range batch {
			report.Detected++
			fp := computeScopedFingerprint(m.OwnerTenant, m.KbID, m.Content)
			if err := scoped.MoveToKBOwnerTenant(ctx, m, fp); err != nil {
				report.Failed++
				logger.Errorf(ctx, "[MemoryV2] scope repair failed for memory %s: %v", m.MemoryID, err)
				continue
			}
			progressed = true
			report.Moved++
			movedIDs = append(movedIDs, m.MemoryID)
			report.Tenants[m.CurrentTenant] = struct{}{}
			report.Tenants[m.OwnerTenant] = struct{}{}
			report.Migrations[m.CurrentTenant+"->"+m.OwnerTenant]++
		}
		if !progressed {
			break // every row in the batch failed; do not spin
		}
	}

	if len(movedIDs) > 0 {
		for start := 0; start < len(movedIDs); start += kbScopeRepairBatch {
			end := start + kbScopeRepairBatch
			if end > len(movedIDs) {
				end = len(movedIDs)
			}
			re, dr, err := scoped.SyncRelationTenants(ctx, movedIDs[start:end])
			if err != nil {
				return report, fmt.Errorf("sync relation tenants: %w", err)
			}
			report.Relations += re
			report.Dropped += dr
		}
		for tenant := range report.Tenants {
			repo.InvalidateResultCache(ctx, tenant)
		}
	}
	return report, nil
}
