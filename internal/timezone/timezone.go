// Package timezone resolves the IANA and Windows timezone identifiers commonly
// returned by calendar providers without adding a runtime dependency.
package timezone

import (
	"fmt"
	"strings"
	"time"
)

var windowsToIANA = map[string]string{
	"dateline standard time":           "Etc/GMT+12",
	"utc-11":                           "Etc/GMT+11",
	"aleutian standard time":           "America/Adak",
	"hawaiian standard time":           "Pacific/Honolulu",
	"alaskan standard time":            "America/Anchorage",
	"pacific standard time":            "America/Los_Angeles",
	"mountain standard time":           "America/Denver",
	"us mountain standard time":        "America/Phoenix",
	"central standard time":            "America/Chicago",
	"canada central standard time":     "America/Regina",
	"eastern standard time":            "America/New_York",
	"us eastern standard time":         "America/Indianapolis",
	"atlantic standard time":           "America/Halifax",
	"newfoundland standard time":       "America/St_Johns",
	"greenwich standard time":          "Atlantic/Reykjavik",
	"gmt standard time":                "Europe/London",
	"w. europe standard time":          "Europe/Berlin",
	"central europe standard time":     "Europe/Budapest",
	"romance standard time":            "Europe/Paris",
	"south africa standard time":       "Africa/Johannesburg",
	"india standard time":              "Asia/Kolkata",
	"china standard time":              "Asia/Shanghai",
	"tokyo standard time":              "Asia/Tokyo",
	"aus eastern standard time":        "Australia/Sydney",
	"new zealand standard time":        "Pacific/Auckland",
	"coordinated universal time":       "UTC",
	"utc":                              "UTC",
	"(utc) coordinated universal time": "UTC",
}

// Resolve returns a location and its canonical IANA name. It accepts quoted
// TZID values and the Windows identifiers emitted by Microsoft Graph and
// Outlook-generated iCalendar files.
func Resolve(name string) (*time.Location, string, error) {
	name = strings.Trim(strings.TrimSpace(name), `"`)
	if name == "" {
		return nil, "", fmt.Errorf("timezone name is empty")
	}
	if loc, err := time.LoadLocation(name); err == nil {
		return loc, loc.String(), nil
	}
	iana, ok := windowsToIANA[strings.ToLower(name)]
	if !ok {
		return nil, "", fmt.Errorf("unknown timezone %q", name)
	}
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return nil, "", fmt.Errorf("load mapped timezone %q for %q: %w", iana, name, err)
	}
	return loc, iana, nil
}
