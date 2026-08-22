package database

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// buildFixture creates a temp migration layout with the given relative file
// paths (each written with content "<path>"). Returns the roots.
func buildFixture(t *testing.T, files map[string]string) (migrationRoots, string) {
	t.Helper()
	base := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(base, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return migrationRoots{
		versioned: filepath.Join(base, "versioned"),
		modules:   filepath.Join(base, "modules"),
		sqlite:    filepath.Join(base, "sqlite"),
	}, base
}

func readAll(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(b)
}

// validFixture returns a well-formed postgres+sqlite fixture:
// core 1..3, module alpha 900001-900002, sqlite 0.
func validFixture(t *testing.T, roots *migrationRoots) {
	testFiles := map[string]string{
		"versioned/000001_init.up.sql":                    "core up 1",
		"versioned/000001_init.down.sql":                  "core down 1",
		"versioned/000002_agent.up.sql":                   "core up 2",
		"versioned/000002_agent.down.sql":                 "core down 2",
		"versioned/000003_docs.up.sql":                    "core up 3",
		"versioned/000003_docs.down.sql":                  "core down 3",
		"versioned/README.md":                             "not a migration",
		"modules/alpha/postgres/900001_alpha.up.sql":      "mod up 900001",
		"modules/alpha/postgres/900001_alpha.down.sql":    "mod down 900001",
		"modules/alpha/postgres/900002_beta.up.sql":       "mod up 900002",
		"modules/alpha/postgres/900002_beta.down.sql":     "mod down 900002",
		"modules/alpha/sqlite/000001_alpha_lite.up.sql":   "must be ignored for postgres",
		"modules/alpha/sqlite/000001_alpha_lite.down.sql": "must be ignored for postgres",
		"sqlite/000000_init.up.sql":                       "sqlite up 0",
		"sqlite/000000_init.down.sql":                     "sqlite down 0",
	}
	*roots, _ = buildFixture(t, testFiles)
}

func TestCompositeSourcePostgresOrderAndReads(t *testing.T) {
	var roots migrationRoots
	validFixture(t, &roots)

	src, err := assembleSource(BackendPostgres, roots)
	if err != nil {
		t.Fatalf("assemble postgres: %v", err)
	}
	cs := src.(*compositeSource)
	got := cs.Versions()
	want := []uint{1, 2, 3, 900001, 900002}
	if len(got) != len(want) {
		t.Fatalf("versions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("versions = %v, want %v", got, want)
		}
	}

	// First / Next / Prev walk across the core→module boundary.
	v, err := src.First()
	if err != nil || v != 1 {
		t.Fatalf("First() = %d, %v; want 1, nil", v, err)
	}
	for i := 0; i < len(want)-1; i++ {
		n, err := src.Next(want[i])
		if err != nil || n != want[i+1] {
			t.Fatalf("Next(%d) = %d, %v; want %d", want[i], n, err, want[i+1])
		}
	}
	if _, err := src.Next(want[len(want)-1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Next(last) err = %v, want os.ErrNotExist", err)
	}
	for i := len(want) - 1; i > 0; i-- {
		p, err := src.Prev(want[i])
		if err != nil || p != want[i-1] {
			t.Fatalf("Prev(%d) = %d, %v; want %d", want[i], p, err, want[i-1])
		}
	}
	if _, err := src.Prev(want[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Prev(first) err = %v, want os.ErrNotExist", err)
	}
	if _, err := src.Prev(424242); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Prev(unknown) err = %v, want os.ErrNotExist", err)
	}
	if _, err := src.Next(424242); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Next(unknown) err = %v, want os.ErrNotExist", err)
	}

	// ReadUp/ReadDown are lazy and identifier == relative path.
	up, id, err := src.ReadUp(900001)
	if err != nil {
		t.Fatalf("ReadUp(900001): %v", err)
	}
	if got := readAll(t, up); got != "mod up 900001" {
		t.Fatalf("ReadUp content = %q", got)
	}
	if !strings.HasSuffix(id, filepath.Join("modules", "alpha", "postgres", "900001_alpha.up.sql")) {
		t.Fatalf("identifier = %q, want module path", id)
	}
	if _, _, err := src.ReadUp(7); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadUp(unknown) err = %v, want os.ErrNotExist", err)
	}
	if _, _, err := src.ReadDown(999999); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ReadDown(unknown) err = %v, want os.ErrNotExist", err)
	}
}

func TestCompositeSourceRejectsDuplicateVersions(t *testing.T) {
	roots, _ := buildFixture(t, map[string]string{
		"versioned/000002_a.up.sql":            "a",
		"versioned/000002_a.down.sql":          "a d",
		"versioned/000002_dup.up.sql":          "dup",
		"versioned/000002_dup.down.sql":        "dup d",
		"modules/m/postgres/900010_m.up.sql":   "m",
		"modules/m/postgres/900010_m.down.sql": "m d",
		"modules/n/postgres/900010_m.up.sql":   "cross-module collision",
		"modules/n/postgres/900010_m.down.sql": "cross-module collision d",
		"sqlite/000000_init.up.sql":            "s",
		"sqlite/000000_init.down.sql":          "s d",
	})
	_, err := assembleSource(BackendPostgres, roots)
	if !errors.Is(err, ErrInvalidMigrationSet) {
		t.Fatalf("err = %v, want ErrInvalidMigrationSet", err)
	}
	msg := err.Error()
	for _, want := range []string{"900010", "modules/m/postgres", "modules/n/postgres", "000002_dup"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestCompositeSourceRejectsMissingPair(t *testing.T) {
	roots, _ := buildFixture(t, map[string]string{
		"versioned/000001_init.up.sql": "up only",
		"sqlite/000000_init.up.sql":    "s",
		"sqlite/000000_init.down.sql":  "s d",
	})
	_, err := assembleSource(BackendPostgres, roots)
	if !errors.Is(err, ErrInvalidMigrationSet) {
		t.Fatalf("err = %v, want ErrInvalidMigrationSet", err)
	}
	if !strings.Contains(err.Error(), "up/down pairs are required") {
		t.Fatalf("error %q does not mention missing pair", err)
	}
}

func TestCompositeSourceRejectsBadName(t *testing.T) {
	roots, _ := buildFixture(t, map[string]string{
		"versioned/002_bad_digits.up.sql": "bad", // only 3 digits
	})
	_, err := assembleSource(BackendPostgres, roots)
	if !errors.Is(err, ErrInvalidMigrationSet) {
		t.Fatalf("err = %v, want ErrInvalidMigrationSet", err)
	}
	if !strings.Contains(err.Error(), "invalid migration file name") {
		t.Fatalf("error %q does not mention invalid name", err)
	}
}

func TestCompositeSourceRejectsCoreBeyondReservedRange(t *testing.T) {
	roots, _ := buildFixture(t, map[string]string{
		"versioned/900001_core.up.sql":   "bad",
		"versioned/900001_core.down.sql": "bad",
	})
	_, err := assembleSource(BackendPostgres, roots)
	if !errors.Is(err, ErrInvalidMigrationSet) {
		t.Fatalf("err = %v, want ErrInvalidMigrationSet", err)
	}
	if !strings.Contains(err.Error(), "reserved for modules") {
		t.Fatalf("error %q does not mention reserved range", err)
	}
}

func TestCompositeSourceRejectsModuleOutsideReservedRange(t *testing.T) {
	for _, version := range []string{"000005", "910000"} {
		roots, _ := buildFixture(t, map[string]string{
			"modules/m/postgres/" + version + "_m.up.sql":   "bad",
			"modules/m/postgres/" + version + "_m.down.sql": "bad",
		})
		_, err := assembleSource(BackendPostgres, roots)
		if !errors.Is(err, ErrInvalidMigrationSet) {
			t.Fatalf("version %s: err = %v, want ErrInvalidMigrationSet", version, err)
		}
		if !strings.Contains(err.Error(), "reserved module range") {
			t.Fatalf("version %s: error %q does not mention module range", version, err)
		}
	}
}

func TestCompositeSourceSQLiteExcludesModules(t *testing.T) {
	var roots migrationRoots
	validFixture(t, &roots)

	src, err := assembleSource(BackendSQLite, roots)
	if err != nil {
		t.Fatalf("assemble sqlite: %v", err)
	}
	cs := src.(*compositeSource)
	got := cs.Versions()
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("sqlite versions = %v, want [0] only", got)
	}
	if _, _, err := src.ReadUp(900001); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sqlite ReadUp(module version) err = %v, want os.ErrNotExist", err)
	}
}

func TestCompositeSourceSQLiteRejectsModuleRangeFiles(t *testing.T) {
	roots, _ := buildFixture(t, map[string]string{
		"sqlite/900074_memory_v2.up.sql":   "PostgreSQL SQL must never enter the sqlite stream",
		"sqlite/900074_memory_v2.down.sql": "down",
	})
	_, err := assembleSource(BackendSQLite, roots)
	if !errors.Is(err, ErrInvalidMigrationSet) {
		t.Fatalf("err = %v, want ErrInvalidMigrationSet", err)
	}
	if !strings.Contains(err.Error(), "must never enter the sqlite stream") {
		t.Fatalf("error %q does not explain the sqlite stream violation", err)
	}
}

func TestParseMigrationBackend(t *testing.T) {
	cases := map[string]MigrationBackend{
		"postgres":   BackendPostgres,
		"PostgreSQL": BackendPostgres,
		"sqlite":     BackendSQLite,
		"sqlite3":    BackendSQLite,
	}
	for in, want := range cases {
		got, err := ParseMigrationBackend(in)
		if err != nil || got != want {
			t.Errorf("ParseMigrationBackend(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseMigrationBackend("mysql"); err == nil {
		t.Errorf("ParseMigrationBackend(mysql) err = nil, want error")
	}
}

// repoRoot returns the repository root by walking up from this test file so
// the real-layout tests work regardless of CWD.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "migrations", "versioned")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}
}

// TestRealRepositoryLayoutAssembles guards the repository's actual migration
// set: postgres must assemble cleanly and carry the module range; sqlite must
// assemble cleanly and exclude it.
func TestRealRepositoryLayoutAssembles(t *testing.T) {
	root := repoRoot(t)
	roots := migrationRoots{
		versioned: filepath.Join(root, versionedMigrationsDir),
		modules:   filepath.Join(root, moduleMigrationsDir),
		sqlite:    filepath.Join(root, sqliteMigrationsDir),
	}

	src, err := assembleSource(BackendPostgres, roots)
	if err != nil {
		t.Fatalf("real postgres set invalid: %v", err)
	}
	versions := src.(*compositeSource).Versions()
	hasModule := false
	seen := map[uint]bool{}
	var max uint
	for _, v := range versions {
		if v >= moduleRangeMin {
			hasModule = true
		}
		if seen[v] {
			t.Fatalf("duplicate version %d in real set", v)
		}
		seen[v] = true
		if v > max {
			max = v
		}
	}
	if !hasModule {
		t.Fatal("real postgres set has no module-range (900073-900076) migrations")
	}
	if max < 900076 {
		t.Fatalf("real postgres set max version = %d, want >= 900076", max)
	}

	sqliteSrc, err := assembleSource(BackendSQLite, roots)
	if err != nil {
		t.Fatalf("real sqlite set invalid: %v", err)
	}
	if migrations := sqliteSrc.(*compositeSource).Versions(); len(migrations) == 0 {
		t.Fatal("real sqlite set is empty")
	}
}

// The module range is the merge-isolation anchor: the four relocated
// migrations must stay present in the postgres stream and never enter the
// sqlite stream, no matter how upstream renumbers the core files. This table
// is the invariant that survives the main merge.
var realLayoutInvariantTable = []struct {
	name      string
	presentIn []uint // must all exist in the postgres stream
	absentIn  []uint // must not exist in the postgres stream
	check     func(t *testing.T, pg, lite []uint)
}{
	{
		name:      "module versions permanently in postgres",
		presentIn: []uint{900073, 900074, 900075, 900076},
	},
	{
		name:     "core stream never crosses into the module range",
		absentIn: []uint{900000, 900072, 909999},
		check: func(t *testing.T, pg, lite []uint) {
			for _, v := range pg {
				if v >= moduleRangeMin && v <= moduleRangeMax {
					continue // legit module zone
				}
				if v > moduleRangeMax {
					t.Fatalf("postgres version %d exceeds the reserved module range", v)
				}
			}
		},
	},
	{
		name: "postgres stream strictly ascending",
		check: func(t *testing.T, pg, lite []uint) {
			for i := 1; i < len(pg); i++ {
				if pg[i] <= pg[i-1] {
					t.Fatalf("postgres versions not strictly ascending at %d: %d then %d", i, pg[i-1], pg[i])
				}
			}
		},
	},
	{
		name: "sqlite stream carries no module SQL",
		check: func(t *testing.T, pg, lite []uint) {
			for _, v := range lite {
				if v >= moduleRangeMin {
					t.Fatalf("sqlite version %d leaks module-range SQL into Lite mode", v)
				}
			}
		},
	},
}

// TestRealLayoutInvariantTable drives the merge-safe invariant table against
// the repository's actual migration files: the module anchors stay in
// postgres, nothing crosses the reserved range, ordering holds, and Lite mode
// never receives module SQL.
func TestRealLayoutInvariantTable(t *testing.T) {
	root := repoRoot(t)
	roots := migrationRoots{
		versioned: filepath.Join(root, versionedMigrationsDir),
		modules:   filepath.Join(root, moduleMigrationsDir),
		sqlite:    filepath.Join(root, sqliteMigrationsDir),
	}

	pgSrc, err := assembleSource(BackendPostgres, roots)
	if err != nil {
		t.Fatalf("real postgres set invalid: %v", err)
	}
	liteSrc, err := assembleSource(BackendSQLite, roots)
	if err != nil {
		t.Fatalf("real sqlite set invalid: %v", err)
	}
	pg := pgSrc.(*compositeSource).Versions()
	lite := liteSrc.(*compositeSource).Versions()

	for _, tc := range realLayoutInvariantTable {
		t.Run(tc.name, func(t *testing.T) {
			pgSet := map[uint]bool{}
			for _, v := range pg {
				pgSet[v] = true
			}
			for _, want := range tc.presentIn {
				if !pgSet[want] {
					t.Fatalf("postgres stream missing anchor version %d", want)
				}
			}
			for _, banned := range tc.absentIn {
				if pgSet[banned] {
					t.Fatalf("postgres stream unexpectedly contains version %d", banned)
				}
			}
			if tc.check != nil {
				tc.check(t, pg, lite)
			}
		})
	}
}
