package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

// TestDB_RealisticScaleProfile seeds several years of events across many
// calendars and asserts that typical kiosk windows stay fast and that
// pathological windows are bounded (P3 #65, #63). Skipped with -short.
func TestDB_RealisticScaleProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("scale profile skipped in -short mode")
	}
	_, store, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Hour)
	_ = store.UpsertPerson(ctx, &models.Person{ID: "person-scale", DisplayName: "Scale", Email: "scale@example.com", Enabled: true, CreatedAt: now, UpdatedAt: now})
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID: "acc-scale", PersonID: "person-scale", Provider: "google", ProviderAccountID: "g-scale",
		VerifiedEmail: "scale@example.com", Scopes: "cal", ExpiresAt: now.Add(time.Hour), Status: "active",
		CreatedAt: now, UpdatedAt: now,
	})

	const calendars = 6
	const years = 3
	const eventsPerCalendar = 2000 // ~12k events total over 3 years
	for c := 0; c < calendars; c++ {
		calID := fmt.Sprintf("cal-scale-%d", c)
		if err := store.UpsertCalendar(ctx, &models.Calendar{
			ID: calID, OAuthAccountID: "acc-scale", Provider: "google",
			ProviderCalendarID: fmt.Sprintf("scale-%d@example.com", c), Name: calID,
			EnabledForDisplay: true, Writable: true, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed calendar: %v", err)
		}
		span := time.Duration(years) * 365 * 24 * time.Hour
		step := span / eventsPerCalendar
		for i := 0; i < eventsPerCalendar; i++ {
			start := now.Add(-span / 2).Add(time.Duration(i) * step)
			if err := store.UpsertEvent(ctx, &models.Event{
				ID: fmt.Sprintf("ev-%d-%d", c, i), CalendarID: calID,
				Title: "Scale event", StartAt: start, EndAt: start.Add(time.Hour),
				Status: "confirmed", Source: "scale", LocalCreatedAt: now, LocalUpdatedAt: now,
			}); err != nil {
				t.Fatalf("seed event: %v", err)
			}
		}
	}

	t0 := time.Now()
	month, err := store.ListEventsRange(ctx, now, now.AddDate(0, 1, 0))
	monthElapsed := time.Since(t0)
	if err != nil {
		t.Fatalf("month window: %v", err)
	}
	t.Logf("month window: %d events in %v", len(month), monthElapsed)
	if monthElapsed > 500*time.Millisecond {
		t.Errorf("month window too slow at scale: %v", monthElapsed)
	}

	huge, err := store.ListEventsRange(ctx, now.AddDate(-5, 0, 0), now.AddDate(5, 0, 0))
	if err != nil {
		t.Fatalf("huge window: %v", err)
	}
	t.Logf("huge window: %d events (cap %d)", len(huge), db.MaxEventsPerWindow)
	if len(huge) > db.MaxEventsPerWindow {
		t.Fatalf("window exceeded cap: %d > %d", len(huge), db.MaxEventsPerWindow)
	}
	if len(huge) != db.MaxEventsPerWindow {
		t.Fatalf("expected cap to engage (%d), got %d", db.MaxEventsPerWindow, len(huge))
	}
}
