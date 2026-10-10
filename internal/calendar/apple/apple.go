package apple

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/mail/ics"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

// AppleCalendarClient implements calendar.Provider for Apple iCloud calendars
// via both Apple Webcal / iCal subscription URLs and Apple iCloud CalDAV (App-Specific Passwords).
type AppleCalendarClient struct {
	caldavBaseURL string
	httpClient    *http.Client
}

// NewAppleCalendarClient creates an Apple Calendar provider client.
func NewAppleCalendarClient(caldavBaseURL string, httpClient *http.Client) *AppleCalendarClient {
	if caldavBaseURL == "" {
		caldavBaseURL = "https://caldav.icloud.com"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &AppleCalendarClient{
		caldavBaseURL: strings.TrimRight(caldavBaseURL, "/"),
		httpClient:    httpClient,
	}
}

func (c *AppleCalendarClient) Name() string {
	return "apple"
}

func isURL(str string) bool {
	s := strings.ToLower(strings.TrimSpace(str))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "webcal://")
}

func normalizeCalendarURL(raw string) string {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(s), "webcal://") {
		hostPart := s[9:]
		if strings.HasPrefix(hostPart, "127.0.0.1") || strings.HasPrefix(hostPart, "localhost") {
			return "http://" + hostPart
		}
		return "https://" + hostPart
	}
	return s
}

// ListCalendars discovers Apple calendars from either a Webcal/iCal feed or iCloud CalDAV credentials.
func (c *AppleCalendarClient) ListCalendars(ctx context.Context, accessToken string) ([]models.Calendar, error) {
	if isURL(accessToken) {
		cleanURL := normalizeCalendarURL(accessToken)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cleanURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create apple calendar feed request: %w", err)
		}
		req.Header.Set("User-Agent", fmt.Sprintf("FamilyBoard-Appliance/%s (Apple iCloud Sync)", buildinfo.Version))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch apple calendar feed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("apple calendar feed returned HTTP %d", resp.StatusCode)
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("read apple calendar feed: %w", err)
		}

		calName := "Apple Family Calendar"
		// Try extracting X-WR-CALNAME from feed header
		lines := strings.Split(string(body), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToUpper(line), "X-WR-CALNAME:") {
				calName = strings.TrimSpace(line[13:])
				break
			}
		}

		h := sha256.Sum256([]byte(cleanURL))
		calID := fmt.Sprintf("cal-apple-%x", h[:6])

		return []models.Calendar{
			{
				ID:                 calID,
				Provider:           "apple",
				ProviderCalendarID: cleanURL,
				Name:               calName,
				Timezone:           "America/Toronto",
				DisplayColor:       "#10b981", // Apple emerald
				EnabledForDisplay:  true,
				Writable:           false,
				DefaultWriteTarget: false,
			},
		}, nil
	}

	// CalDAV Basic Auth mode: credentials formatted as "username:app_password"
	parts := strings.SplitN(accessToken, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid apple credentials: expected webcal URL or username:app_password")
	}
	username, password := parts[0], parts[1]

	// Query CalDAV principal / home collections
	principalURL := fmt.Sprintf("%s/%s/principal/", c.caldavBaseURL, urlPathEscape(username))
	homeURL, err := c.discoverCalendarHome(ctx, principalURL, username, password)
	if err != nil {
		// Fallback to default user calendar path
		homeURL = fmt.Sprintf("%s/%s/calendars/", c.caldavBaseURL, urlPathEscape(username))
	}

	return c.discoverCollections(ctx, homeURL, username, password)
}

func urlPathEscape(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "%40", "@")
}

func (c *AppleCalendarClient) discoverCalendarHome(ctx context.Context, principalURL, user, pass string) (string, error) {
	propfindBody := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <C:calendar-home-set/>
  </D:prop>
</D:propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", principalURL, strings.NewReader(propfindBody))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("principal propfind failed: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Simple extraction of href inside calendar-home-set
	dataStr := string(data)
	if idx := strings.Index(dataStr, "calendar-home-set"); idx != -1 {
		sub := dataStr[idx:]
		if hStart := strings.Index(sub, "<D:href>"); hStart != -1 {
			if hEnd := strings.Index(sub[hStart+8:], "</D:href>"); hEnd != -1 {
				href := sub[hStart+8 : hStart+8+hEnd]
				if strings.HasPrefix(href, "http") {
					return href, nil
				}
				return c.caldavBaseURL + href, nil
			}
		}
	}

	return "", errors.New("no calendar-home-set found")
}

func (c *AppleCalendarClient) discoverCollections(ctx context.Context, homeURL, user, pass string) ([]models.Calendar, error) {
	propfindBody := `<?xml version="1.0" encoding="utf-8" ?>
<D:propfind xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav" xmlns:IC="http://apple.com/ns/ical/">
  <D:prop>
    <D:displayname/>
    <D:resourcetype/>
    <IC:calendar-color/>
  </D:prop>
</D:propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", homeURL, strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discover collections returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	type propstat struct {
		Prop struct {
			DisplayName  string `xml:"displayname"`
			ResourceType struct {
				Calendar *struct{} `xml:"calendar"`
			} `xml:"resourcetype"`
			CalendarColor string `xml:"calendar-color"`
		} `xml:"prop"`
	}

	type response struct {
		Href     string   `xml:"href"`
		Propstat propstat `xml:"propstat"`
	}

	type multistatus struct {
		Responses []response `xml:"response"`
	}

	var ms multistatus
	if err := xml.Unmarshal(data, &ms); err != nil {
		return nil, fmt.Errorf("decode caldav multistatus: %w", err)
	}

	var cals []models.Calendar
	for _, r := range ms.Responses {
		if r.Propstat.Prop.ResourceType.Calendar != nil {
			name := r.Propstat.Prop.DisplayName
			if name == "" {
				parts := strings.Split(strings.Trim(r.Href, "/"), "/")
				name = parts[len(parts)-1]
			}
			color := r.Propstat.Prop.CalendarColor
			if color == "" {
				color = "#10b981"
			}
			h := sha256.Sum256([]byte(r.Href))
			calID := fmt.Sprintf("cal-apple-%x", h[:6])

			cals = append(cals, models.Calendar{
				ID:                 calID,
				Provider:           "apple",
				ProviderCalendarID: r.Href,
				Name:               name,
				Timezone:           "America/Toronto",
				DisplayColor:       color,
				EnabledForDisplay:  true,
				Writable:           true,
				DefaultWriteTarget: strings.EqualFold(name, "Home") || strings.EqualFold(name, "Family"),
			})
		}
	}

	if len(cals) == 0 {
		// Provide default primary collection fallback
		cals = append(cals, models.Calendar{
			ID:                 "cal-apple-primary",
			Provider:           "apple",
			ProviderCalendarID: homeURL,
			Name:               "iCloud Calendar",
			Timezone:           "America/Toronto",
			DisplayColor:       "#10b981",
			EnabledForDisplay:  true,
			Writable:           true,
			DefaultWriteTarget: true,
		})
	}

	return cals, nil
}

// SyncEvents syncs events from either an Apple Webcal feed or Apple CalDAV server.
func (c *AppleCalendarClient) SyncEvents(ctx context.Context, accessToken string, cal models.Calendar, syncCursor string) (*calendar.SyncResult, error) {
	var body []byte
	var err error

	if isURL(cal.ProviderCalendarID) || isURL(accessToken) {
		feedURL := cal.ProviderCalendarID
		if !isURL(feedURL) {
			feedURL = accessToken
		}
		cleanURL := normalizeCalendarURL(feedURL)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, cleanURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create apple feed request: %w", err)
		}
		req.Header.Set("User-Agent", fmt.Sprintf("FamilyBoard-Appliance/%s (Apple iCloud Sync)", buildinfo.Version))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("sync apple feed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("apple calendar feed returned HTTP %d", resp.StatusCode)
		}

		body, err = io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("read apple feed: %w", err)
		}
	} else {
		// CalDAV REPORT query
		parts := strings.SplitN(accessToken, ":", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid apple credentials for sync")
		}
		user, pass := parts[0], parts[1]

		calURL := cal.ProviderCalendarID
		if !strings.HasPrefix(calURL, "http") {
			calURL = c.caldavBaseURL + calURL
		}

		timeMin := time.Now().AddDate(0, -1, 0).UTC().Format("20060102T150405Z")
		timeMax := time.Now().AddDate(1, 0, 0).UTC().Format("20060102T150405Z")

		reportXml := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" ?>
<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">
  <D:prop>
    <D:getetag/>
    <C:calendar-data/>
  </D:prop>
  <C:filter>
    <C:comp-filter name="VCALENDAR">
      <C:comp-filter name="VEVENT">
        <C:time-range start="%s" end="%s"/>
      </C:comp-filter>
    </C:comp-filter>
  </C:filter>
</C:calendar-query>`, timeMin, timeMax)

		req, err := http.NewRequestWithContext(ctx, "REPORT", calURL, strings.NewReader(reportXml))
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(user, pass)
		req.Header.Set("Depth", "1")
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("caldav report returned HTTP %d", resp.StatusCode)
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		// Extract all calendar-data blocks and concatenate
		dataStr := string(data)
		var combinedBuf bytes.Buffer
		idx := 0
		for {
			startTag := strings.Index(dataStr[idx:], "<C:calendar-data>")
			if startTag == -1 {
				startTag = strings.Index(dataStr[idx:], "<cal:calendar-data>")
			}
			if startTag == -1 {
				break
			}
			tagLen := 17
			if dataStr[idx+startTag:idx+startTag+5] == "<cal:" {
				tagLen = 19
			}
			dataStart := idx + startTag + tagLen
			endTag := strings.Index(dataStr[dataStart:], "</")
			if endTag == -1 {
				break
			}
			combinedBuf.WriteString(dataStr[dataStart : dataStart+endTag])
			combinedBuf.WriteString("\n")
			idx = dataStart + endTag
		}
		body = combinedBuf.Bytes()
	}

	// Parse raw iCalendar bytes using robust parser
	parsedCal, err := ics.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse apple ics data: %w", err)
	}

	var events []models.Event
	for _, item := range parsedCal.Events {
		uid := item.UID
		if uid == "" {
			h := sha256.Sum256(fmt.Appendf(nil, "%s-%s", item.Summary, item.StartAt))
			uid = fmt.Sprintf("apple-%x", h[:8])
		}

		status := "confirmed"
		if strings.EqualFold(item.Status, "cancelled") {
			status = "cancelled"
		}

		desc := item.Description
		loc := item.Location
		if item.URL != "" {
			if !strings.Contains(desc, item.URL) {
				if desc != "" {
					desc += "\n\nLink: " + item.URL
				} else {
					desc = item.URL
				}
			}
			if loc == "" && (strings.Contains(item.URL, "facetime.apple.com") || strings.Contains(item.URL, "meet.google.com") || strings.Contains(item.URL, "zoom.us") || strings.Contains(item.URL, "teams.microsoft.com")) {
				loc = "Online Meeting"
			}
		}

		event := models.Event{
			ID:              fmt.Sprintf("ev-apple-%s", uid),
			Provider:        "apple",
			ProviderEventID: uid,
			ProviderUID:     uid,
			CalendarID:      cal.ID,
			Title:           item.Summary,
			Description:     desc,
			Location:        loc,
			StartAt:         item.StartAt,
			EndAt:           item.EndAt,
			Timezone:        item.Timezone,
			AllDay:          item.AllDay,
			Status:          status,
			RecurrenceRule:  item.RRule,
			Organizer:       item.Organizer,
			Source:          "remote_sync",
		}
		events = append(events, event)
	}

	cursor := fmt.Sprintf("cursor-%d", time.Now().Unix())
	return &calendar.SyncResult{
		CalendarID:    cal.ID,
		SyncCursor:    cursor,
		UpsertedCount: len(events),
		Events:        events,
	}, nil
}

// CreateEvent creates an event in Apple iCloud via CalDAV.
func (c *AppleCalendarClient) CreateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event, idempotencyKey string) (*models.Event, error) {
	if isURL(calendarID) || isURL(accessToken) {
		return nil, errors.New("cannot create events in read-only Apple Webcal subscription")
	}

	parts := strings.SplitN(accessToken, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid apple caldav credentials")
	}
	user, pass := parts[0], parts[1]

	uid := event.ProviderUID
	if uid == "" {
		uid = fmt.Sprintf("fb-apple-%d@familyboard.local", time.Now().UnixNano())
	}

	calURL := calendarID
	if !strings.HasPrefix(calURL, "http") {
		calURL = c.caldavBaseURL + calURL
	}
	eventSlug := strings.TrimSuffix(uid, ".ics") + ".ics"
	eventURL := fmt.Sprintf("%s/%s", strings.TrimRight(calURL, "/"), eventSlug)

	icsPayload := formatVEventICS(uid, event)

	req, err := http.NewRequestWithContext(ctx, "PUT", eventURL, strings.NewReader(icsPayload))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("apple caldav create event failed %d: %s", resp.StatusCode, string(body))
	}

	result := *event
	result.Provider = "apple"
	result.ProviderEventID = uid
	result.ProviderUID = uid
	return &result, nil
}

// UpdateEvent updates an event in Apple iCloud via CalDAV PUT.
func (c *AppleCalendarClient) UpdateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event) (*models.Event, error) {
	if isURL(calendarID) || isURL(accessToken) {
		return nil, errors.New("cannot update events in read-only Apple Webcal subscription")
	}

	parts := strings.SplitN(accessToken, ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid apple caldav credentials")
	}
	user, pass := parts[0], parts[1]

	uid := event.ProviderEventID
	if uid == "" {
		uid = event.ProviderUID
	}

	calURL := calendarID
	if !strings.HasPrefix(calURL, "http") {
		calURL = c.caldavBaseURL + calURL
	}
	eventSlug := strings.TrimSuffix(uid, ".ics") + ".ics"
	eventURL := fmt.Sprintf("%s/%s", strings.TrimRight(calURL, "/"), eventSlug)

	icsPayload := formatVEventICS(uid, event)

	req, err := http.NewRequestWithContext(ctx, "PUT", eventURL, strings.NewReader(icsPayload))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("apple caldav update event failed %d: %s", resp.StatusCode, string(body))
	}

	result := *event
	return &result, nil
}

// DeleteEvent deletes an event in Apple iCloud via CalDAV DELETE.
func (c *AppleCalendarClient) DeleteEvent(ctx context.Context, accessToken string, calendarID string, providerEventID string) error {
	if isURL(calendarID) || isURL(accessToken) {
		return errors.New("cannot delete events in read-only Apple Webcal subscription")
	}

	parts := strings.SplitN(accessToken, ":", 2)
	if len(parts) != 2 {
		return errors.New("invalid apple caldav credentials")
	}
	user, pass := parts[0], parts[1]

	calURL := calendarID
	if !strings.HasPrefix(calURL, "http") {
		calURL = c.caldavBaseURL + calURL
	}
	eventSlug := strings.TrimSuffix(providerEventID, ".ics") + ".ics"
	eventURL := fmt.Sprintf("%s/%s", strings.TrimRight(calURL, "/"), eventSlug)

	req, err := http.NewRequestWithContext(ctx, "DELETE", eventURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("apple caldav delete failed %d: %s", resp.StatusCode, string(body))
}

func formatVEventICS(uid string, ev *models.Event) string {
	dtStamp := time.Now().UTC().Format("20060102T150405Z")
	var dtStartLine, dtEndLine string
	if ev.AllDay {
		endAt := ev.EndAt
		if !endAt.After(ev.StartAt) {
			endAt = ev.StartAt.AddDate(0, 0, 1)
		}
		dtStartLine = fmt.Sprintf("DTSTART;VALUE=DATE:%s", ev.StartAt.Format("20060102"))
		dtEndLine = fmt.Sprintf("DTEND;VALUE=DATE:%s", endAt.Format("20060102"))
	} else {
		dtStartLine = fmt.Sprintf("DTSTART:%s", ev.StartAt.UTC().Format("20060102T150405Z"))
		dtEndLine = fmt.Sprintf("DTEND:%s", ev.EndAt.UTC().Format("20060102T150405Z"))
	}

	return fmt.Sprintf("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//FamilyBoard//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:%s\r\n%s\r\n%s\r\nSUMMARY:%s\r\nDESCRIPTION:%s\r\nLOCATION:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		uid,
		dtStamp,
		dtStartLine,
		dtEndLine,
		ev.Title,
		ev.Description,
		ev.Location,
	)
}
