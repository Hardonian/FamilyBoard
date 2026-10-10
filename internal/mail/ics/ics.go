package ics

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	UID          string
	Summary      string
	Description  string
	Location     string
	Organizer    string
	URL          string
	StartAt      time.Time
	EndAt        time.Time
	Timezone     string
	AllDay       bool
	Status       string // "confirmed", "tentative", "cancelled"
	Method       string // "REQUEST", "CANCEL", etc.
	RRule        string
	RecurrenceID string
	Sequence     int
}

type Calendar struct {
	Method string
	Events []Event
}

// Parse parses raw RFC 5545 iCalendar data.
func Parse(data []byte) (*Calendar, error) {
	unfolded := unfoldLines(data)
	scanner := bufio.NewScanner(bytes.NewReader(unfolded))

	cal := &Calendar{Method: "REQUEST"}
	var currentEvent *Event
	inEvent := false

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		rawKey := parts[0]
		val := unescapeText(parts[1])

		keyParts := strings.Split(rawKey, ";")
		key := strings.ToUpper(strings.TrimSpace(keyParts[0]))

		params := make(map[string]string)
		for _, param := range keyParts[1:] {
			pParts := strings.SplitN(param, "=", 2)
			if len(pParts) == 2 {
				params[strings.ToUpper(strings.TrimSpace(pParts[0]))] = strings.TrimSpace(pParts[1])
			}
		}

		if key == "BEGIN" && strings.ToUpper(val) == "VEVENT" {
			inEvent = true
			currentEvent = &Event{
				Status:   "confirmed",
				Timezone: "America/Toronto",
			}
			continue
		}

		if key == "END" && strings.ToUpper(val) == "VEVENT" {
			if inEvent && currentEvent != nil {
				if currentEvent.Method == "" && cal.Method != "" {
					currentEvent.Method = cal.Method
				}
				cal.Events = append(cal.Events, *currentEvent)
			}
			inEvent = false
			currentEvent = nil
			continue
		}

		if !inEvent {
			if key == "METHOD" {
				cal.Method = strings.ToUpper(strings.TrimSpace(val))
			}
			continue
		}

		// Inside VEVENT
		switch key {
		case "UID":
			currentEvent.UID = val
		case "SUMMARY":
			currentEvent.Summary = val
		case "DESCRIPTION":
			currentEvent.Description = val
		case "LOCATION":
			currentEvent.Location = val
		case "URL":
			currentEvent.URL = val
		case "ORGANIZER":
			currentEvent.Organizer = val
		case "STATUS":
			currentEvent.Status = strings.ToLower(strings.TrimSpace(val))
		case "METHOD":
			currentEvent.Method = strings.ToUpper(strings.TrimSpace(val))
		case "RRULE":
			currentEvent.RRule = val
		case "RECURRENCE-ID":
			currentEvent.RecurrenceID = val
		case "SEQUENCE":
			var seq int
			_, _ = fmt.Sscanf(val, "%d", &seq)
			currentEvent.Sequence = seq
		case "DTSTART":
			t, allDay, tz := parseICSDateTime(val, params)
			currentEvent.StartAt = t
			currentEvent.AllDay = allDay
			if tz != "" {
				currentEvent.Timezone = tz
			}
		case "DTEND":
			t, _, _ := parseICSDateTime(val, params)
			currentEvent.EndAt = t
		}
	}

	for i := range cal.Events {
		ev := &cal.Events[i]
		if ev.EndAt.IsZero() && !ev.StartAt.IsZero() {
			if ev.AllDay {
				ev.EndAt = ev.StartAt.AddDate(0, 0, 1)
			} else {
				ev.EndAt = ev.StartAt.Add(1 * time.Hour)
			}
		}
		if ev.Method == "CANCEL" {
			ev.Status = "cancelled"
		}
	}

	return cal, nil
}

// unfoldLines unwraps RFC 5545 folded lines (lines starting with space or tab).
func unfoldLines(data []byte) []byte {
	var buf bytes.Buffer
	lines := bytes.Split(data, []byte("\n"))

	for _, line := range lines {
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			buf.Write(line[1:])
		} else {
			if buf.Len() > 0 {
				buf.WriteByte('\n')
			}
			buf.Write(line)
		}
	}
	return buf.Bytes()
}

func unescapeText(s string) string {
	s = strings.ReplaceAll(s, `\,`, ",")
	s = strings.ReplaceAll(s, `\;`, ";")
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\N`, "\n")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

func parseICSDateTime(val string, params map[string]string) (time.Time, bool, string) {
	val = strings.TrimSpace(val)
	tzID := params["TZID"]
	valueType := strings.ToUpper(params["VALUE"])

	if valueType == "DATE" || len(val) == 8 {
		// All-day: YYYYMMDD
		t, err := time.Parse("20060102", val)
		if err == nil {
			return t, true, "UTC"
		}
	}

	if strings.HasSuffix(val, "Z") {
		// UTC: YYYYMMDDTHHMMSSZ
		t, err := time.Parse("20060102T150405Z", val)
		if err == nil {
			return t, false, "UTC"
		}
	}

	loc := time.UTC
	if tzID != "" {
		loadedLoc, err := time.LoadLocation(tzID)
		if err == nil {
			loc = loadedLoc
		}
	}

	t, err := time.ParseInLocation("20060102T150405", val, loc)
	if err == nil {
		return t, false, tzID
	}

	return time.Time{}, false, tzID
}
