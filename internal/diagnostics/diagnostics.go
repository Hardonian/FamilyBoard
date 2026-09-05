package diagnostics

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Hardonian/FamilyBoard/internal/db"
)

type DiagnosticReport struct {
	Version      string         `json:"version"`
	Platform     string         `json:"platform"`
	Architecture string         `json:"architecture"`
	Hardware     string         `json:"hardware"`
	NumCPU       int            `json:"num_cpu"`
	Goroutines   int            `json:"goroutines"`
	MemoryAlloc  uint64         `json:"memory_alloc_bytes"`
	MemorySys    uint64         `json:"memory_sys_bytes"`
	Database     DatabaseReport `json:"database"`
	Queue        QueueReport    `json:"queue"`
	Counts       map[string]int `json:"counts"`
}

type DatabaseReport struct {
	Path      string `json:"path"`
	Integrity string `json:"integrity"`
	SizeBytes int64  `json:"size_bytes"`
	WALBytes  int64  `json:"wal_bytes"`
}

type QueueReport struct {
	Pending    int `json:"pending"`
	Retry      int `json:"retry"`
	DeadLetter int `json:"dead_letter"`
}

// Generate collects privacy-scrubbed system and appliance diagnostics.
func Generate(ctx context.Context, store *db.Store, dbPath string) (*DiagnosticReport, error) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	hwModel := "Standard PC / VM"
	if modelBytes, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		hwModel = strings.TrimRight(string(modelBytes), "\x00\n\r")
	}

	dbIntegrity := "OK"
	if err := store.DB().CheckIntegrity(); err != nil {
		dbIntegrity = "FAILED: " + err.Error()
	}

	var dbSize, walSize int64
	if fi, err := os.Stat(dbPath); err == nil {
		dbSize = fi.Size()
	}
	if fi, err := os.Stat(dbPath + "-wal"); err == nil {
		walSize = fi.Size()
	}

	counts := make(map[string]int)
	tables := []string{"people", "oauth_accounts", "calendars", "events", "mail_sources", "processed_messages", "event_candidates", "mutation_queue", "conflicts", "audit_log"}
	for _, tbl := range tables {
		var c int
		_ = store.DB().QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s;", tbl)).Scan(&c)
		counts[tbl] = c
	}

	var pending, retry, deadLetter int
	_ = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM mutation_queue WHERE state = 'pending';").Scan(&pending)
	_ = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM mutation_queue WHERE state = 'retry';").Scan(&retry)
	_ = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM mutation_queue WHERE state = 'dead_letter';").Scan(&deadLetter)

	report := &DiagnosticReport{
		Version:      "v0.9.0",
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		Hardware:     hwModel,
		NumCPU:       runtime.NumCPU(),
		Goroutines:   runtime.NumGoroutine(),
		MemoryAlloc:  m.Alloc,
		MemorySys:    m.Sys,
		Database: DatabaseReport{
			Path:      dbPath,
			Integrity: dbIntegrity,
			SizeBytes: dbSize,
			WALBytes:  walSize,
		},
		Queue: QueueReport{
			Pending:    pending,
			Retry:      retry,
			DeadLetter: deadLetter,
		},
		Counts: counts,
	}

	return report, nil
}
