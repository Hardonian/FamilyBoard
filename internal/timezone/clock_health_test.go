package timezone

import (
	"testing"
)

func TestCheckClockHealth_Valid(t *testing.T) {
	health := CheckClockHealth("America/New_York")
	if health.Status != "healthy" {
		t.Errorf("expected healthy status, got %s (details: %s)", health.Status, health.Details)
	}
	if !health.TimezoneValid {
		t.Error("expected valid timezone")
	}
	if !health.YearSane {
		t.Error("expected sane year")
	}
}

func TestCheckClockHealth_InvalidTimezone(t *testing.T) {
	health := CheckClockHealth("Invalid/NonExistent_Zone_12345")
	if health.Status != "warning" {
		t.Errorf("expected warning status for invalid tz, got %s", health.Status)
	}
	if health.TimezoneValid {
		t.Error("expected timezone to be invalid")
	}
}
