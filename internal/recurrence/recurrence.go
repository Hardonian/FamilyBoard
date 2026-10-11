// Package recurrence expands a practical RFC 5545 RRULE subset into bounded
// occurrence lists. It is wall-clock based: an event at 09:00 local stays at
// 09:00 local across DST changes, instead of drifting by an hour.
//
// Supported: FREQ=DAILY|WEEKLY|MONTHLY|YEARLY, INTERVAL, COUNT, UNTIL,
// BYDAY (weekly), plus caller-supplied exclusions (EXDATE / cancelled
// instances). Unsupported rules return an error rather than silently
// producing wrong occurrences.
package recurrence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaxOccurrences hard-bounds one expansion to protect memory and rendering.
const MaxOccurrences = 1000

// Rule is a parsed recurrence rule.
type Rule struct {
	Freq     string
	Interval int
	Count    int
	Until    time.Time
	ByDay    []time.Weekday
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// Parse parses an RRULE string, with or without the "RRULE:" prefix.
func Parse(s string) (Rule, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "RRULE:")
	r := Rule{Interval: 1}
	if s == "" {
		return r, fmt.Errorf("empty recurrence rule")
	}
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return r, fmt.Errorf("malformed rule part %q", part)
		}
		key, val := strings.ToUpper(kv[0]), kv[1]
		switch key {
		case "FREQ":
			switch strings.ToUpper(val) {
			case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
				r.Freq = strings.ToUpper(val)
			default:
				return r, fmt.Errorf("unsupported FREQ %q", val)
			}
		case "INTERVAL":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return r, fmt.Errorf("invalid INTERVAL %q", val)
			}
			r.Interval = n
		case "COUNT":
			n, err := strconv.Atoi(val)
			if err != nil || n < 1 {
				return r, fmt.Errorf("invalid COUNT %q", val)
			}
			r.Count = n
		case "UNTIL":
			t, err := parseUntil(val)
			if err != nil {
				return r, err
			}
			r.Until = t
		case "BYDAY":
			for _, code := range strings.Split(val, ",") {
				wd, ok := weekdayCodes[strings.ToUpper(code)]
				if !ok {
					return r, fmt.Errorf("unsupported BYDAY %q", code)
				}
				r.ByDay = append(r.ByDay, wd)
			}
		case "WKST":
			// accepted; week start does not alter this subset.
		default:
			return r, fmt.Errorf("unsupported rule part %s", key)
		}
	}
	if r.Freq == "" {
		return r, fmt.Errorf("missing FREQ")
	}
	if len(r.ByDay) > 0 && r.Freq != "WEEKLY" {
		return r, fmt.Errorf("BYDAY is only supported with FREQ=WEEKLY")
	}
	return r, nil
}

func parseUntil(v string) (time.Time, error) {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102"} {
		if t, err := time.Parse(layout, v); err == nil {
			if layout == "20060102" {
				t = t.Add(24*time.Hour - time.Second)
			}
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid UNTIL %q", v)
}

// Expand returns occurrence start times that fall inside [windowStart,
// windowEnd), computed in loc using wall-clock arithmetic from dtstart.
// excluded holds occurrence starts (compared at second precision) to skip.
// Results are capped at MaxOccurrences.
func Expand(r Rule, dtstart time.Time, loc *time.Location, windowStart, windowEnd time.Time, excluded []time.Time) []time.Time {
	if loc == nil {
		loc = time.UTC
	}
	start := dtstart.In(loc)
	skip := make(map[int64]struct{}, len(excluded))
	skipDate := make(map[string]struct{}, len(excluded))
	for _, ex := range excluded {
		skip[ex.Unix()] = struct{}{}
		exLoc := ex.In(loc)
		skipDate[fmt.Sprintf("%04d-%02d-%02d", exLoc.Year(), exLoc.Month(), exLoc.Day())] = struct{}{}
	}

	var out []time.Time
	produced := 0 // counts every generated occurrence, for COUNT semantics

	emit := func(t time.Time) bool {
		if !r.Until.IsZero() && t.After(r.Until) {
			return false
		}
		if r.Count > 0 && produced >= r.Count {
			return false
		}
		produced++
		tLoc := t.In(loc)
		dateKey := fmt.Sprintf("%04d-%02d-%02d", tLoc.Year(), tLoc.Month(), tLoc.Day())
		_, isUnixEx := skip[t.Unix()]
		_, isDateEx := skipDate[dateKey]
		if !isUnixEx && !isDateEx && !t.Before(windowStart) && t.Before(windowEnd) {
			out = append(out, t)
		}
		return len(out) < MaxOccurrences
	}

	at := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, start.Hour(), start.Minute(), start.Second(), 0, loc)
	}

	// Iteration guard: never loop more than needed to pass the window.
	const maxSteps = 100000
	switch r.Freq {
	case "DAILY":
		for i := 0; i < maxSteps; i++ {
			t := at(start.Year(), start.Month(), start.Day()+i*r.Interval)
			if !t.Before(windowEnd) || !emit(t) {
				break
			}
		}
	case "WEEKLY":
		days := r.ByDay
		if len(days) == 0 {
			days = []time.Weekday{start.Weekday()}
		}
		weekStart := at(start.Year(), start.Month(), start.Day()-int(start.Weekday()))
		for w := 0; w < maxSteps; w++ {
			base := weekStart.AddDate(0, 0, 7*w*r.Interval)
			if !base.Before(windowEnd) {
				break
			}
			stop := false
			for wd := time.Sunday; wd <= time.Saturday; wd++ {
				if !containsDay(days, wd) {
					continue
				}
				t := at(base.Year(), base.Month(), base.Day()+int(wd))
				if t.Before(start) {
					continue
				}
				if !emit(t) {
					stop = true
					break
				}
			}
			if stop {
				break
			}
		}
	case "MONTHLY":
		for i := 0; i < maxSteps; i++ {
			y, m := addMonths(start.Year(), start.Month(), i*r.Interval)
			if start.Day() > daysIn(y, m) {
				continue // e.g. the 31st skips short months, per RFC 5545
			}
			t := at(y, m, start.Day())
			if !t.Before(windowEnd) || !emit(t) {
				break
			}
		}
	case "YEARLY":
		for i := 0; i < maxSteps; i++ {
			y := start.Year() + i*r.Interval
			if start.Day() > daysIn(y, start.Month()) {
				continue // Feb 29 only in leap years
			}
			t := at(y, start.Month(), start.Day())
			if !t.Before(windowEnd) || !emit(t) {
				break
			}
		}
	}
	return out
}

func containsDay(days []time.Weekday, d time.Weekday) bool {
	for _, x := range days {
		if x == d {
			return true
		}
	}
	return false
}

func addMonths(y int, m time.Month, n int) (int, time.Month) {
	total := int(m) - 1 + n
	return y + total/12, time.Month(total%12 + 1)
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
