package database

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4/source"
)

// MigrationBackend identifies which database backend a migration set targets.
type MigrationBackend string

const (
	// BackendPostgres assembles core migrations/versioned plus every
	// migrations/modules/<name>/postgres module directory.
	BackendPostgres MigrationBackend = "postgres"
	// BackendSQLite assembles only migrations/sqlite — PostgreSQL SQL must
	// never reach SQLite/Lite mode.
	BackendSQLite MigrationBackend = "sqlite"
)

// Default directory layout (relative to the application working directory,
// mirroring the historical "file://migrations/versioned" convention).
const (
	versionedMigrationsDir = "migrations/versioned"
	moduleMigrationsDir    = "migrations/modules"
	sqliteMigrationsDir    = "migrations/sqlite"
)

// Reserved module range. Core migrations must stay below it; module
// migrations must stay inside it, so the two streams can never collide,
// even when upstream renumbers core files.
const (
	moduleRangeMin = uint(900000)
	moduleRangeMax = uint(909999)
)

// ErrInvalidMigrationSet is the fail-closed sentinel: assembling the source
// failed (bad name, duplicate version, out-of-range module file, missing
// pair). Application startup treats this error as fatal because the schema
// source itself cannot be trusted. Runtime errors from running m.Up() are
// unrelated and keep the historical warn-and-continue behaviour.
var ErrInvalidMigrationSet = errors.New("invalid migration set")

// migrationFileRe matches "NNNNNN_name.{up,down}.sql".
var migrationFileRe = regexp.MustCompile(`^([0-9]{6})_(.+)\.(up|down)\.sql$`)

// ParseMigrationBackend maps a user-provided backend name to a MigrationBackend.
func ParseMigrationBackend(name string) (MigrationBackend, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pg":
		return BackendPostgres, nil
	case "sqlite", "sqlite3":
		return BackendSQLite, nil
	default:
		return "", fmt.Errorf("unsupported migration backend %q (want postgres or sqlite)", name)
	}
}

// compositeVersion is one fully-paired migration version.
type compositeVersion struct {
	version uint
	up      string // relative path, also used as the identifier
	down    string // relative path, also used as the identifier
}

// compositeSource is an in-memory source.Driver implementation. It is built
// once from disk (validating the set up front) and injected through
// migrate.NewWithSourceInstance, so no URL scheme registration or temporary
// directory is needed.
type compositeSource struct {
	versions []compositeVersion
	index    map[uint]compositeVersion
	name     string
}

// migrationRoots makes the layout injectable for tests.
type migrationRoots struct {
	versioned string
	modules   string
	sqlite    string
}

func defaultMigrationRoots() migrationRoots {
	return migrationRoots{
		versioned: versionedMigrationsDir,
		modules:   moduleMigrationsDir,
		sqlite:    sqliteMigrationsDir,
	}
}

// NewCompositeMigrationSource assembles the migration source for a backend
// using the default repository layout.
func NewCompositeMigrationSource(backend MigrationBackend) (source.Driver, error) {
	return assembleSource(backend, defaultMigrationRoots())
}

// ValidateMigrationSet assembles the set for the given backend and reports
// every problem found (duplicates, missing pairs, ordering, out-of-range
// module files, wrong backend). It returns nil when the set is valid. Used
// by `./scripts/migrate.sh validate`.
func ValidateMigrationSet(backend string) error {
	b, err := ParseMigrationBackend(backend)
	if err != nil {
		return err
	}
	_, err = assembleSource(b, defaultMigrationRoots())
	return err
}

// assembleSource walks the layout for the backend and returns an ordered,
// duplicate-rejecting source, or ErrInvalidMigrationSet with all problems.
func assembleSource(backend MigrationBackend, roots migrationRoots) (source.Driver, error) {
	files := make(map[uint]compositeVersion)
	var problems []string

	addFile := func(version uint, direction, path, zone string) {
		cur := files[version]
		if direction == "up" {
			if cur.up != "" {
				problems = append(problems, fmt.Sprintf(
					"duplicate version %d (%s): both %q and %q provide the up migration",
					version, zone, cur.up, path))
				return
			}
			cur.up = path
			cur.version = version
		} else {
			if cur.down != "" {
				problems = append(problems, fmt.Sprintf(
					"duplicate version %d (%s): both %q and %q provide the down migration",
					version, zone, cur.down, path))
				return
			}
			cur.down = path
			cur.version = version
		}
		files[version] = cur
	}

	scanDir := func(dir, zone string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			problems = append(problems, fmt.Sprintf("cannot read %s migration dir %q: %v", zone, dir, err))
			return
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
				continue
			}
			rel := filepath.Join(dir, e.Name())
			m := migrationFileRe.FindStringSubmatch(e.Name())
			if m == nil {
				problems = append(problems, fmt.Sprintf(
					"invalid migration file name %q in %s: want NNNNNN_name.up.sql / NNNNNN_name.down.sql", rel, zone))
				continue
			}
			version, err := strconv.ParseUint(m[1], 10, 32)
			if err != nil {
				problems = append(problems, fmt.Sprintf("invalid migration version in %q: %v", rel, err))
				continue
			}
			switch zone {
			case "core":
				if version >= uint64(moduleRangeMin) {
					problems = append(problems, fmt.Sprintf(
						"core migration %q uses version %d, which is reserved for modules (%d-%d); move it under migrations/modules/<name>/postgres or choose a lower number",
						rel, version, moduleRangeMin, moduleRangeMax))
					continue
				}
			case "module":
				if version < uint64(moduleRangeMin) || version > uint64(moduleRangeMax) {
					problems = append(problems, fmt.Sprintf(
						"module migration %q uses version %d, outside the reserved module range %d-%d",
						rel, version, moduleRangeMin, moduleRangeMax))
					continue
				}
			case "sqlite":
				if version >= uint64(moduleRangeMin) {
					problems = append(problems, fmt.Sprintf(
						"sqlite migration %q uses version %d from the module range (%d-%d); PostgreSQL module SQL must never enter the sqlite stream",
						rel, version, moduleRangeMin, moduleRangeMax))
					continue
				}
			}
			addFile(uint(version), m[3], rel, zone)
		}
	}

	scanModules := func() {
		entries, err := os.ReadDir(roots.modules)
		if err != nil {
			if os.IsNotExist(err) {
				return // no modules yet — nothing to assemble
			}
			problems = append(problems, fmt.Sprintf("cannot read module migration dir %q: %v", roots.modules, err))
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			pgDir := filepath.Join(roots.modules, e.Name(), string(BackendPostgres))
			if _, err := os.Stat(pgDir); err != nil {
				continue // module without a postgres dir is simply not applicable to this backend
			}
			scanDir(pgDir, "module")
		}
	}

	switch backend {
	case BackendPostgres:
		scanDir(roots.versioned, "core")
		scanModules()
	case BackendSQLite:
		scanDir(roots.sqlite, "sqlite")
	default:
		problems = append(problems, fmt.Sprintf("unsupported migration backend %q", backend))
	}

	if len(problems) == 0 {
		for version, f := range files {
			if f.up == "" {
				problems = append(problems, fmt.Sprintf("version %d has only a down migration (%s): up/down pairs are required", version, f.down))
			}
			if f.down == "" {
				problems = append(problems, fmt.Sprintf("version %d has only an up migration (%s): up/down pairs are required", version, f.up))
			}
		}
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMigrationSet, strings.Join(problems, "; "))
	}

	versions := make([]compositeVersion, 0, len(files))
	index := make(map[uint]compositeVersion, len(files))
	for version, f := range files {
		versions = append(versions, f)
		index[version] = f
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].version < versions[j].version })

	return &compositeSource{versions: versions, index: index, name: string(backend)}, nil
}

// Open is part of source.Driver. The composite source is built in memory and
// injected via NewWithSourceInstance, so Open must never be called.
func (d *compositeSource) Open(string) (source.Driver, error) {
	return nil, errors.New("Open() cannot be called on the composite in-memory driver; use NewCompositeMigrationSource")
}

// Close is part of source.Driver. The composite source holds no file handles
// (files are opened lazily per read).
func (d *compositeSource) Close() error { return nil }

// First returns the lowest migration version.
func (d *compositeSource) First() (version uint, err error) {
	if len(d.versions) == 0 {
		return 0, notExistError("first", d.name)
	}
	return d.versions[0].version, nil
}

// Prev returns the immediately lower version than the given one.
func (d *compositeSource) Prev(version uint) (prevVersion uint, err error) {
	for i, v := range d.versions {
		if v.version == version {
			if i == 0 {
				return 0, notExistError("prev for version "+strconv.FormatUint(uint64(version), 10), d.name)
			}
			return d.versions[i-1].version, nil
		}
		if v.version > version {
			break
		}
	}
	return 0, notExistError("prev for version "+strconv.FormatUint(uint64(version), 10), d.name)
}

// Next returns the immediately higher version than the given one.
func (d *compositeSource) Next(version uint) (nextVersion uint, err error) {
	for i, v := range d.versions {
		if v.version == version {
			if i == len(d.versions)-1 {
				return 0, notExistError("next for version "+strconv.FormatUint(uint64(version), 10), d.name)
			}
			return d.versions[i+1].version, nil
		}
		if v.version > version {
			// Unknown-but-in-range version: still "not exist" for the runner.
			return 0, notExistError("next for version "+strconv.FormatUint(uint64(version), 10), d.name)
		}
	}
	return 0, notExistError("next for version "+strconv.FormatUint(uint64(version), 10), d.name)
}

// ReadUp lazily opens the up migration and returns its path as identifier.
func (d *compositeSource) ReadUp(version uint) (r io.ReadCloser, identifier string, err error) {
	f, ok := d.index[version]
	if !ok || f.up == "" {
		return nil, "", notExistError("read up for version "+strconv.FormatUint(uint64(version), 10), d.name)
	}
	body, err := os.Open(f.up)
	if err != nil {
		return nil, "", err
	}
	return body, f.up, nil
}

// ReadDown lazily opens the down migration and returns its path as identifier.
func (d *compositeSource) ReadDown(version uint) (r io.ReadCloser, identifier string, err error) {
	f, ok := d.index[version]
	if !ok || f.down == "" {
		return nil, "", notExistError("read down for version "+strconv.FormatUint(uint64(version), 10), d.name)
	}
	body, err := os.Open(f.down)
	if err != nil {
		return nil, "", err
	}
	return body, f.down, nil
}

// notExistError mirrors the *fs.PathError contract used by the iofs driver so
// the migrate runner's errors.Is(err, os.ErrNotExist) checks keep working.
func notExistError(op, path string) error {
	return &fs.PathError{Op: op, Path: path, Err: fs.ErrNotExist}
}

// Versions returns the ordered, paired versions for inspection (used by tests
// and by the validate command reporting).
func (d *compositeSource) Versions() []uint {
	out := make([]uint, len(d.versions))
	for i, v := range d.versions {
		out[i] = v.version
	}
	return out
}