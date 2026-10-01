package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAssembleStreamSeparatesCoreAndModules(t *testing.T) {
	var roots migrationRoots
	validFixture(t, &roots)

	cases := []struct {
		backend MigrationBackend
		stream  MigrationStream
		want    []uint
	}{
		{BackendPostgres, StreamCore, []uint{1, 2, 3}},
		{BackendPostgres, StreamModules, []uint{900001, 900002}},
		{BackendPostgres, StreamAll, []uint{1, 2, 3, 900001, 900002}},
		{BackendSQLite, StreamCore, []uint{0}},
		{BackendSQLite, StreamModules, []uint{900001}},
		{BackendSQLite, StreamAll, []uint{0, 900001}},
	}
	for _, tc := range cases {
		t.Run(string(tc.backend)+"/"+string(tc.stream), func(t *testing.T) {
			src, err := assembleStream(tc.backend, roots, tc.stream)
			require.NoError(t, err)
			require.Equal(t, tc.want, src.(*compositeSource).Versions())
		})
	}
}

func TestRealLayoutStreamsNeverMix(t *testing.T) {
	chdirAndRestore(t, sqliteRepoRoot(t))
	for _, backend := range []MigrationBackend{BackendPostgres, BackendSQLite} {
		core, err := NewMigrationStreamSource(backend, StreamCore)
		require.NoError(t, err)
		for _, v := range core.(*compositeSource).Versions() {
			require.Less(t, v, moduleRangeMin, "%s core stream must stay below the module range", backend)
		}
		modules, err := NewMigrationStreamSource(backend, StreamModules)
		require.NoError(t, err)
		for _, v := range modules.(*compositeSource).Versions() {
			require.GreaterOrEqual(t, v, moduleRangeMin, "%s module stream must stay inside the module range", backend)
			require.LessOrEqual(t, v, moduleRangeMax)
		}
	}
}

func openLegacyTestDB(t *testing.T, createModules bool, coreVersion int, dirty bool) *sql.DB {
	t.Helper()
	db := openSQLiteDB(t, filepath.Join(t.TempDir(), "legacy.db"))
	_, err := db.Exec(`CREATE TABLE schema_migrations (version uint64, dirty bool)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (?, ?)`, coreVersion, dirty)
	require.NoError(t, err)
	if createModules {
		_, err = db.Exec(`CREATE TABLE schema_migrations_modules (version uint64, dirty bool)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO schema_migrations_modules (version, dirty) VALUES (900001, 0)`)
		require.NoError(t, err)
	}
	return db
}

func versionRow(t *testing.T, db *sql.DB, table string) (int, bool) {
	t.Helper()
	var v int
	var d bool
	require.NoError(t, db.QueryRow(`SELECT version, dirty FROM `+table).Scan(&v, &d))
	return v, d
}

func TestLegacyConversionSplitsSingleCounter(t *testing.T) {
	db := openLegacyTestDB(t, false, 900077, false)

	require.NoError(t, migrateLegacySingleVersionTable(context.Background(), db, BackendSQLite))

	v, d := versionRow(t, db, "schema_migrations_modules")
	require.Equal(t, 900077, v, "module progress must be carried over")
	require.False(t, d)
	v, d = versionRow(t, db, "schema_migrations")
	require.Equal(t, int(legacyCoreBaselineSQLite), v, "core counter must be rewound to the baseline")
	require.False(t, d)

	// Idempotent: a second run (e.g. next restart) changes nothing.
	require.NoError(t, migrateLegacySingleVersionTable(context.Background(), db, BackendSQLite))
	v, _ = versionRow(t, db, "schema_migrations")
	require.Equal(t, int(legacyCoreBaselineSQLite), v)
	v, _ = versionRow(t, db, "schema_migrations_modules")
	require.Equal(t, 900077, v)
}

func TestLegacyConversionNoOps(t *testing.T) {
	ctx := context.Background()

	t.Run("core-only database", func(t *testing.T) {
		db := openLegacyTestDB(t, false, 20, false)
		require.NoError(t, migrateLegacySingleVersionTable(ctx, db, BackendSQLite))
		v, _ := versionRow(t, db, "schema_migrations")
		require.Equal(t, 20, v)
		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'schema_migrations_modules'`).Scan(&n))
		require.Zero(t, n, "no modules table may be created for a core-only database")
	})

	t.Run("already converted", func(t *testing.T) {
		db := openLegacyTestDB(t, true, 33, false)
		require.NoError(t, migrateLegacySingleVersionTable(ctx, db, BackendSQLite))
		v, _ := versionRow(t, db, "schema_migrations")
		require.Equal(t, 33, v)
		v, _ = versionRow(t, db, "schema_migrations_modules")
		require.Equal(t, 900001, v)
	})

	t.Run("fresh database", func(t *testing.T) {
		db := openSQLiteDB(t, filepath.Join(t.TempDir(), "fresh.db"))
		require.NoError(t, migrateLegacySingleVersionTable(ctx, db, BackendSQLite))
	})
}

func TestLegacyConversionRefusesDirtyState(t *testing.T) {
	db := openLegacyTestDB(t, false, 900077, true)

	err := migrateLegacySingleVersionTable(context.Background(), db, BackendSQLite)
	require.ErrorIs(t, err, ErrInvalidMigrationSet)
	require.Contains(t, err.Error(), "dirty")

	v, d := versionRow(t, db, "schema_migrations")
	require.Equal(t, 900077, v, "a refused conversion must leave the state untouched")
	require.True(t, d)
}

func TestSQLiteStartupConvertsLegacyDatabaseAndKeepsStreamsSeparate(t *testing.T) {
	chdirAndRestore(t, sqliteRepoRoot(t))
	dbPath := filepath.Join(t.TempDir(), "streams.db")
	opts := MigrationOptions{SQLiteDBPath: dbPath}

	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", opts))
	db := openSQLiteDB(t, dbPath)
	coreV, _ := versionRow(t, db, "schema_migrations")
	modV, _ := versionRow(t, db, "schema_migrations_modules")
	require.Less(t, coreV, int(moduleRangeMin))
	require.GreaterOrEqual(t, modV, int(moduleRangeMin))

	// A second start is a no-op for both streams.
	require.NoError(t, RunMigrationsWithOptions("sqlite3://unused", opts))
	coreV2, _ := versionRow(t, db, "schema_migrations")
	modV2, _ := versionRow(t, db, "schema_migrations_modules")
	require.Equal(t, coreV, coreV2)
	require.Equal(t, modV, modV2)
}
