package microsoft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

var (
	ErrRateLimited = errors.New("microsoft graph rate limit exceeded (429)")
	ErrNotFound    = errors.New("microsoft calendar or event not found (404)")
)

// CalendarClient implements calendar.Provider for Microsoft 365, Exchange Online, and Outlook.com.
type CalendarClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewCalendarClient creates a new Microsoft Exchange/Graph calendar client.
func NewCalendarClient(baseURL string, httpClient *http.Client) *CalendarClient {
	if baseURL == "" {
		baseURL = "https://graph.microsoft.com/v1.0/me"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &CalendarClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *CalendarClient) Name() string {
	return "microsoft"
}

// executeWithRetry executes an HTTP request with exponential backoff for 429 and 5xx.
func (c *CalendarClient) executeWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	maxRetries := 3
	backoff := 500 * time.Millisecond

	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(bodyBytes)), nil
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries || ctx.Err() != nil {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff + time.Duration(rand.Intn(100))*time.Millisecond):
				backoff *= 2
				continue
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode >= 500 && resp.StatusCode <= 599) {
			resp.Body.Close()
			if attempt == maxRetries {
				if resp.StatusCode == http.StatusTooManyRequests {
					return nil, ErrRateLimited
				}
				return nil, fmt.Errorf("microsoft graph server error %d", resp.StatusCode)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff + time.Duration(rand.Intn(100))*time.Millisecond):
				backoff *= 2
				continue
			}
		}

		return resp, nil
	}

	return nil, errors.New("exhausted retry attempts")
}

type msCalendarListResponse struct {
	Value []struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Color             string `json:"color"`
		HexColor          string `json:"hexColor"`
		IsDefaultCalendar bool   `json:"isDefaultCalendar"`
		CanEdit           bool   `json:"canEdit"`
		Owner             struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"owner"`
	} `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

func (c *CalendarClient) ListCalendars(ctx context.Context, accessToken string) ([]models.Calendar, error) {
	var calendars []models.Calendar
	endpoint := fmt.Sprintf("%s/calendars", c.baseURL)

	for endpoint != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("create list calendars request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.executeWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list calendars request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("list calendars failed status %d: %s", resp.StatusCode, string(body))
		}

		var listResp msCalendarListResponse
		if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode calendar list: %w", err)
		}
		resp.Body.Close()

		for _, item := range listResp.Value {
			color := item.HexColor
			if color == "" {
				color = "#0078d4" // Microsoft blue
			}
			calID := fmt.Sprintf("cal-ms-%s", item.ID)

			calendars = append(calendars, models.Calendar{
				ID:                 calID,
				Provider:           "microsoft",
				ProviderCalendarID: item.ID,
				Name:               item.Name,
				Timezone:           "America/Toronto",
				DisplayColor:       color,
				EnabledForDisplay:  true,
				Writable:           item.CanEdit,
				DefaultWriteTarget: item.IsDefaultCalendar,
			})
		}

		endpoint = listResp.NextLink
	}

	return calendars, nil
}

type msEventsResponse struct {
	Value []struct {
		ID          string `json:"id"`
		ICalUID     string `json:"iCalUId"`
		Subject     string `json:"subject"`
		Body        struct {
			ContentType string `json:"contentType"`
			Content     string `json:"content"`
		} `json:"body"`
		Location struct {
			DisplayName string `json:"displayName"`
		} `json:"location"`
		Start struct {
			DateTime string `json:"dateTime"`
			TimeZone string `json:"timeZone"`
		} `json:"start"`
		End struct {
			DateTime string `json:"dateTime"`
			TimeZone string `json:"timeZone"`
		} `json:"end"`
		IsAllDay    bool   `json:"isAllDay"`
		IsCancelled bool   `json:"isCancelled"`
		ChangeKey        string `json:"changeKey"`
		IsOnlineMeeting  bool   `json:"isOnlineMeeting"`
		OnlineMeetingURL string `json:"onlineMeetingUrl"`
		OnlineMeeting    *struct {
			JoinURL string `json:"joinUrl"`
		} `json:"onlineMeeting"`
		Organizer struct {
			EmailAddress struct {
				Name    string `json:"name"`
				Address string `json:"address"`
			} `json:"emailAddress"`
		} `json:"organizer"`
		Recurrence *struct {
			Pattern struct {
				Type     string `json:"type"`
				Interval int    `json:"interval"`
			} `json:"pattern"`
		} `json:"recurrence"`
	} `json:"value"`
	NextLink  string `json:"@odata.nextLink"`
	DeltaLink string `json:"@odata.deltaLink"`
}

func parseMSTime(dtStr, tzStr string) (time.Time, error) {
	// Graph returns dates like "2026-10-10T14:00:00.0000000" or RFC3339
	if t, err := time.Parse(time.RFC3339, dtStr); err == nil {
		return t, nil
	}
	formats := []string{
		"2006-01-02T15:04:05.9999999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	loc := time.UTC
	if tzStr != "" {
		if l, err := time.LoadLocation(tzStr); err == nil {
			loc = l
		}
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, dtStr, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time format: %s", dtStr)
}

func (c *CalendarClient) SyncEvents(ctx context.Context, accessToken string, cal models.Calendar, syncCursor string) (*calendar.SyncResult, error) {
	var allEvents []models.Event
	nextCursor := ""

	endpoint := fmt.Sprintf("%s/calendars/%s/events", c.baseURL, url.PathEscape(cal.ProviderCalendarID))
	if syncCursor != "" && strings.HasPrefix(syncCursor, "http") {
		endpoint = syncCursor
	} else {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		q := u.Query()
		q.Set("$top", "250")
		// Filter default: past 30 days to 1 year ahead
		timeMin := time.Now().AddDate(0, -1, 0).UTC().Format(time.RFC3339)
		timeMax := time.Now().AddDate(1, 0, 0).UTC().Format(time.RFC3339)
		q.Set("$filter", fmt.Sprintf("start/dateTime ge '%s' and end/dateTime le '%s'", timeMin, timeMax))
		q.Set("$select", "id,iCalUId,subject,body,location,start,end,isAllDay,isCancelled,changeKey,organizer,recurrence")
		u.RawQuery = q.Encode()
		endpoint = u.String()
	}

	for endpoint != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Prefer", `outlook.timezone="UTC"`)

		resp, err := c.executeWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusGone {
			resp.Body.Close()
			return nil, errors.New("microsoft delta token expired (410)")
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("sync events failed status %d: %s", resp.StatusCode, string(body))
		}

		var pageResp msEventsResponse
		if err := json.NewDecoder(resp.Body).Decode(&pageResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode events response: %w", err)
		}
		resp.Body.Close()

		for _, item := range pageResp.Value {
			startAt, err := parseMSTime(item.Start.DateTime, item.Start.TimeZone)
			if err != nil {
				startAt = time.Now()
			}
			endAt, err := parseMSTime(item.End.DateTime, item.End.TimeZone)
			if err != nil {
				endAt = startAt.Add(time.Hour)
			}

			status := "confirmed"
			if item.IsCancelled {
				status = "cancelled"
			}

			uid := item.ICalUID
			if uid == "" {
				uid = item.ID
			}

			desc := item.Body.Content
			// Strip HTML if body type is html
			if strings.EqualFold(item.Body.ContentType, "html") {
				desc = strings.ReplaceAll(desc, "<br>", "\n")
				desc = strings.ReplaceAll(desc, "<br/>", "\n")
				desc = strings.ReplaceAll(desc, "</p>", "\n\n")
			}

			loc := item.Location.DisplayName
			meetingURL := item.OnlineMeetingURL
			if meetingURL == "" && item.OnlineMeeting != nil {
				meetingURL = item.OnlineMeeting.JoinURL
			}
			if meetingURL != "" {
				if !strings.Contains(desc, meetingURL) {
					if desc != "" {
						desc += "\n\nMicrosoft Teams Meeting: " + meetingURL
					} else {
						desc = "Microsoft Teams Meeting: " + meetingURL
					}
				}
				if loc == "" {
					loc = "Microsoft Teams Meeting"
				}
			}

			event := models.Event{
				ID:              fmt.Sprintf("ev-ms-%s", item.ID),
				Provider:        "microsoft",
				ProviderEventID: item.ID,
				ProviderUID:     uid,
				CalendarID:      cal.ID,
				ETag:            item.ChangeKey,
				Title:           item.Subject,
				Description:     desc,
				Location:        loc,
				StartAt:         startAt,
				EndAt:           endAt,
				Timezone:        item.Start.TimeZone,
				AllDay:          item.IsAllDay,
				Status:          status,
				Organizer:       item.Organizer.EmailAddress.Address,
				Source:          "remote_sync",
			}
			allEvents = append(allEvents, event)
		}

		if pageResp.DeltaLink != "" {
			nextCursor = pageResp.DeltaLink
		}
		endpoint = pageResp.NextLink
	}

	return &calendar.SyncResult{
		CalendarID:    cal.ID,
		SyncCursor:    nextCursor,
		UpsertedCount: len(allEvents),
		Events:        allEvents,
	}, nil
}

type msEventCreatePayload struct {
	Subject  string `json:"subject"`
	Body     struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Start struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"start"`
	End struct {
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"end"`
	Location struct {
		DisplayName string `json:"displayName"`
	} `json:"location"`
	IsAllDay bool `json:"isAllDay"`
}

func (c *CalendarClient) CreateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event, idempotencyKey string) (*models.Event, error) {
	endpoint := fmt.Sprintf("%s/calendars/%s/events", c.baseURL, url.PathEscape(calendarID))

	payload := msEventCreatePayload{
		Subject: event.Title,
		IsAllDay: event.AllDay,
	}
	payload.Body.ContentType = "text"
	payload.Body.Content = event.Description
	payload.Location.DisplayName = event.Location

	payload.Start.DateTime = event.StartAt.UTC().Format("2006-01-02T15:04:05")
	payload.Start.TimeZone = "UTC"
	payload.End.DateTime = event.EndAt.UTC().Format("2006-01-02T15:04:05")
	payload.End.TimeZone = "UTC"

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal create event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("client-request-id", idempotencyKey)
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("create event request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("create event failed status %d: %s", resp.StatusCode, string(body))
	}

	var created struct {
		ID        string `json:"id"`
		ICalUID   string `json:"iCalUId"`
		ChangeKey string `json:"changeKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decode created event: %w", err)
	}

	result := *event
	result.Provider = "microsoft"
	result.ProviderEventID = created.ID
	result.ProviderUID = created.ICalUID
	if result.ProviderUID == "" {
		result.ProviderUID = created.ID
	}
	result.ETag = created.ChangeKey
	return &result, nil
}

func (c *CalendarClient) UpdateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event) (*models.Event, error) {
	endpoint := fmt.Sprintf("%s/events/%s", c.baseURL, url.PathEscape(event.ProviderEventID))

	payload := msEventCreatePayload{
		Subject: event.Title,
		IsAllDay: event.AllDay,
	}
	payload.Body.ContentType = "text"
	payload.Body.Content = event.Description
	payload.Location.DisplayName = event.Location
	payload.Start.DateTime = event.StartAt.UTC().Format("2006-01-02T15:04:05")
	payload.Start.TimeZone = "UTC"
	payload.End.DateTime = event.EndAt.UTC().Format("2006-01-02T15:04:05")
	payload.End.TimeZone = "UTC"

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal update event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if event.ETag != "" {
		req.Header.Set("If-Match", event.ETag)
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("update event request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("update event failed status %d: %s", resp.StatusCode, string(body))
	}

	var updated struct {
		ID        string `json:"id"`
		ChangeKey string `json:"changeKey"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decode updated event: %w", err)
	}

	result := *event
	result.ETag = updated.ChangeKey
	return &result, nil
}

func (c *CalendarClient) DeleteEvent(ctx context.Context, accessToken string, calendarID string, providerEventID string) error {
	endpoint := fmt.Sprintf("%s/events/%s", c.baseURL, url.PathEscape(providerEventID))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return fmt.Errorf("delete event request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("delete event failed status %d: %s", resp.StatusCode, string(body))
}
