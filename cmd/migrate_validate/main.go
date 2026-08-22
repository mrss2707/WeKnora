// Command migrate_validate checks that the on-disk migration set assembles
// cleanly for a backend: duplicate version numbers, missing up/down pairs,
// ordering, out-of-range module versions and wrong-backend files all fail
// with a non-zero exit code. Invoked by ./scripts/migrate.sh validate.
package main

import (
	"fmt"
	"os"

	"github.com/Tencent/WeKnora/internal/database"
)

func main() {
	backend := "postgres"
	if len(os.Args) > 1 && os.Args[1] != "" {
		backend = os.Args[1]
	}
	if err := database.ValidateMigrationSet(backend); err != nil {
		fmt.Fprintf(os.Stderr, "migration set validation failed for backend %q: %v\n", backend, err)
		os.Exit(1)
	}
	fmt.Printf("migration set OK for backend %q\n", backend)
}
