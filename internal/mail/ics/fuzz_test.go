package ics_test

import (
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/mail/ics"
)

func FuzzICSParse(f *testing.F) {
	// Seed with valid and boundary inputs
	f.Add([]byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nSUMMARY:Test\r\nDTSTART:20261011T120000Z\r\nDTEND:20261011T130000Z\r\nEND:VEVENT\r\nEND:VCALENDAR"))
	f.Add([]byte(""))
	f.Add([]byte("BEGIN:VCALENDAR\nMETHOD:CANCEL\nEND:VCALENDAR"))
	f.Add([]byte("BEGIN:VEVENT\nSUMMARY:Broken\nDTSTART;TZID=America/Toronto:20261011T120000\nEND:VEVENT"))
	f.Add([]byte("INVALID DATA\x00\xff\xfe"))

	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic on arbitrary input
		_, _ = ics.Parse(data)
	})
}
