package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Tencent/WeKnora/internal/logger"
)

// Names of the per-stream golang-migrate version tables. Core keeps main's
// historical table so a database served by develop stays readable by main.
const (
	coreMigrationsTable   = "schema_migrations"
	moduleMigrationsTable = "schema_migrations_modules"
)

// Highest core migration version that existed in the develop tree when the core
// and module streams were separated. Before the split a single version counter
// held both streams, so a database whose counter had reached the module range
// (>= moduleRangeMin) had necessarily applied every core migration that
// existed at the time, i.e. everything up to these baselines. Never change
// these: they describe historical databases, not the current tree.
const (
	legacyCoreBaselinePostgres uint = 109
	legacyCoreBaselineSQLite   uint = 29
)

func legacyCoreBaseline(backend MigrationBackend) uint {
	if backend == BackendSQLite {
		return legacyCoreBaselineSQLite
	}
	return legacyCoreBaselinePostgres
}

// migrateLegacySingleVersionTable converts a database managed by the former
// single composite stream into the two-stream layout, once, in one transaction.
//
// Legacy state: schema_migrations.version is a module version (>= 900000), which
// golang-migrate treats as "newer than every core file", so core migrations
// added later by main would be skipped silently. Conversion:
//   - schema_migrations_modules receives the legacy version (module progress);
//   - schema_migrations is rewound to the core baseline so the core stream
//     applies whatever main added since.
//
// It is a no-op for fresh databases, for databases already converted
// (schema_migrations_modules exists) and for core-only databases (version below
// the module range). A dirty legacy state is refused: it must be repaired first.
func migrateLegacySingleVersionTable(ctx context.Context, db *sql.DB, backend MigrationBackend) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin legacy migration conversion: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	coreExists, err := tableExists(ctx, tx, backend, coreMigrationsTable)
	if err != nil || !coreExists {
		return err
	}
	if backend == BackendPostgres {
		// Serialise concurrent starters; the loser re-checks below and no-ops.
		if _, err := tx.ExecContext(ctx, `LOCK TABLE `+coreMigrationsTable+` IN EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("lock %s: %w", coreMigrationsTable, err)
		}
	}
	modulesExists, err := tableExists(ctx, tx, backend, moduleMigrationsTable)
	if err != nil || modulesExists {
		return err
	}

	var version int64
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT version, dirty FROM `+coreMigrationsTable+` LIMIT 1`).Scan(&version, &dirty); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("read %s: %w", coreMigrationsTable, err)
	}
	if version < int64(moduleRangeMin) {
		return nil // core-only database: nothing to convert
	}
	if dirty {
		return fmt.Errorf("%w: %s is dirty at legacy version %d; repair it "+
			"(./scripts/migrate.sh force <version>) before upgrading to the two-stream migration layout",
			ErrInvalidMigrationSet, coreMigrationsTable, version)
	}

	baseline := legacyCoreBaseline(backend)
	logger.Infof(ctx, "Converting legacy single-stream migration state (version %d): modules=%d, core rewound to %d",
		version, version, baseline)

	var ddl string
	switch backend {
	case BackendSQLite:
		ddl = `CREATE TABLE IF NOT EXISTS "` + moduleMigrationsTable + `" (version uint64, dirty bool)`
	default:
		ddl = `CREATE TABLE IF NOT EXISTS "` + moduleMigrationsTable + `" (version bigint not null primary key, dirty boolean not null)`
	}
	if _, err := tx.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create %s: %w", moduleMigrationsTable, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO "%s" (version, dirty) VALUES (%d, false)`, moduleMigrationsTable, version)); err != nil {
		return fmt.Errorf("seed %s: %w", moduleMigrationsTable, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE "%s" SET version = %d, dirty = false`, coreMigrationsTable, baseline)); err != nil {
		return fmt.Errorf("rewind %s: %w", coreMigrationsTable, err)
	}
	return tx.Commit()
}

func tableExists(ctx context.Context, tx *sql.Tx, backend MigrationBackend, table string) (bool, error) {
	var n int
	var q string
	if backend == BackendSQLite {
		q = `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`
	} else {
		q = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1`
	}
	if err := tx.QueryRowContext(ctx, q, table).Scan(&n); err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return n > 0, nil
}
