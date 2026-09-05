package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Migration struct {
	Version int
	Name    string
	SQL     string
}

// LoadMigrations reads and parses embedded migration scripts.
func LoadMigrations() ([]Migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		parts := strings.SplitN(entry.Name(), "_", 2)
		if len(parts) < 2 {
			continue
		}

		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("invalid migration version in %s: %w", entry.Name(), err)
		}

		content, err := migrationFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration file %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(content),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// CurrentVersion returns the highest applied migration version.
func CurrentVersion(ctx context.Context, db *DB) (int, error) {
	var exists int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations';").Scan(&exists)
	if err != nil {
		return 0, fmt.Errorf("check schema_migrations table: %w", err)
	}
	if exists == 0 {
		return 0, nil
	}

	var maxVersion sql.NullInt64
	err = db.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations;").Scan(&maxVersion)
	if err != nil {
		return 0, fmt.Errorf("get max version: %w", err)
	}

	if !maxVersion.Valid {
		return 0, nil
	}
	return int(maxVersion.Int64), nil
}

// Migrate applies all pending migrations in sequential order.
func Migrate(ctx context.Context, db *DB) error {
	migrations, err := LoadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	// Ensure migration table exists
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL
	);`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	currentVer, err := CurrentVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("get current migration version: %w", err)
	}

	for _, m := range migrations {
		if m.Version <= currentVer {
			continue
		}

		err = db.WithTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return fmt.Errorf("execute migration %s: %w", m.Name, err)
			}

			_, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?);",
				m.Version, time.Now())
			if err != nil {
				return fmt.Errorf("record migration %d: %w", m.Version, err)
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("apply migration %s (v%d): %w", m.Name, m.Version, err)
		}
	}

	return nil
}
