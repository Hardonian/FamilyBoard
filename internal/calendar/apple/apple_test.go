package apple

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/mail/ics"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func TestAppleCalendarClient_Name(t *testing.T) {
	client := NewAppleCalendarClient("", nil)
	if client.Name() != "apple" {
		t.Errorf("expected client name 'apple', got %q", client.Name())
	}
}

func TestAppleCalendarClient_WebcalListAndSync(t *testing.T) {
	sampleICS := `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//macOS 15.0//EN
X-WR-CALNAME:Smith Family Shared
BEGIN:VEVENT
UID:evt-webcal-1@apple.com
DTSTART:20261020T170000Z
DTEND:20261020T180000Z
SUMMARY:Soccer Practice
LOCATION:Westfield Park
DESCRIPTION:Bring shin guards
STATUS:CONFIRMED
END:VEVENT
BEGIN:VEVENT
UID:evt-webcal-2@apple.com
DTSTART:20261021T190000Z
DTEND:20261021T193000Z
SUMMARY:Grandma FaceTime
URL:https://facetime.apple.com/join#v=1&p=123456
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer server.Close()

	webcalURL := "webcal://" + strings.TrimPrefix(server.URL, "http://")
	client := NewAppleCalendarClient("", server.Client())
	ctx := context.Background()

	// 1. ListCalendars
	cals, err := client.ListCalendars(ctx, webcalURL)
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(cals))
	}
	cal := cals[0]
	if cal.Name != "Smith Family Shared" {
		t.Errorf("expected calendar name 'Smith Family Shared', got %q", cal.Name)
	}
	if cal.Provider != "apple" {
		t.Errorf("expected provider 'apple', got %q", cal.Provider)
	}
	if cal.Writable {
		t.Errorf("webcal feed should not be writable")
	}

	// 2. SyncEvents
	syncRes, err := client.SyncEvents(ctx, webcalURL, cal, "")
	if err != nil {
		t.Fatalf("SyncEvents failed: %v", err)
	}
	if len(syncRes.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(syncRes.Events))
	}
	if syncRes.Events[0].Title != "Soccer Practice" {
		t.Errorf("unexpected event 0 title: %q", syncRes.Events[0].Title)
	}
	if syncRes.Events[1].Location != "Online Meeting" {
		t.Errorf("expected online meeting location for FaceTime call, got %q", syncRes.Events[1].Location)
	}

	// 3. Webcal mutations should fail gracefully as read-only
	newEvent := &models.Event{Title: "Test"}
	if _, err := client.CreateEvent(ctx, webcalURL, cal.ProviderCalendarID, newEvent, "idemp-1"); err == nil {
		t.Errorf("expected error creating event in read-only webcal feed")
	}
	if _, err := client.UpdateEvent(ctx, webcalURL, cal.ProviderCalendarID, newEvent); err == nil {
		t.Errorf("expected error updating event in read-only webcal feed")
	}
	if err := client.DeleteEvent(ctx, webcalURL, cal.ProviderCalendarID, "evt-1"); err == nil {
		t.Errorf("expected error deleting event in read-only webcal feed")
	}
}

func TestAppleCalendarClient_CalDAV_DiscoveryAndCRUD(t *testing.T) {
	var createdUID string
	var createdBody string
	var updatedBody string
	deletedUID := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user@icloud.com" || pass != "app-pass-1234" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.Method {
		case "PROPFIND":
			depth := r.Header.Get("Depth")
			if strings.Contains(r.URL.Path, "/principal/") {
				// Calendar-home-set discovery
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:response>
    <D:href>/user@icloud.com/principal/</D:href>
    <D:propstat>
      <D:prop>
        <C:calendar-home-set>
          <D:href>/user@icloud.com/calendars/</D:href>
        </C:calendar-home-set>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
				return
			}

			if strings.Contains(r.URL.Path, "/calendars/") && depth == "1" {
				// Collections discovery
				w.Header().Set("Content-Type", "application/xml; charset=utf-8")
				w.WriteHeader(http.StatusMultiStatus)
				_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:IC="http://apple.com/ns/ical/">
  <D:response>
    <D:href>/user@icloud.com/calendars/home/</D:href>
    <D:propstat>
      <D:prop>
        <D:displayname>Home</D:displayname>
        <D:resourcetype>
          <D:collection/>
          <C:calendar/>
        </D:resourcetype>
        <IC:calendar-color>#10b981</IC:calendar-color>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
				return
			}

		case "REPORT":
			w.Header().Set("Content-Type", "application/xml; charset=utf-8")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:response>
    <D:href>/user@icloud.com/calendars/home/event-101.ics</D:href>
    <D:propstat>
      <D:prop>
        <D:getetag>"etag-101"</D:getetag>
        <C:calendar-data>BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Apple Inc.//EN
BEGIN:VEVENT
UID:event-101
DTSTART:20261025T140000Z
DTEND:20261025T150000Z
SUMMARY:Family Walk
LOCATION:Riverside Park
STATUS:CONFIRMED
END:VEVENT
END:VCALENDAR</C:calendar-data>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))
			return

		case "PUT":
			bodyBytes, _ := io.ReadAll(r.Body)
			if strings.Contains(string(bodyBytes), "SUMMARY:Updated Family Walk") {
				updatedBody = string(bodyBytes)
				w.WriteHeader(http.StatusOK)
				return
			}
			createdBody = string(bodyBytes)
			parts := strings.Split(r.URL.Path, "/")
			createdUID = strings.TrimSuffix(parts[len(parts)-1], ".ics")
			w.WriteHeader(http.StatusCreated)
			return

		case "DELETE":
			parts := strings.Split(r.URL.Path, "/")
			deletedUID = strings.TrimSuffix(parts[len(parts)-1], ".ics")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewAppleCalendarClient(server.URL, server.Client())
	ctx := context.Background()
	credentials := "user@icloud.com:app-pass-1234"

	// 1. Discover Calendars via CalDAV
	cals, err := client.ListCalendars(ctx, credentials)
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	if len(cals) != 1 {
		t.Fatalf("expected 1 calendar, got %d", len(cals))
	}
	cal := cals[0]
	if cal.Name != "Home" {
		t.Errorf("expected calendar name 'Home', got %q", cal.Name)
	}
	if !cal.Writable {
		t.Errorf("CalDAV collection should be writable")
	}
	if !cal.DefaultWriteTarget {
		t.Errorf("Calendar named 'Home' should default to write target")
	}

	// 2. Sync Events via CalDAV REPORT
	res, err := client.SyncEvents(ctx, credentials, cal, "")
	if err != nil {
		t.Fatalf("SyncEvents failed: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(res.Events))
	}
	if res.Events[0].Title != "Family Walk" {
		t.Errorf("expected event title 'Family Walk', got %q", res.Events[0].Title)
	}

	// 3. Create Event via CalDAV PUT
	newEvent := &models.Event{
		ProviderUID: "fb-apple-uuid-999",
		Title:       "Doctor Checkup",
		Description: "Annual physical",
		Location:    "Health Center",
		StartAt:     time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC),
		EndAt:       time.Date(2026, 10, 26, 11, 0, 0, 0, time.UTC),
		AllDay:      false,
	}
	created, err := client.CreateEvent(ctx, credentials, cal.ProviderCalendarID, newEvent, "idemp-put-1")
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if created.ProviderEventID != "fb-apple-uuid-999" {
		t.Errorf("expected ProviderEventID fb-apple-uuid-999, got %q", created.ProviderEventID)
	}
	if createdUID != "fb-apple-uuid-999" {
		t.Errorf("expected PUT slug fb-apple-uuid-999, got %q", createdUID)
	}
	if !strings.Contains(createdBody, "SUMMARY:Doctor Checkup") {
		t.Errorf("expected SUMMARY:Doctor Checkup in PUT body, got %s", createdBody)
	}

	// 4. Update Event via CalDAV PUT
	created.Title = "Updated Family Walk"
	_, err = client.UpdateEvent(ctx, credentials, cal.ProviderCalendarID, created)
	if err != nil {
		t.Fatalf("UpdateEvent failed: %v", err)
	}
	if !strings.Contains(updatedBody, "SUMMARY:Updated Family Walk") {
		t.Errorf("expected updated title in body, got %s", updatedBody)
	}

	// 5. Delete Event via CalDAV DELETE
	if err := client.DeleteEvent(ctx, credentials, cal.ProviderCalendarID, "event-101.ics"); err != nil {
		t.Fatalf("DeleteEvent failed: %v", err)
	}
	if deletedUID != "event-101" {
		t.Errorf("expected deleted slug 'event-101', got %q", deletedUID)
	}
}

func TestAppleCalendar_AllDay_RFC5545Compliance(t *testing.T) {
	startDate := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	endDate := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	ev := &models.Event{
		Title:       "All-Day School PD Day",
		Description: "No classes today",
		Location:    "Home",
		StartAt:     startDate,
		EndAt:       endDate,
		AllDay:      true,
	}

	icsPayload := formatVEventICS("all-day-test-1", ev)

	if !strings.Contains(icsPayload, "DTSTART;VALUE=DATE:20261028") {
		t.Errorf("expected DTSTART;VALUE=DATE:20261028 in RFC 5545 payload, got: %s", icsPayload)
	}
	if !strings.Contains(icsPayload, "DTEND;VALUE=DATE:20261029") {
		t.Errorf("expected DTEND;VALUE=DATE:20261029 in RFC 5545 payload, got: %s", icsPayload)
	}

	// Verify parse round-trip
	parsed, err := ics.Parse([]byte(icsPayload))
	if err != nil {
		t.Fatalf("ics.Parse failed on generated payload: %v", err)
	}
	if len(parsed.Events) != 1 {
		t.Fatalf("expected 1 parsed event, got %d", len(parsed.Events))
	}
	pEv := parsed.Events[0]
	if !pEv.AllDay {
		t.Errorf("expected parsed event to have AllDay=true")
	}
	if pEv.Summary != "All-Day School PD Day" {
		t.Errorf("expected summary 'All-Day School PD Day', got %q", pEv.Summary)
	}
}

func TestAppleCalendar_CredentialsValidation(t *testing.T) {
	client := NewAppleCalendarClient("", nil)
	ctx := context.Background()

	// Missing password
	_, err := client.ListCalendars(ctx, "invalid-single-token")
	if err == nil {
		t.Errorf("expected error for credentials without colon separator")
	}
}

func TestAppleCalendar_CreateUsesStableUIDForReplay(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		paths = append(paths, r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewAppleCalendarClient(server.URL, server.Client())
	event := &models.Event{
		Title: "Stable create", StartAt: time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC),
		EndAt: time.Date(2026, 10, 25, 11, 0, 0, 0, time.UTC),
	}
	first, err := client.CreateEvent(context.Background(), "user@example.com:app-password", "/calendar", event, "stable-apple-key")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := client.CreateEvent(context.Background(), "user@example.com:app-password", "/calendar", event, "stable-apple-key")
	if err != nil {
		t.Fatalf("replayed create: %v", err)
	}
	if len(paths) != 2 || paths[0] != paths[1] || first.ProviderUID != second.ProviderUID {
		t.Fatalf("create replay was not stable: paths=%v first=%q second=%q", paths, first.ProviderUID, second.ProviderUID)
	}
}
