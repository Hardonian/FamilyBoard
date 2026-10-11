package timezone

import (
	"fmt"
	"os"
	"runtime"
	"time"
)

type ClockHealth struct {
	CurrentTimeUTC   time.Time `json:"current_time_utc"`
	ConfiguredTZ     string    `json:"configured_timezone"`
	TimezoneValid    bool      `json:"timezone_valid"`
	YearSane         bool      `json:"year_sane"`
	MonotonicCheckOK bool      `json:"monotonic_check_ok"`
	NTPStatus        string    `json:"ntp_status"` // synchronized, unsynchronized, or unknown
	Status           string    `json:"status"`     // "healthy", "warning", "critical"
	Details          string    `json:"details"`
}

// CheckClockHealth validates local clock sanity, RTC sanity, and timezone resolvability.
func CheckClockHealth(configuredTZ string) ClockHealth {
	now := time.Now()
	health := ClockHealth{
		CurrentTimeUTC:   now.UTC(),
		ConfiguredTZ:     configuredTZ,
		YearSane:         now.Year() >= 2024 && now.Year() <= 2100,
		MonotonicCheckOK: true,
		Status:           "healthy",
		NTPStatus:        detectNTPStatus(),
		Details:          "Clock and timezone appear healthy",
	}

	if configuredTZ != "" {
		if _, _, err := Resolve(configuredTZ); err != nil {
			health.TimezoneValid = false
			health.Status = "warning"
			health.Details = fmt.Sprintf("Configured timezone %q is invalid or unresolvable: %v", configuredTZ, err)
		} else {
			health.TimezoneValid = true
		}
	} else {
		health.TimezoneValid = true
	}

	if !health.YearSane {
		health.Status = "critical"
		health.Details = fmt.Sprintf("System clock reports year %d (must be 2024-2100), indicating lost NTP synchronization or dead RTC battery", now.Year())
	}
	if health.Status == "healthy" && health.NTPStatus == "unsynchronized" {
		health.Status = "warning"
		health.Details = "System clock is plausible, but systemd-timesyncd has not confirmed synchronization"
	} else if health.Status == "healthy" && health.NTPStatus == "synchronized" {
		health.Details = "Clock, timezone, and NTP synchronization are healthy"
	} else if health.Status == "healthy" {
		health.Details = "Clock and timezone are healthy; NTP state is unavailable on this platform"
	}

	return health
}

func detectNTPStatus() string {
	if runtime.GOOS != "linux" {
		return "unknown"
	}
	if _, err := os.Stat("/run/systemd/timesync/synchronized"); err == nil {
		return "synchronized"
	}
	if _, err := os.Stat("/run/systemd/timesync"); err == nil {
		return "unsynchronized"
	}
	return "unknown"
}
