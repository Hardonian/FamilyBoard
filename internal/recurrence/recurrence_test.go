package recurrence

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("tz database unavailable: %v", err)
	}
	return loc
}

func TestParse_RejectsUnsupportedAndMalformed(t *testing.T) {
	bad := []string{"", "FREQ=HOURLY", "FREQ=DAILY;BYSETPOS=1", "FREQ=MONTHLY;BYDAY=MO", "INTERVAL=2", "FREQ=DAILY;COUNT=0", "FREQ"}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) should fail", s)
		}
	}
	if _, err := Parse("RRULE:FREQ=WEEKLY;BYDAY=MO,WE;INTERVAL=2;WKST=SU"); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
}

func TestExpand_DailyKeepsWallClockAcrossDST(t *testing.T) {
	loc := mustLoc(t, "America/Toronto")
	// 2026 DST starts Sun Mar 8. 09:00 local must remain 09:00 afterwards.
	start := time.Date(2026, 3, 6, 9, 0, 0, 0, loc)
	r, _ := Parse("FREQ=DAILY;COUNT=5")
	got := Expand(r, start, loc, start, start.AddDate(0, 0, 10), nil)
	if len(got) != 5 {
		t.Fatalf("want 5 occurrences, got %d", len(got))
	}
	for _, g := range got {
		if g.In(loc).Hour() != 9 {
			t.Fatalf("occurrence drifted off 09:00 local: %v", g.In(loc))
		}
	}
}

func TestExpand_WeeklyByDayAndInterval(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 5, 18, 0, 0, 0, loc) // Monday
	r, _ := Parse("FREQ=WEEKLY;BYDAY=MO,WE;INTERVAL=2;COUNT=4")
	got := Expand(r, start, loc, start, start.AddDate(0, 3, 0), nil)
	want := []time.Time{
		time.Date(2026, 1, 5, 18, 0, 0, 0, loc),
		time.Date(2026, 1, 7, 18, 0, 0, 0, loc),
		time.Date(2026, 1, 19, 18, 0, 0, 0, loc),
		time.Date(2026, 1, 21, 18, 0, 0, 0, loc),
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("occurrence %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestExpand_MonthlyThirtyFirstSkipsShortMonths(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 31, 8, 0, 0, 0, loc)
	r, _ := Parse("FREQ=MONTHLY;COUNT=3")
	got := Expand(r, start, loc, start, start.AddDate(1, 0, 0), nil)
	// Jan 31, Mar 31, May 31 (Feb/Apr skipped)
	if len(got) != 3 || got[1].Month() != time.March || got[2].Month() != time.May {
		t.Fatalf("unexpected monthly expansion: %v", got)
	}
}

func TestExpand_YearlyLeapDay(t *testing.T) {
	loc := time.UTC
	start := time.Date(2024, 2, 29, 12, 0, 0, 0, loc)
	r, _ := Parse("FREQ=YEARLY;COUNT=2")
	got := Expand(r, start, loc, start, start.AddDate(10, 0, 0), nil)
	if len(got) != 2 || got[1].Year() != 2028 {
		t.Fatalf("leap day should recur in 2028: %v", got)
	}
}

func TestExpand_ExclusionsUntilAndWindow(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 6, 1, 7, 30, 0, 0, loc)
	r, _ := Parse("FREQ=DAILY;UNTIL=20260610")
	skip := []time.Time{start.AddDate(0, 0, 2)}
	got := Expand(r, start, loc, start.AddDate(0, 0, 1), start.AddDate(0, 0, 5), skip)
	// Window days 2..5 minus excluded day 3 => days 2,4,5 (indices 1,3,4)
	if len(got) != 3 {
		t.Fatalf("expected 3 occurrences, got %v", got)
	}
	all := Expand(r, start, loc, start, start.AddDate(1, 0, 0), nil)
	if len(all) != 10 {
		t.Fatalf("UNTIL should bound to 10 days, got %d", len(all))
	}
}

func TestExpand_BoundedByMaxOccurrences(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
	r, _ := Parse("FREQ=DAILY")
	got := Expand(r, start, loc, start, start.AddDate(50, 0, 0), nil)
	if len(got) > MaxOccurrences {
		t.Fatalf("expansion exceeded bound: %d", len(got))
	}
}
