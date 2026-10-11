// Package releasefeed performs bounded, read-only checks against the official
// FamilyBoard GitHub release feed. It never downloads or installs binaries.
package releasefeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	OfficialEndpoint = "https://api.github.com/repos/Hardonian/FamilyBoard/releases?per_page=30"
	maxResponseBytes = 2 << 20
)

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

type Result struct {
	Installed       string    `json:"installed"`
	Latest          string    `json:"latest"`
	Channel         string    `json:"channel"`
	UpdateAvailable bool      `json:"update_available"`
	ReleaseURL      string    `json:"release_url,omitempty"`
	ReleaseName     string    `json:"release_name,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	PublishedAt     time.Time `json:"published_at,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
}

func Check(ctx context.Context, client *http.Client, endpoint, installed, channel string) (Result, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		channel = "stable"
	}
	if channel != "stable" && channel != "beta" && channel != "dev" {
		return Result{}, fmt.Errorf("unsupported release channel %q", channel)
	}
	if client == nil {
		return Result{}, errors.New("release feed HTTP client is required")
	}
	if endpoint == "" {
		endpoint = OfficialEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, fmt.Errorf("create release feed request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FamilyBoard/"+strings.TrimPrefix(installed, "v"))
	response, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("check release feed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return Result{}, fmt.Errorf("release feed returned HTTP %d", response.StatusCode)
	}
	var releases []Release
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes+1))
	if err := decoder.Decode(&releases); err != nil {
		return Result{}, fmt.Errorf("decode release feed: %w", err)
	}
	if len(releases) > 100 {
		return Result{}, errors.New("release feed exceeds item limit")
	}

	var latest *Release
	for i := range releases {
		release := &releases[i]
		if release.Draft || !validVersion(release.TagName) {
			continue
		}
		if channel == "stable" && release.Prerelease {
			continue
		}
		if latest == nil || compareVersions(release.TagName, latest.TagName) > 0 {
			latest = release
		}
	}
	if latest == nil {
		return Result{}, fmt.Errorf("release feed contains no %s releases", channel)
	}

	notes := strings.TrimSpace(latest.Body)
	if len(notes) > 4000 {
		notes = notes[:4000] + "…"
	}
	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	installed = strings.TrimPrefix(strings.TrimSpace(installed), "v")
	return Result{
		Installed: installed, Latest: latestVersion, Channel: channel,
		UpdateAvailable: compareVersions(latestVersion, installed) > 0,
		ReleaseURL: latest.HTMLURL, ReleaseName: latest.Name, Notes: notes,
		PublishedAt: latest.PublishedAt, CheckedAt: time.Now().UTC(),
	}, nil
}

func validVersion(value string) bool {
	_, _, _, _, ok := parseVersion(value)
	return ok
}

func compareVersions(left, right string) int {
	lMajor, lMinor, lPatch, lPre, lOK := parseVersion(left)
	rMajor, rMinor, rPatch, rPre, rOK := parseVersion(right)
	if !lOK && !rOK {
		return 0
	}
	if !lOK {
		return -1
	}
	if !rOK {
		return 1
	}
	for _, pair := range [][2]int{{lMajor, rMajor}, {lMinor, rMinor}, {lPatch, rPatch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if lPre == rPre {
		return 0
	}
	if lPre == "" {
		return 1
	}
	if rPre == "" {
		return -1
	}
	return comparePrerelease(lPre, rPre)
}

func parseVersion(value string) (int, int, int, string, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	base, pre, _ := strings.Cut(value, "-")
	parts := strings.Split(base, ".")
	if len(parts) != 3 {
		return 0, 0, 0, "", false
	}
	parsed := make([]int, 3)
	for i := range parts {
		if parts[i] == "" || (len(parts[i]) > 1 && parts[i][0] == '0') {
			return 0, 0, 0, "", false
		}
		value, err := strconv.Atoi(parts[i])
		if err != nil || value < 0 {
			return 0, 0, 0, "", false
		}
		parsed[i] = value
	}
	return parsed[0], parsed[1], parsed[2], pre, true
}

func comparePrerelease(left, right string) int {
	lParts, rParts := strings.Split(left, "."), strings.Split(right, ".")
	for i := 0; i < len(lParts) && i < len(rParts); i++ {
		if lParts[i] == rParts[i] {
			continue
		}
		lNumber, lErr := strconv.Atoi(lParts[i])
		rNumber, rErr := strconv.Atoi(rParts[i])
		if lErr == nil && rErr == nil {
			if lNumber < rNumber {
				return -1
			}
			return 1
		}
		if lErr == nil {
			return -1
		}
		if rErr == nil {
			return 1
		}
		return strings.Compare(lParts[i], rParts[i])
	}
	if len(lParts) < len(rParts) {
		return -1
	}
	if len(lParts) > len(rParts) {
		return 1
	}
	return 0
}
