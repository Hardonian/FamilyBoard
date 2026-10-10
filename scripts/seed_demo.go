package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
	_ "modernc.org/sqlite"
)

func main() {
	dbDir := "demo_run"
	_ = os.MkdirAll(dbDir, 0755)
	dbPath := filepath.Join(dbDir, "demo.db")
	_ = os.Remove(dbPath)

	database, err := db.Open(dbPath, 5000, 2, 1)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	if err := db.Migrate(ctx, database); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	store := db.NewStore(database)
	now := time.Now()

	// 1. Initialize appliance metadata
	if err := store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    "Kitchen Kiosk",
		Timezone:      "America/Toronto",
		InitializedAt: now.AddDate(0, -1, 0),
		Version:       buildinfo.Version,
	}); err != nil {
		log.Fatalf("set metadata: %v", err)
	}

	// 2. OAuth Accounts for Family Members
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-member-1",
		PersonID:          "person-1",
		Provider:          "google",
		ProviderAccountID: "g-member-1",
		VerifiedEmail:     "alex@example.com",
		Scopes:            "calendar",
		ExpiresAt:         now.Add(365 * 24 * time.Hour),
		Status:            "active",
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-member-2",
		PersonID:          "person-2",
		Provider:          "google",
		ProviderAccountID: "g-member-2",
		VerifiedEmail:     "jordan@example.com",
		Scopes:            "calendar",
		ExpiresAt:         now.Add(365 * 24 * time.Hour),
		Status:            "active",
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	// 3. Calendars
	calFamily := &models.Calendar{
		ID:                 "cal-family",
		OAuthAccountID:     "acc-member-1",
		Provider:           "google",
		ProviderCalendarID: "family@group.calendar.google.com",
		Name:               "Family Hub",
		DisplayColor:       "#8b5cf6",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, calFamily)

	calAlex := &models.Calendar{
		ID:                 "cal-member-1",
		OAuthAccountID:     "acc-member-1",
		Provider:           "google",
		ProviderCalendarID: "alex@example.com",
		Name:               "Alex",
		DisplayColor:       "#38bdf8",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, calAlex)

	calJordan := &models.Calendar{
		ID:                 "cal-member-2",
		OAuthAccountID:     "acc-member-2",
		Provider:           "google",
		ProviderCalendarID: "jordan@example.com",
		Name:               "Jordan",
		DisplayColor:       "#fb7185",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, calJordan)

	calKids := &models.Calendar{
		ID:                 "cal-kids",
		OAuthAccountID:     "acc-member-1",
		Provider:           "google",
		ProviderCalendarID: "kids@group.calendar.google.com",
		Name:               "Kids & School",
		DisplayColor:       "#34d399",
		EnabledForDisplay:  true,
		Writable:           true,
		DefaultWriteTarget: false,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	_ = store.UpsertCalendar(ctx, calKids)

	// 4. Sample Events for Today and this week
	todayYear, todayMonth, todayDay := now.Date()

	events := []*models.Event{
		{
			ID:             "ev-dropoff",
			CalendarID:     calKids.ID,
			Title:          "School Drop-off 🎒",
			StartAt:        time.Date(todayYear, todayMonth, todayDay, 8, 30, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay, 9, 0, 0, 0, now.Location()),
			Location:       "Central Elementary",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-dentist",
			CalendarID:     calJordan.ID,
			Title:          "Dentist Checkup 🦷",
			StartAt:        time.Date(todayYear, todayMonth, todayDay, 12, 30, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay, 13, 15, 0, 0, now.Location()),
			Location:       "City Dental Clinic",
			Description:    "Annual routine cleaning and checkup.",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-soccer",
			CalendarID:     calKids.ID,
			Title:          "Soccer Practice ⚽",
			StartAt:        time.Date(todayYear, todayMonth, todayDay, 16, 0, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay, 17, 30, 0, 0, now.Location()),
			Location:       "Sports Complex Field 3",
			Description:    "Bring water bottle and shin guards.",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-dinner",
			CalendarID:     calFamily.ID,
			Title:          "Family Taco Night 🌮",
			StartAt:        time.Date(todayYear, todayMonth, todayDay, 18, 30, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay, 19, 45, 0, 0, now.Location()),
			Location:       "Kitchen",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-swimming",
			CalendarID:     calKids.ID,
			Title:          "Swim Lessons 🏊",
			StartAt:        time.Date(todayYear, todayMonth, todayDay+1, 10, 0, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay+1, 11, 0, 0, 0, now.Location()),
			Location:       "Civic Community Pool",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-birthday",
			CalendarID:     calFamily.ID,
			Title:          "Family Birthday! 🎂",
			StartAt:        time.Date(todayYear, todayMonth, todayDay+2, 0, 0, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay+3, 0, 0, 0, 0, now.Location()),
			AllDay:         true,
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
		{
			ID:             "ev-movie",
			CalendarID:     calFamily.ID,
			Title:          "Family Movie Night 🎬",
			StartAt:        time.Date(todayYear, todayMonth, todayDay+3, 19, 0, 0, 0, now.Location()),
			EndAt:          time.Date(todayYear, todayMonth, todayDay+3, 21, 30, 0, 0, now.Location()),
			Location:       "Living Room",
			Status:         "confirmed",
			Source:         "kiosk",
			LocalCreatedAt: now,
			LocalUpdatedAt: now,
		},
	}

	for _, ev := range events {
		if err := store.UpsertEvent(ctx, ev); err != nil {
			log.Fatalf("upsert event: %v", err)
		}
	}

	// Write demo config TOML
	hash, _ := auth.HashPassword("admin123")
	cfgContent := fmt.Sprintf(`[server]
host = "127.0.0.1"
port = 8085
base_url = "http://127.0.0.1:8085"
timezone = "America/Toronto"
device_name = "Kitchen Kiosk"

[database]
path = "demo_run/demo.db"
busy_timeout_ms = 5000
max_open_conns = 4
max_idle_conns = 2

[kiosk]
theme = "auto"
default_view = "hybrid"
day_starts_at = "07:00"
night_starts_at = "20:30"

[security]
session_secret = "demo-session-secret-kiosk-12345678"
cookie_secure = false
credentials_dir = "demo_run/credentials"

[auth]
admin_username = "admin"
admin_password_hash = "%s"
`, hash)

	_ = os.WriteFile("demo_run/demo.toml", []byte(cfgContent), 0644)
	fmt.Println("Demo environment seeded successfully at demo_run/demo.db and demo_run/demo.toml")
}
