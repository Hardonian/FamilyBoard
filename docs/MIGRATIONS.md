# FamilyBoard Database Migration Architecture & Compatibility

FamilyBoard stores all appliance state in SQLite with WAL (Write-Ahead Logging) enabled.

## 1. Schema Versioning & Forward Migration

Schema migrations are located in `internal/db/migrations/*.sql` and embedded into the application binary at build time.

Migration files follow strict numeric sequential naming:
* `001_initial_schema.sql` (Core identity, oauth, calendars, events, queue)
* `002_indexes.sql` (Performance indices for calendar range queries)
* `003_kiosk_enhancements.sql` (Kiosk preferences, quick templates, day/night schedules)
* `004_retention_and_indexes.sql` (Retention indices, conflict resolution records)
* `005_product_security.sql` (Device pairing tokens, capability grants, privacy exports)

### Invariants for Migrations:
1. **Additive and Non-Destructive**: Migrations MUST NOT drop tables or columns in production upgrades.
2. **Idempotent Statements**: Use `CREATE TABLE IF NOT EXISTS` and `CREATE INDEX IF NOT EXISTS`.
3. **Atomic Migration Execution**: Each migration step executes inside an explicit database transaction (`BEGIN IMMEDIATE ... COMMIT`). If an error occurs, the transaction rolls back cleanly, leaving the database at the prior valid version.
4. **Automated Tracking**: The `schema_migrations` table tracks the version and timestamp of every applied migration.

## 2. Rollback Strategy

SQLite does not support `ALTER TABLE DROP COLUMN` consistently across historical versions or undoing arbitrary schema scripts reliably in an embedded appliance context.

Therefore, FamilyBoard enforces an **automated backup before migration** policy:
1. Prior to applying migrations during an update, `deploy/pi/update.sh` and the internal updater create a point-in-time snapshot of the database:
   ```
   backups/pre-upgrade-$(date +%Y%m%d%H%M%S).db
   ```
2. If schema migration fails or post-upgrade health checks fail, the previous application binary and database snapshot are restored automatically.
3. This guarantees zero data loss or partial schema corruption on unexpected power loss or failure during an upgrade.
