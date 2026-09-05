package tests

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestStress_ScaleAndMemory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tmpDir, err := os.MkdirTemp("", "fb-stress-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "stress.db"), 5000, 4, 2)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(database)

	// 1. Create 10 calendars
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-stress",
		PersonID:          "person-scott",
		Provider:          "google",
		ProviderAccountID: "g-stress",
		VerifiedEmail:     "scottrmhardie@gmail.com",
		Scopes:            "cal",
		ExpiresAt:         time.Now().Add(time.Hour),
		Status:            "active",
	})

	for i := 1; i <= 10; i++ {
		_ = store.UpsertCalendar(ctx, &models.Calendar{
			ID:                 fmt.Sprintf("cal-%d", i),
			OAuthAccountID:     "acc-stress",
			Provider:           "google",
			ProviderCalendarID: fmt.Sprintf("cal-%d@gmail.com", i),
			Name:               fmt.Sprintf("Calendar %d", i),
			EnabledForDisplay:  true,
			Writable:           true,
		})
	}

	// 2. Bulk Insert 10,000 events in batch transactions
	t.Log("Inserting 10,000 events...")
	startInsert := time.Now()

	baseDate := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	batchSize := 500

	for batch := 0; batch < 20; batch++ {
		err := database.WithTx(ctx, func(tx *sql.Tx) error {
			stmt, err := tx.PrepareContext(ctx, `INSERT INTO events (id, provider, provider_event_id, provider_uid, calendar_id, title, start_at, end_at, status, local_created_at, local_updated_at)
				VALUES (?, 'google', ?, ?, ?, ?, ?, ?, 'confirmed', ?, ?);`)
			if err != nil {
				return err
			}
			defer stmt.Close()

			for j := 0; j < batchSize; j++ {
				idx := batch*batchSize + j
				evID := fmt.Sprintf("evt-stress-%d", idx)
				calID := fmt.Sprintf("cal-%d", (idx%10)+1)
				evStart := baseDate.Add(time.Duration(idx) * 45 * time.Minute)
				evEnd := evStart.Add(30 * time.Minute)
				now := time.Now()

				_, err := stmt.ExecContext(ctx, evID, fmt.Sprintf("pevt-%d", idx), fmt.Sprintf("uid-%d@g.com", idx), calID, fmt.Sprintf("Event %d", idx), evStart, evEnd, now, now)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("batch insert %d failed: %v", batch, err)
		}
	}
	insertDuration := time.Since(startInsert)
	t.Logf("10,000 events inserted in %v (avg %.2f ms per 100 events)", insertDuration, float64(insertDuration.Milliseconds())/100.0)

	// 3. Verify event count
	count, err := store.CountActiveEvents(ctx)
	if err != nil || count != 10000 {
		t.Fatalf("expected 10,000 events, got %d (err=%v)", count, err)
	}

	// 4. Test Range Query Performance (1 month window over 10,000 events)
	startQuery := time.Now()
	queryStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	queryEnd := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	results, err := store.ListEventsRange(ctx, queryStart, queryEnd)
	queryDuration := time.Since(startQuery)
	if err != nil {
		t.Fatalf("range query failed: %v", err)
	}

	t.Logf("Range query retrieved %d events in %v", len(results), queryDuration)
	if queryDuration > 100*time.Millisecond {
		t.Errorf("expected range query < 100ms, took %v", queryDuration)
	}

	// 5. Check Memory Footprint
	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	allocMB := memStats.Alloc / 1024 / 1024
	sysMB := memStats.Sys / 1024 / 1024
	t.Logf("Memory footprint after 10,000 events: Alloc=%d MB, Sys=%d MB", allocMB, sysMB)

	if allocMB > 80 {
		t.Errorf("memory footprint exceeds 80 MB threshold for 1 GB RAM appliance: %d MB", allocMB)
	}
}
