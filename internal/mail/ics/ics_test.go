package ics

import (
	"strings"
	"testing"
	"time"
)

func TestParseCalendarAndWindowsTimezones(t *testing.T) {
	t.Parallel()

	data := []byte("BEGIN:VCALENDAR\r\n" +
		"X-WR-TIMEZONE:America/Toronto\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:floating\r\n" +
		"DTSTART:20261101T090000\r\n" +
		"DTEND:20261101T100000\r\n" +
		"SUMMARY:Floating local time\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:outlook\r\n" +
		"DTSTART;TZID=\"Eastern Standard Time\":20261102T090000\r\n" +
		"DTEND;TZID=\"Eastern Standard Time\":20261102T100000\r\n" +
		"SUMMARY:Outlook time\r\n" +
		"END:VEVENT\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:all-day\r\n" +
		"DTSTART;VALUE=DATE:20261103\r\n" +
		"SUMMARY:Local all-day event\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n")

	cal, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(cal.Events))
	}
	for _, event := range cal.Events {
		if event.Timezone != "America/Toronto" && event.Timezone != "America/New_York" {
			t.Errorf("event %q has unexpected timezone %q", event.UID, event.Timezone)
		}
		if event.StartAt.Hour() != 0 && event.StartAt.Hour() != 9 {
			t.Errorf("event %q has unexpected local hour %d", event.UID, event.StartAt.Hour())
		}
	}
	if !cal.Events[2].AllDay || cal.Events[2].StartAt.Location().String() != "America/Toronto" {
		t.Fatalf("all-day event did not retain calendar timezone: %+v", cal.Events[2])
	}
}

func TestParseAcceptsLongVendorProperty(t *testing.T) {
	t.Parallel()

	summary := strings.Repeat("x", 70*1024)
	data := []byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:long\nDTSTART:20261010T120000Z\nSUMMARY:" + summary + "\nEND:VEVENT\nEND:VCALENDAR\n")
	cal, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cal.Events) != 1 || len(cal.Events[0].Summary) != len(summary) {
		t.Fatalf("long property was truncated: events=%d summary=%d", len(cal.Events), len(cal.Events[0].Summary))
	}
}

func TestParseRejectsUnboundedProperty(t *testing.T) {
	t.Parallel()

	data := []byte("BEGIN:VCALENDAR\nX-VENDOR:" + strings.Repeat("x", maxICSPropertyLen+1) + "\nEND:VCALENDAR\n")
	if _, err := Parse(data); err == nil {
		t.Fatal("expected an oversized property to fail")
	}
}

func TestParseUTCDateTime(t *testing.T) {
	t.Parallel()
	cal, err := Parse([]byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:utc\nDTSTART:20261010T120000Z\nEND:VEVENT\nEND:VCALENDAR\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cal.Events) != 1 || cal.Events[0].StartAt.Location() != time.UTC {
		t.Fatalf("expected UTC event, got %+v", cal.Events)
	}
}
