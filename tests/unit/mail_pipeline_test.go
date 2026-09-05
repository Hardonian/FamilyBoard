package unit_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/dedupe"
	"github.com/Hardonian/FamilyBoard/internal/mail/ics"
	"github.com/Hardonian/FamilyBoard/internal/mail/mime"
	"github.com/Hardonian/FamilyBoard/internal/mail/parser"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/rules"
)

func TestMIMEParser_HTMLSanitizationAndLimits(t *testing.T) {
	p := mime.NewParser(1024*1024, 512*1024)

	raw := []byte("From: coach@soccer.com\r\n" +
		"Subject: Soccer Practice\r\n" +
		"Date: Mon, 10 Sep 2026 10:00:00 -0400\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body>" +
		"<p>Soccer practice on <b>September 18 at 6 PM</b>.</p>" +
		"<script>alert('malicious script execution attempt');</script>" +
		"<img src='https://tracker.pixel.com/beacon.gif' />" +
		"</body></html>")

	parsed, err := p.Parse(raw)
	if err != nil {
		t.Fatalf("failed to parse mime: %v", err)
	}

	if parsed.Subject != "Soccer Practice" {
		t.Errorf("expected subject 'Soccer Practice', got '%s'", parsed.Subject)
	}

	// Verify dangerous script tag and its inner content was stripped
	if strings.Contains(parsed.TextBody, "alert") || strings.Contains(parsed.TextBody, "<script>") {
		t.Errorf("sanitization failed: body contains script content: %q", parsed.TextBody)
	}

	// Verify text content was extracted cleanly
	if !strings.Contains(parsed.TextBody, "Soccer practice on September 18 at 6 PM.") {
		t.Errorf("expected clean text body, got: %q", parsed.TextBody)
	}
}

func TestICSParser_UnfoldingAndEvents(t *testing.T) {
	rawICS := []byte("BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"METHOD:REQUEST\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:event-12345@familyboard.local\r\n" +
		"DTSTART;TZID=America/Toronto:20260922T153000\r\n" +
		"DTEND;TZID=America/Toronto:20260922T163000\r\n" +
		"SUMMARY:Dentist App\r\n" +
		" ointment with Dr. Smith\r\n" +
		"LOCATION:City Dental Clinic\\, Suite 101\r\n" +
		"DESCRIPTION:Routine teeth cleaning\\nBring dental insurance card\r\n" +
		"STATUS:CONFIRMED\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	cal, err := ics.Parse(rawICS)
	if err != nil {
		t.Fatalf("failed to parse ics: %v", err)
	}

	if len(cal.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(cal.Events))
	}

	ev := cal.Events[0]
	if ev.UID != "event-12345@familyboard.local" {
		t.Errorf("expected UID 'event-12345@familyboard.local', got '%s'", ev.UID)
	}

	// Verify line unfolding worked on SUMMARY
	if ev.Summary != "Dentist Appointment with Dr. Smith" {
		t.Errorf("expected unfolded summary 'Dentist Appointment with Dr. Smith', got '%s'", ev.Summary)
	}

	// Verify comma unescaping on LOCATION
	if ev.Location != "City Dental Clinic, Suite 101" {
		t.Errorf("expected unescaped location, got '%s'", ev.Location)
	}

	// Verify newline unescaping on DESCRIPTION
	if !strings.Contains(ev.Description, "\n") {
		t.Errorf("expected newline in description, got '%s'", ev.Description)
	}

	// Verify parsed start and end times
	if ev.StartAt.Hour() != 15 || ev.StartAt.Minute() != 30 {
		t.Errorf("expected start at 15:30, got %s", ev.StartAt)
	}
	if ev.EndAt.Hour() != 16 || ev.EndAt.Minute() != 30 {
		t.Errorf("expected end at 16:30, got %s", ev.EndAt)
	}
}

func TestNLParser_PracticalFamilyMessages(t *testing.T) {
	nlp := parser.NewNaturalLanguageParser("America/Toronto")
	referenceDate := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	cases := []struct {
		input      string
		subject    string
		wantMonth  time.Month
		wantDay    int
		wantStartH int
		wantStartM int
		wantAllDay bool
		wantLoc    string
	}{
		{
			input:      "Soccer September 18 at 6 PM",
			subject:    "Soccer Schedule",
			wantMonth:  time.September,
			wantDay:    18,
			wantStartH: 18,
			wantStartM: 0,
			wantAllDay: false,
		},
		{
			input:      "Dentist Sept 22 3:30-4:30",
			subject:    "Reminder",
			wantMonth:  time.September,
			wantDay:    22,
			wantStartH: 15,
			wantStartM: 30,
			wantAllDay: false,
		},
		{
			input:      "Swimming Wednesday 5pm at Community Centre",
			subject:    "Swim lessons",
			wantStartH: 17,
			wantStartM: 0,
			wantAllDay: false,
			wantLoc:    "Community Centre",
		},
		{
			input:      "School picture day October 2",
			subject:    "School Event",
			wantMonth:  time.October,
			wantDay:    2,
			wantAllDay: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			res, err := nlp.Parse(tc.input, tc.subject, referenceDate)
			if err != nil {
				t.Fatalf("unexpected error parsing %q: %v", tc.input, err)
			}
			if tc.wantMonth != 0 && res.StartAt.Month() != tc.wantMonth {
				t.Errorf("expected month %s, got %s", tc.wantMonth, res.StartAt.Month())
			}
			if tc.wantDay != 0 && res.StartAt.Day() != tc.wantDay {
				t.Errorf("expected day %d, got %d", tc.wantDay, res.StartAt.Day())
			}
			if !tc.wantAllDay {
				if res.StartAt.Hour() != tc.wantStartH {
					t.Errorf("expected start hour %d, got %d", tc.wantStartH, res.StartAt.Hour())
				}
				if res.StartAt.Minute() != tc.wantStartM {
					t.Errorf("expected start minute %d, got %d", tc.wantStartM, res.StartAt.Minute())
				}
			}
			if res.AllDay != tc.wantAllDay {
				t.Errorf("expected AllDay=%v, got %v", tc.wantAllDay, res.AllDay)
			}
			if tc.wantLoc != "" && !strings.Contains(res.Location, tc.wantLoc) {
				t.Errorf("expected location containing %q, got %q", tc.wantLoc, res.Location)
			}
			if res.Confidence < 0.5 {
				t.Errorf("expected confidence >= 0.5, got %f", res.Confidence)
			}
		})
	}
}

func TestRules_PolicyAutoApproval(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rules-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "test.db"), 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	_ = db.Migrate(ctx, database)
	store := db.NewStore(database)

	policy := rules.NewPolicyEngine(store, 0.90)

	// Registered family member is trusted sender
	trusted, _ := policy.IsSenderTrusted(ctx, "alex@example.com")
	if !trusted {
		t.Errorf("expected Alex to be trusted")
	}

	// Unknown sender is untrusted
	trusted, _ = policy.IsSenderTrusted(ctx, "stranger@unknown.com")
	if trusted {
		t.Errorf("expected stranger to be untrusted")
	}

	// High confidence candidate from trusted sender -> auto-approved
	highConfCand := &models.EventCandidate{
		Title:               "Soccer Practice",
		StartAt:             time.Now(),
		SuggestedCalendarID: "cal-primary",
		Confidence:          0.95,
	}

	state, _ := policy.Evaluate(highConfCand, true, false)
	if state != models.CandidateAutoApproved {
		t.Errorf("expected CandidateAutoApproved, got %s", state)
	}

	// Untrusted sender -> pending
	state, reason := policy.Evaluate(highConfCand, false, false)
	if state != models.CandidatePending {
		t.Errorf("expected CandidatePending for untrusted sender, got %s", state)
	}
	if !strings.Contains(reason, "trusted") {
		t.Errorf("expected trusted in reason, got %s", reason)
	}

	// Duplicate -> duplicate
	state, _ = policy.Evaluate(highConfCand, true, true)
	if state != models.CandidateDuplicate {
		t.Errorf("expected CandidateDuplicate, got %s", state)
	}
}

func TestDedupe_SemanticAndUID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dedupe-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	database, err := db.Open(filepath.Join(tmpDir, "test.db"), 5000, 2, 1)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	_ = db.Migrate(ctx, database)
	store := db.NewStore(database)

	// Setup person, account, calendar
	_ = store.UpsertOAuthAccount(ctx, &models.OAuthAccount{
		ID:                "acc-test",
		PersonID:          "person-1",
		Provider:          "google",
		ProviderAccountID: "g-1",
		VerifiedEmail:     "alex@example.com",
		Scopes:            "cal",
		ExpiresAt:         time.Now().Add(time.Hour),
		Status:            "active",
	})
	calID := "cal-dedupe-1"
	_ = store.UpsertCalendar(ctx, &models.Calendar{
		ID:                 calID,
		OAuthAccountID:     "acc-test",
		Provider:           "google",
		ProviderCalendarID: "p-cal-1",
		Name:               "Family",
		EnabledForDisplay:  true,
		Writable:           true,
	})

	startTime := time.Date(2026, 9, 20, 18, 0, 0, 0, time.UTC)
	_ = store.UpsertEvent(ctx, &models.Event{
		ID:             "evt-existing-1",
		Provider:       "google",
		ProviderUID:    "uid-unique-999",
		CalendarID:     calID,
		Title:          "Family Dinner",
		StartAt:        startTime,
		EndAt:          startTime.Add(2 * time.Hour),
		Status:         "confirmed",
		LocalCreatedAt: time.Now(),
		LocalUpdatedAt: time.Now(),
	})

	engine := dedupe.NewEngine(store)

	// Check by matching UID
	res, err := engine.CheckEventDuplicate(ctx, calID, "uid-unique-999", "Different Title", startTime)
	if err != nil || !res.IsDuplicate {
		t.Errorf("expected duplicate by UID, got res=%+v, err=%v", res, err)
	}

	// Check by semantic matching (same calendar, same title, same start time)
	res, err = engine.CheckEventDuplicate(ctx, calID, "new-uid", "Family Dinner", startTime)
	if err != nil || !res.IsDuplicate {
		t.Errorf("expected duplicate by semantic title & time, got res=%+v, err=%v", res, err)
	}

	// Different event -> not duplicate
	res, err = engine.CheckEventDuplicate(ctx, calID, "new-uid-2", "Grocery Shopping", startTime.Add(24*time.Hour))
	if err != nil || res.IsDuplicate {
		t.Errorf("expected non-duplicate, got res=%+v, err=%v", res, err)
	}
}
