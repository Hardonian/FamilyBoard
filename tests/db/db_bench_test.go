package db_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestDB_ExplainQueryPlan_ListEventsRange(t *testing.T) {
	database, _, cleanup := setupTestDB(t)
	defer cleanup()

	// Query from ListEventsRange
	query := `EXPLAIN QUERY PLAN
		SELECT e.id, e.provider, e.provider_event_id, e.provider_uid, e.calendar_id, e.etag, e.version, e.title, e.description, e.location,
		e.start_at, e.end_at, e.timezone, e.all_day, e.status, e.recurrence_rule, e.recurrence_parent, e.organizer, e.source,
		e.remote_created_at, e.remote_updated_at, e.local_created_at, e.local_updated_at, e.deleted_at
		FROM events e
		JOIN calendars c ON e.calendar_id = c.id
		WHERE c.enabled_for_display = 1
		  AND e.deleted_at IS NULL
		  AND e.start_at < ? AND e.end_at > ?
		ORDER BY e.start_at ASC;`

	rows, err := database.Query(query, time.Now().AddDate(0, 1, 0), time.Now())
	if err != nil {
		t.Fatalf("explain query plan failed: %v", err)
	}
	defer rows.Close()

	var planLines []string
	hasIndexUsage := false
	hasEventsScan := false

	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		planLines = append(planLines, detail)

		if strings.Contains(detail, "USING INDEX") && (strings.Contains(detail, "idx_events") || strings.Contains(detail, "events")) {
			hasIndexUsage = true
		}
		if strings.Contains(detail, "SCAN TABLE events") {
			hasEventsScan = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error: %v", err)
	}

	t.Logf("Query Plan:\n%s", strings.Join(planLines, "\n"))

	if hasEventsScan {
		t.Errorf("Query plan contains unindexed SCAN TABLE events: %v", planLines)
	}
	if !hasIndexUsage {
		t.Errorf("Query plan did not use index on events: %v", planLines)
	}
}

func TestDB_PruneRetentionRecords(t *testing.T) {
	database, store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Seed completed mutation older than 10 days
	oldMut := &models.MutationQueueItem{
		ID:             "mut-old",
		Operation:      "create",
		Provider:       "google",
		CalendarID:     "cal-1",
		LocalEventID:   "ev-1",
		IdempotencyKey: "idem-old",
		State:          models.QueueCompleted,
		AttemptCount:   1,
		NextAttemptAt:  now.AddDate(0, 0, -10),
		CreatedAt:      now.AddDate(0, 0, -10),
		UpdatedAt:      now.AddDate(0, 0, -10),
	}
	_ = store.EnqueueMutation(ctx, oldMut)
	_ = store.UpdateMutationState(ctx, oldMut.ID, models.QueueCompleted, 1, now.AddDate(0, 0, -10), "")
	_, _ = database.Exec("UPDATE mutation_queue SET updated_at = ? WHERE id = ?;", now.AddDate(0, 0, -10), oldMut.ID)

	// Seed fresh completed mutation (1 day old)
	freshMut := &models.MutationQueueItem{
		ID:             "mut-fresh",
		Operation:      "create",
		Provider:       "google",
		CalendarID:     "cal-1",
		LocalEventID:   "ev-2",
		IdempotencyKey: "idem-fresh",
		State:          models.QueueCompleted,
		AttemptCount:   1,
		NextAttemptAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = store.EnqueueMutation(ctx, freshMut)
	_ = store.UpdateMutationState(ctx, freshMut.ID, models.QueueCompleted, 1, now, "")

	// Prune completed mutations older than 7 days
	err := store.PruneRetentionRecords(ctx, 7*24*time.Hour, 30*24*time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("prune retention records: %v", err)
	}

	// Verify old was pruned and fresh remains
	var oldID string
	errOld := database.QueryRow("SELECT id FROM mutation_queue WHERE id = ?;", "mut-old").Scan(&oldID)
	if errOld == nil {
		t.Errorf("expected old mutation to be pruned from database")
	}

	var freshID string
	errFresh := database.QueryRow("SELECT id FROM mutation_queue WHERE id = ?;", "mut-fresh").Scan(&freshID)
	if errFresh != nil || freshID != "mut-fresh" {
		t.Errorf("expected fresh mutation to remain in database: %v", errFresh)
	}
}

func setupBenchmarkDB(b *testing.B) (*db.DB, *db.Store, func()) {
	b.Helper()
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "bench.db")
	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		b.Fatalf("open db: %v", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		database.Close()
		b.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(database)
	cleanup := func() {
		database.Close()
	}
	return database, store, cleanup
}

func BenchmarkDB_ListEventsRange(b *testing.B) {
	_, store, cleanup := setupBenchmarkDB(b)
	defer cleanup()

	ctx := context.Background()
	now := time.Now()

	// Seed OAuth account and calendar
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-bench",
		PersonID:          "person-scott",
		Provider:          "google",
		ProviderAccountID: "g-bench",
		VerifiedEmail:     "scottrmhardie@gmail.com",
		Scopes:            "cal",
		ExpiresAt:         now.Add(time.Hour),
		Status:            "active",
	})
	calID := "cal-bench"
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 calID,
		OAuthAccountID:     "acc-bench",
		Provider:           "google",
		ProviderCalendarID: "scottrmhardie@gmail.com",
		Name:               "Bench Calendar",
		EnabledForDisplay:  true,
		Writable:           true,
	})

	// Insert 200 events for benchmark
	for i := 0; i < 200; i++ {
		start := now.Add(time.Duration(i*2) * time.Hour)
		_ = store.UpsertEvent(ctx, &models.Event{
			ID:             fmt.Sprintf("ev-bench-%d", i),
			CalendarID:     calID,
			Title:          fmt.Sprintf("Benchmark Event %d", i),
			StartAt:        start,
			EndAt:          start.Add(time.Hour),
			Status:         "confirmed",
			Source:         "bench",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		})
	}

	startQuery := now
	endQuery := now.AddDate(0, 1, 0)

	b.ResetTimer()
	for b.Loop() {
		events, err := store.ListEventsRange(ctx, startQuery, endQuery)
		if err != nil {
			b.Fatalf("list events failed: %v", err)
		}
		if len(events) == 0 {
			b.Fatalf("expected events")
		}
	}
}

func BenchmarkKiosk_GzipCompression(b *testing.B) {
	type KioskView struct {
		ID       string    `json:"id"`
		Title    string    `json:"title"`
		StartAt  time.Time `json:"start_at"`
		EndAt    time.Time `json:"end_at"`
		Location string    `json:"location"`
	}

	events := make([]KioskView, 50)
	now := time.Now()
	for i := 0; i < 50; i++ {
		events[i] = KioskView{
			ID:       fmt.Sprintf("ev-%d", i),
			Title:    fmt.Sprintf("Soccer Practice #%d", i),
			StartAt:  now.Add(time.Duration(i) * time.Hour),
			EndAt:    now.Add(time.Duration(i+1) * time.Hour),
			Location: "Sports Complex Field 2",
		}
	}

	rawJSON, _ := json.Marshal(events)

	b.ResetTimer()
	for b.Loop() {
		var buf bytes.Buffer
		gz, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
		_, _ = gz.Write(rawJSON)
		_ = gz.Close()
	}
}
