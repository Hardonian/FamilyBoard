package db

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	path string
}

// Open opens or creates a SQLite database with production appliance PRAGMAs.
func Open(dbPath string, busyTimeoutMs, maxOpenConns, maxIdleConns int) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("create db directory %s: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)",
		dbPath, busyTimeoutMs)

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if maxOpenConns <= 0 {
		maxOpenConns = 4
	}
	if maxIdleConns <= 0 {
		maxIdleConns = 2
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Hour)

	db := &DB{DB: sqlDB, path: dbPath}

	// Apply hardware/microSD-optimized PRAGMAs
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d;", busyTimeoutMs),
		"PRAGMA foreign_keys = ON;",
		"PRAGMA journal_mode = WAL;",
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",       // Memory-backed temporary tables and indices (0 disk writes)
		"PRAGMA cache_size = -8000;",        // ~8MB memory cache
		"PRAGMA mmap_size = 33554432;",      // 32MB memory mapped I/O
		"PRAGMA wal_autocheckpoint = 1000;", // Bounded WAL size
	}

	for _, pragma := range pragmas {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("execute %s: %w", pragma, err)
		}
	}

	// Quick integrity check on startup
	if err := db.QuickCheck(); err != nil {
		sqlDB.Close()
		quarantinePath, qErr := db.QuarantineCorrupt()
		if qErr != nil {
			return nil, fmt.Errorf("database corrupted (%v) and quarantine failed: %w", err, qErr)
		}
		return nil, fmt.Errorf("database corrupted (%v); preserved copy at %s", err, quarantinePath)
	}

	return db, nil
}

// Path returns the filesystem path of the database.
func (db *DB) Path() string {
	return db.path
}

// CheckIntegrity executes PRAGMA integrity_check.
func (db *DB) CheckIntegrity() error {
	var result string
	row := db.QueryRow("PRAGMA integrity_check;")
	if err := row.Scan(&result); err != nil {
		return fmt.Errorf("run integrity_check: %w", err)
	}
	if strings.ToLower(result) != "ok" {
		return fmt.Errorf("integrity_check reported: %s", result)
	}
	return nil
}

// QuickCheck executes PRAGMA quick_check.
func (db *DB) QuickCheck() error {
	var result string
	row := db.QueryRow("PRAGMA quick_check;")
	if err := row.Scan(&result); err != nil {
		return fmt.Errorf("run quick_check: %w", err)
	}
	if strings.ToLower(result) != "ok" {
		return fmt.Errorf("quick_check reported: %s", result)
	}
	return nil
}

// QuarantineCorrupt preserves the corrupted database and associated WAL files without deleting them.
func (db *DB) QuarantineCorrupt() (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	quarantinePath := fmt.Sprintf("%s.corrupt-%s", db.path, timestamp)

	copyFile := func(src, dst string) error {
		in, err := os.Open(src)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		defer in.Close()

		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()

		_, err = io.Copy(out, in)
		return err
	}

	if err := copyFile(db.path, quarantinePath); err != nil {
		return "", fmt.Errorf("copy corrupt db: %w", err)
	}
	_ = copyFile(db.path+"-wal", quarantinePath+"-wal")
	_ = copyFile(db.path+"-shm", quarantinePath+"-shm")

	return quarantinePath, nil
}

// WithTx runs a function inside a database transaction.
func (db *DB) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// Checkpoint runs a WAL checkpoint to truncate or passive checkpoint the WAL file.
func (db *DB) Checkpoint() error {
	var busy, log, checkpointed int
	row := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE);")
	return row.Scan(&busy, &log, &checkpointed)
}
