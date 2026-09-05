package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type ParsedEvent struct {
	Title            string
	Description      string
	Location         string
	StartAt          time.Time
	EndAt            time.Time
	Timezone         string
	AllDay           bool
	Confidence       float64
	AmbiguityReasons []string
}

type NaturalLanguageParser struct {
	defaultTZ *time.Location
}

func NewNaturalLanguageParser(timezoneName string) *NaturalLanguageParser {
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		loc, _ = time.LoadLocation("America/Toronto")
	}
	if loc == nil {
		loc = time.Local
	}
	return &NaturalLanguageParser{defaultTZ: loc}
}

var (
	// Month names regex
	monthRegex = `(?i)\b(january|jan|february|feb|march|mar|april|apr|may|june|jun|july|jul|august|aug|september|sept|sep|october|oct|november|nov|december|dec)\b`

	// Weekdays regex
	weekdayRegex = `(?i)\b(monday|mon|tuesday|tue|tues|wednesday|wed|thursday|thu|thur|thurs|friday|fri|saturday|sat|sunday|sun)\b`

	// Explicit Date pattern: Month Day (e.g. "September 18", "Sept 22", "October 2")
	monthDayRegex = regexp.MustCompile(monthRegex + `\s+(\d{1,2})(?:st|nd|rd|th)?(?:\s*,?\s*(\d{4}))?`)

	// Relative Weekday pattern: (this|next)? Weekday (e.g. "Sunday", "this Friday", "next Wednesday")
	relativeWeekdayRegex = regexp.MustCompile(`(?i)\b(?:(this|next)\s+)?` + weekdayRegex)

	// Time range pattern: e.g. "3:30-4:30", "3:30 - 4:30pm", "5:00pm - 6:00pm", "5pm-6pm"
	timeRangeRegex = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:-|to)\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b`)

	// Single time pattern: requires "at <time>", or ":MM", or "am/pm", or "noon/midnight"
	// Prevents bare date numbers like "2" or "22" from being interpreted as hours!
	singleTimeRegex = regexp.MustCompile(`(?i)(?:\b(noon|midnight)\b|\bat\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b|\b(\d{1,2}):(\d{2})\s*(am|pm)?\b|\b(\d{1,2})\s*(am|pm)\b)`)

	// Location pattern: e.g. "at Community Centre", "in Room 204"
	locationRegex = regexp.MustCompile(`(?i)\b(?:at|in)\s+([A-Z][A-Za-z0-9\s,.-]+?)(?:\s+(?:on|from|starting|\.|\n|$)|$)`)
)

func (p *NaturalLanguageParser) Parse(text, referenceSubject string, now time.Time) (*ParsedEvent, error) {
	if now.IsZero() {
		now = time.Now().In(p.defaultTZ)
	} else {
		now = now.In(p.defaultTZ)
	}

	fullText := text
	if referenceSubject != "" {
		fullText = referenceSubject + " " + text
	}

	result := &ParsedEvent{
		Timezone:   p.defaultTZ.String(),
		Confidence: 0.0,
	}

	// 1. Check for explicit Date (Month + Day)
	mMatch := monthDayRegex.FindStringSubmatchIndex(fullText)
	var eventDate time.Time
	hasExplicitDate := false
	hasRelativeWeekday := false

	if len(mMatch) > 0 {
		monthStr := fullText[mMatch[2]:mMatch[3]]
		dayStr := fullText[mMatch[4]:mMatch[5]]
		yearStr := ""
		if mMatch[6] != -1 && mMatch[7] != -1 {
			yearStr = fullText[mMatch[6]:mMatch[7]]
		}

		month := parseMonth(monthStr)
		day, _ := strconv.Atoi(dayStr)
		year := now.Year()
		if yearStr != "" {
			year, _ = strconv.Atoi(yearStr)
		} else {
			// Year rollover check: if month is in the past (> 2 months ago), assume next year
			if int(month) < int(now.Month())-2 {
				year++
			}
		}

		eventDate = time.Date(year, month, day, 0, 0, 0, 0, p.defaultTZ)
		hasExplicitDate = true
	} else {
		// 2. Check for relative weekday (e.g. "Sunday at noon")
		wMatch := relativeWeekdayRegex.FindStringSubmatch(fullText)
		if len(wMatch) > 0 {
			prefix := strings.ToLower(wMatch[1])
			weekdayStr := strings.ToLower(wMatch[2])
			targetWd := parseWeekday(weekdayStr)

			eventDate = calculateNextWeekday(now, targetWd, prefix == "next")
			hasRelativeWeekday = true
		}
	}

	if !hasExplicitDate && !hasRelativeWeekday {
		result.Confidence = 0.2
		result.AmbiguityReasons = append(result.AmbiguityReasons, "no identifiable date or weekday found")
		return result, nil
	}

	// 3. Check for Time
	hasTimeRange := false
	hasSingleTime := false
	var startTime, endTime time.Time

	trMatch := timeRangeRegex.FindStringSubmatch(fullText)
	if len(trMatch) > 0 {
		startHour, _ := strconv.Atoi(trMatch[1])
		startMin := 0
		if trMatch[2] != "" {
			startMin, _ = strconv.Atoi(trMatch[2])
		}
		startMeridiem := strings.ToLower(trMatch[3])

		endHour, _ := strconv.Atoi(trMatch[4])
		endMin := 0
		if trMatch[5] != "" {
			endMin, _ = strconv.Atoi(trMatch[5])
		}
		endMeridiem := strings.ToLower(trMatch[6])

		// If start meridiem is missing, infer from end meridiem
		if startMeridiem == "" && endMeridiem != "" {
			startMeridiem = endMeridiem
			if startHour > endHour && endMeridiem == "pm" && startHour != 12 {
				startMeridiem = "am"
			}
		} else if startMeridiem == "" && endMeridiem == "" {
			// Both missing: e.g. 3:30-4:30
			if startHour >= 1 && startHour <= 7 {
				startMeridiem = "pm"
				endMeridiem = "pm"
			} else {
				startMeridiem = "am"
				endMeridiem = "am"
			}
			result.AmbiguityReasons = append(result.AmbiguityReasons, fmt.Sprintf("inferred %s for time range %d:%02d-%d:%02d", startMeridiem, startHour, startMin, endHour, endMin))
		}

		startHour = normalize12Hour(startHour, startMeridiem)
		endHour = normalize12Hour(endHour, endMeridiem)

		startTime = time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), startHour, startMin, 0, 0, p.defaultTZ)
		endTime = time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), endHour, endMin, 0, 0, p.defaultTZ)
		hasTimeRange = true
	} else {
		// Single time or noon/midnight
		stMatch := singleTimeRegex.FindStringSubmatch(fullText)
		if len(stMatch) > 0 {
			special := strings.ToLower(stMatch[1])
			if special == "noon" {
				startTime = time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), 12, 0, 0, 0, p.defaultTZ)
				endTime = startTime.Add(1 * time.Hour)
				hasSingleTime = true
			} else if special == "midnight" {
				startTime = time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), 0, 0, 0, 0, p.defaultTZ)
				endTime = startTime.Add(1 * time.Hour)
				hasSingleTime = true
			} else {
				var h, m int
				var meridiem string
				if stMatch[2] != "" { // at H:M am/pm
					h, _ = strconv.Atoi(stMatch[2])
					if stMatch[3] != "" {
						m, _ = strconv.Atoi(stMatch[3])
					}
					meridiem = strings.ToLower(stMatch[4])
				} else if stMatch[5] != "" { // H:M am/pm
					h, _ = strconv.Atoi(stMatch[5])
					m, _ = strconv.Atoi(stMatch[6])
					meridiem = strings.ToLower(stMatch[7])
				} else if stMatch[8] != "" { // H am/pm
					h, _ = strconv.Atoi(stMatch[8])
					meridiem = strings.ToLower(stMatch[9])
				}

				if h > 0 || m > 0 || meridiem != "" {
					if meridiem == "" {
						if h >= 1 && h <= 7 {
							meridiem = "pm"
						} else {
							meridiem = "am"
						}
						result.AmbiguityReasons = append(result.AmbiguityReasons, fmt.Sprintf("inferred %s for ambiguous time %d:%02d", meridiem, h, m))
					}
					h = normalize12Hour(h, meridiem)
					startTime = time.Date(eventDate.Year(), eventDate.Month(), eventDate.Day(), h, m, 0, 0, p.defaultTZ)
					endTime = startTime.Add(1 * time.Hour)
					hasSingleTime = true
				}
			}
		}
	}

	if hasTimeRange || hasSingleTime {
		result.StartAt = startTime
		result.EndAt = endTime
		result.AllDay = false
	} else {
		// Explicit date with no time is an all-day event
		result.StartAt = eventDate
		result.EndAt = eventDate.AddDate(0, 0, 1)
		result.AllDay = true
	}

	// 4. Extract Location if present
	locMatch := locationRegex.FindStringSubmatch(fullText)
	if len(locMatch) > 1 {
		locCandidate := strings.TrimSpace(locMatch[1])
		// Verify it's not a day or time word
		if !strings.EqualFold(locCandidate, "noon") && !strings.EqualFold(locCandidate, "midnight") {
			result.Location = locCandidate
		}
	}

	// 5. Clean Title extraction
	title := cleanTitle(fullText)
	if title == "" && referenceSubject != "" {
		title = referenceSubject
	}
	result.Title = title

	// 6. Compute confidence score
	confidence := 0.5
	if hasExplicitDate {
		confidence += 0.3
	} else if hasRelativeWeekday {
		confidence += 0.2
	}

	if hasTimeRange {
		confidence += 0.2
	} else if hasSingleTime {
		confidence += 0.15
	} else if result.AllDay {
		confidence += 0.1
	}

	if result.Location != "" {
		confidence += 0.05
	}

	if len(result.AmbiguityReasons) > 0 {
		confidence -= 0.15 * float64(len(result.AmbiguityReasons))
	}

	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < 0.0 {
		confidence = 0.0
	}
	result.Confidence = confidence

	return result, nil
}

func parseMonth(s string) time.Month {
	s = strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "jan"):
		return time.January
	case strings.HasPrefix(s, "feb"):
		return time.February
	case strings.HasPrefix(s, "mar"):
		return time.March
	case strings.HasPrefix(s, "apr"):
		return time.April
	case s == "may":
		return time.May
	case strings.HasPrefix(s, "jun"):
		return time.June
	case strings.HasPrefix(s, "jul"):
		return time.July
	case strings.HasPrefix(s, "aug"):
		return time.August
	case strings.HasPrefix(s, "sep"):
		return time.September
	case strings.HasPrefix(s, "oct"):
		return time.October
	case strings.HasPrefix(s, "nov"):
		return time.November
	case strings.HasPrefix(s, "dec"):
		return time.December
	}
	return time.January
}

func parseWeekday(s string) time.Weekday {
	s = strings.ToLower(s)
	switch {
	case strings.HasPrefix(s, "mon"):
		return time.Monday
	case strings.HasPrefix(s, "tue"):
		return time.Tuesday
	case strings.HasPrefix(s, "wed"):
		return time.Wednesday
	case strings.HasPrefix(s, "thu"):
		return time.Thursday
	case strings.HasPrefix(s, "fri"):
		return time.Friday
	case strings.HasPrefix(s, "sat"):
		return time.Saturday
	case strings.HasPrefix(s, "sun"):
		return time.Sunday
	}
	return time.Sunday
}

func calculateNextWeekday(now time.Time, targetWd time.Weekday, mustBeNextWeek bool) time.Time {
	daysAhead := int(targetWd) - int(now.Weekday())
	if daysAhead < 0 || (daysAhead == 0 && mustBeNextWeek) {
		daysAhead += 7
	}
	if mustBeNextWeek && daysAhead < 7 {
		daysAhead += 7
	}
	targetDate := now.AddDate(0, 0, daysAhead)
	return time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, now.Location())
}

func normalize12Hour(hour int, meridiem string) int {
	meridiem = strings.ToLower(meridiem)
	if meridiem == "pm" && hour < 12 {
		return hour + 12
	}
	if meridiem == "am" && hour == 12 {
		return 0
	}
	return hour
}

var stripDateJunkRegex = regexp.MustCompile(`(?i)(?:on|at|from|starts?|scheduled for)\s+` + monthRegex + `\s+\d{1,2}.*`)

func cleanTitle(raw string) string {
	cleaned := strings.Split(raw, "\n")[0]
	cleaned = stripDateJunkRegex.ReplaceAllString(cleaned, "")
	cleaned = strings.TrimSpace(cleaned)
	if len(cleaned) > 80 {
		cleaned = cleaned[:80]
	}
	return cleaned
}
