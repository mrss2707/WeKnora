package interfaces

import (
	"context"
	"time"
)

// MemoryV2DreamerBudget is an optional, shared budget ledger. Reservations
// survive process restarts; a crashed call conservatively consumes its maximum
// estimate until the UTC day ends. A reservation can be settled only once.
type MemoryV2DreamerBudget interface {
	ReserveDreamerTokens(ctx context.Context, tenantID, reservationID string, day time.Time, tokens, limit int) (bool, error)
	SettleDreamerTokens(ctx context.Context, tenantID, reservationID string, tokens int) error
}
