# Module migration scaffold — copy this directory

This is a template, not a real module. It is never picked up by
`internal/database/migration_source.go`: the example SQL files end in
`.sql.example` (not `.sql`), so the composite migration scanner skips them.

See `docs/_supermeo/CONVENTIONS.md` §1 for the full convention this
scaffold follows. Quick steps to start a new module:

1. Copy this directory: `cp -r migrations/modules/TEMPLATE migrations/
   modules/<your_module_name>`.
2. Inside the copy, rename the example files and drop the `.example`
   suffix, giving them a real version number in the reserved range
   `900000-909999` (pick the next free number — check the other
   `manifest.json` files under `migrations/modules/` for what's taken):
   - `postgres/000000_example.up.sql.example` →
     `postgres/9NNNNN_<your_module_name>.up.sql`
   - `postgres/000000_example.down.sql.example` →
     `postgres/9NNNNN_<your_module_name>.down.sql`
3. Edit the SQL to your real schema. Keep every statement idempotent
   (`CREATE TABLE IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`, `CREATE INDEX
   IF NOT EXISTS`, ...).
4. Copy `manifest.template.json` to `manifest.json` and fill in every
   field — see the comments inside it.
5. Replace this README with one following the structure of
   `migrations/modules/memory_v2/README.md` (version|file|purpose table,
   backend scoping, prerequisites, rollback guidance).
6. Validate before committing:
   ```
   ./scripts/migrate.sh validate postgres
   ./scripts/migrate.sh validate sqlite   # only if your module could ever
                                           # affect SQLite/Lite mode
   ```
   `validate` runs `cmd/migrate_validate`, a pure Go check with no database
   connection required — it catches duplicate versions, missing up/down
   pairs, and out-of-range module versions immediately.

Do not leave a `TEMPLATE/` copy with real `.sql` files in it — always
rename to your module's own directory.
