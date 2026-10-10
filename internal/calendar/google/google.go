package google

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
	ErrCursorInvalid = errors.New("sync cursor is invalid or expired (410 Gone)")
	ErrRateLimited   = errors.New("google calendar rate limit exceeded (429)")
	ErrNotFound      = errors.New("google calendar or event not found (404)")
)

type GoogleCalendarClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewGoogleCalendarClient(baseURL string, httpClient *http.Client) *GoogleCalendarClient {
	if baseURL == "" {
		baseURL = "https://www.googleapis.com/calendar/v3"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &GoogleCalendarClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *GoogleCalendarClient) Name() string {
	return "google"
}

// executeWithRetry executes an HTTP request with exponential backoff for 429 and 5xx.
func (c *GoogleCalendarClient) executeWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
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
				return nil, fmt.Errorf("google api returned server error %d", resp.StatusCode)
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
	return nil, errors.New("max retries reached")
}

// Google Calendar List Response schema
type calendarListResponse struct {
	NextPageToken string `json:"nextPageToken"`
	Items         []struct {
		ID              string `json:"id"`
		Summary         string `json:"summary"`
		Description     string `json:"description"`
		TimeZone        string `json:"timeZone"`
		BackgroundColor string `json:"backgroundColor"`
		Primary         bool   `json:"primary"`
		AccessRole      string `json:"accessRole"`
	} `json:"items"`
}

func (c *GoogleCalendarClient) ListCalendars(ctx context.Context, accessToken string) ([]models.Calendar, error) {
	var calendars []models.Calendar
	pageToken := ""

	for {
		endpoint := fmt.Sprintf("%s/users/me/calendarList", c.baseURL)
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse list calendars url: %w", err)
		}
		q := u.Query()
		q.Set("maxResults", "250")
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("create list calendars request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.executeWithRetry(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("list calendars request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("list calendars failed status %d: %s", resp.StatusCode, string(body))
		}

		var list calendarListResponse
		if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode calendar list: %w", err)
		}
		resp.Body.Close()

		for _, item := range list.Items {
			color := item.BackgroundColor
			if color == "" {
				color = "#3b82f6"
			}
			tz := item.TimeZone
			if tz == "" {
				tz = "America/Toronto"
			}
			writable := item.AccessRole == "owner" || item.AccessRole == "writer"

			name := item.Summary
			if name == "" {
				name = item.ID
			}
			calID := fmt.Sprintf("cal-g-%s", item.ID)

			calendars = append(calendars, models.Calendar{
				ID:                 calID,
				Provider:           "google",
				ProviderCalendarID: item.ID,
				Name:               name,
				Timezone:           tz,
				DisplayColor:       color,
				EnabledForDisplay:  true,
				Writable:           writable,
				DefaultWriteTarget: item.Primary,
			})
		}

		if list.NextPageToken == "" {
			break
		}
		pageToken = list.NextPageToken
	}

	return calendars, nil
}

// Google Events List Response schema
type googleEventsResponse struct {
	NextPageToken string        `json:"nextPageToken"`
	NextSyncToken string        `json:"nextSyncToken"`
	Items         []googleEvent `json:"items"`
}

type googleEvent struct {
	ID               string   `json:"id"`
	ICalUID          string   `json:"iCalUID"`
	Status           string   `json:"status"` // "confirmed", "tentative", "cancelled"
	Summary          string   `json:"summary"`
	Description      string   `json:"description"`
	Location         string   `json:"location"`
	ETag             string   `json:"etag"`
	Recurrence       []string `json:"recurrence"`
	RecurringEventID string   `json:"recurringEventId"`
	HangoutLink      string   `json:"hangoutLink"`
	ConferenceData   *struct {
		EntryPoints []struct {
			EntryPointType string `json:"entryPointType"`
			URI            string `json:"uri"`
			Label          string `json:"label"`
		} `json:"entryPoints"`
	} `json:"conferenceData"`
	Organizer struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	} `json:"organizer"`
	Start struct {
		Date     string `json:"date"`
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"start"`
	End struct {
		Date     string `json:"date"`
		DateTime string `json:"dateTime"`
		TimeZone string `json:"timeZone"`
	} `json:"end"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// SyncEvents fetches events using full sync or incremental sync cursor.
func (c *GoogleCalendarClient) SyncEvents(ctx context.Context, accessToken string, cal models.Calendar, syncCursor string) (*calendar.SyncResult, error) {
	calIDEscaped := url.PathEscape(cal.ProviderCalendarID)
	var allEvents []models.Event
	var nextSyncToken string
	pageToken := ""

	for {
		u, err := url.Parse(fmt.Sprintf("%s/calendars/%s/events", c.baseURL, calIDEscaped))
		if err != nil {
			return nil, err
		}

		q := u.Query()
		q.Set("maxResults", "250")
		q.Set("showDeleted", "true")

		if pageToken != "" {
			q.Set("pageToken", pageToken)
		} else if syncCursor != "" {
			q.Set("syncToken", syncCursor)
		} else {
			// Initial full sync: limit to past 30 days and forward 1 year
			timeMin := time.Now().AddDate(0, -1, 0).Format(time.RFC3339)
			q.Set("timeMin", timeMin)
		}

		u.RawQuery = q.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := c.executeWithRetry(ctx, req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusGone {
			resp.Body.Close()
			// 410 Gone: syncToken invalid. Caller must wipe cursor and trigger full sync.
			return nil, ErrCursorInvalid
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			return nil, fmt.Errorf("sync events failed status %d: %s", resp.StatusCode, string(body))
		}

		var pageResp googleEventsResponse
		if err := json.NewDecoder(resp.Body).Decode(&pageResp); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode events response: %w", err)
		}
		resp.Body.Close()

		for _, item := range pageResp.Items {
			ev, err := convertGoogleEventToModel(cal.ID, item)
			if err != nil {
				continue
			}
			allEvents = append(allEvents, ev)
		}

		if pageResp.NextSyncToken != "" {
			nextSyncToken = pageResp.NextSyncToken
		}

		if pageResp.NextPageToken == "" {
			break
		}
		pageToken = pageResp.NextPageToken
	}

	upsertCount := 0
	deletedCount := 0
	for _, ev := range allEvents {
		if ev.Status == "cancelled" || ev.DeletedAt != nil {
			deletedCount++
		} else {
			upsertCount++
		}
	}

	return &calendar.SyncResult{
		CalendarID:    cal.ID,
		SyncCursor:    nextSyncToken,
		UpsertedCount: upsertCount,
		DeletedCount:  deletedCount,
		Events:        allEvents,
	}, nil
}

func convertGoogleEventToModel(calendarID string, item googleEvent) (models.Event, error) {
	var startAt, endAt time.Time
	allDay := false
	tz := item.Start.TimeZone
	if tz == "" {
		tz = "America/Toronto"
	}

	if item.Start.Date != "" {
		// All-day event
		allDay = true
		loc, _ := time.LoadLocation(tz)
		if loc == nil {
			loc = time.Local
		}
		startAt, _ = time.ParseInLocation("2006-01-02", item.Start.Date, loc)
		endAt, _ = time.ParseInLocation("2006-01-02", item.End.Date, loc)
		if endAt.IsZero() {
			endAt = startAt.AddDate(0, 0, 1)
		}
	} else if item.Start.DateTime != "" {
		allDay = false
		startAt, _ = time.Parse(time.RFC3339, item.Start.DateTime)
		endAt, _ = time.Parse(time.RFC3339, item.End.DateTime)
		if endAt.IsZero() {
			endAt = startAt.Add(1 * time.Hour)
		}
	}

	status := item.Status
	if status == "" {
		status = "confirmed"
	}

	var deletedAt *time.Time
	if status == "cancelled" {
		now := time.Now()
		deletedAt = &now
	}

	rrule := ""
	if len(item.Recurrence) > 0 {
		rrule = strings.Join(item.Recurrence, "\n")
	}

	organizer := item.Organizer.Email
	if item.Organizer.DisplayName != "" {
		organizer = fmt.Sprintf("%s <%s>", item.Organizer.DisplayName, item.Organizer.Email)
	}

	now := time.Now()
	eventID := fmt.Sprintf("evt-g-%s", item.ID)
	if calendarID != "" {
		eventID = fmt.Sprintf("evt-g-%s-%s", calendarID, item.ID)
	}

	desc := item.Description
	loc := item.Location

	meetURL := item.HangoutLink
	if meetURL == "" && item.ConferenceData != nil {
		for _, ep := range item.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" || strings.HasPrefix(ep.URI, "https://meet.google.com") {
				meetURL = ep.URI
				break
			}
		}
	}
	if meetURL != "" {
		if !strings.Contains(desc, meetURL) {
			if desc != "" {
				desc += "\n\nGoogle Meet: " + meetURL
			} else {
				desc = "Google Meet: " + meetURL
			}
		}
		if loc == "" {
			loc = "Google Meet"
		}
	}

	return models.Event{
		ID:               eventID,
		Provider:         "google",
		ProviderEventID:  item.ID,
		ProviderUID:      item.ICalUID,
		CalendarID:       calendarID,
		ETag:             item.ETag,
		Version:          item.Updated.Unix(),
		Title:            item.Summary,
		Description:      desc,
		Location:         loc,
		StartAt:          startAt,
		EndAt:            endAt,
		Timezone:         tz,
		AllDay:           allDay,
		Status:           status,
		RecurrenceRule:   rrule,
		RecurrenceParent: item.RecurringEventID,
		Organizer:        organizer,
		Source:           "remote_sync",
		RemoteCreatedAt:  &item.Created,
		RemoteUpdatedAt:  &item.Updated,
		LocalCreatedAt:   now,
		LocalUpdatedAt:   now,
		DeletedAt:        deletedAt,
	}, nil
}

// CreateEvent sends a new event to Google Calendar.
func (c *GoogleCalendarClient) CreateEvent(ctx context.Context, accessToken, calendarID string, ev *models.Event, idempotencyKey string) (*models.Event, error) {
	calEscaped := url.PathEscape(calendarID)
	endpoint := fmt.Sprintf("%s/calendars/%s/events", c.baseURL, calEscaped)

	payload := map[string]interface{}{
		"summary":     ev.Title,
		"description": ev.Description,
		"location":    ev.Location,
	}

	if ev.AllDay {
		payload["start"] = map[string]string{"date": ev.StartAt.Format("2006-01-02")}
		payload["end"] = map[string]string{"date": ev.EndAt.Format("2006-01-02")}
	} else {
		payload["start"] = map[string]string{"dateTime": ev.StartAt.Format(time.RFC3339)}
		payload["end"] = map[string]string{"dateTime": ev.EndAt.Format(time.RFC3339)}
	}

	if ev.RecurrenceRule != "" {
		payload["recurrence"] = strings.Split(ev.RecurrenceRule, "\n")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal create event payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("X-Idempotency-Key", idempotencyKey)
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("create event failed status %d: %s", resp.StatusCode, string(body))
	}

	var createdItem googleEvent
	if err := json.NewDecoder(resp.Body).Decode(&createdItem); err != nil {
		return nil, fmt.Errorf("decode created event: %w", err)
	}

	createdModel, err := convertGoogleEventToModel(calendarID, createdItem)
	if err != nil {
		return nil, err
	}
	createdModel.ID = ev.ID // Retain local event ID
	return &createdModel, nil
}

// UpdateEvent updates an existing event in Google Calendar.
func (c *GoogleCalendarClient) UpdateEvent(ctx context.Context, accessToken, calendarID string, ev *models.Event) (*models.Event, error) {
	calEscaped := url.PathEscape(calendarID)
	evEscaped := url.PathEscape(ev.ProviderEventID)
	endpoint := fmt.Sprintf("%s/calendars/%s/events/%s", c.baseURL, calEscaped, evEscaped)

	payload := map[string]interface{}{
		"summary":     ev.Title,
		"description": ev.Description,
		"location":    ev.Location,
		"status":      ev.Status,
	}

	if ev.AllDay {
		payload["start"] = map[string]string{"date": ev.StartAt.Format("2006-01-02")}
		payload["end"] = map[string]string{"date": ev.EndAt.Format("2006-01-02")}
	} else {
		payload["start"] = map[string]string{"dateTime": ev.StartAt.Format(time.RFC3339)}
		payload["end"] = map[string]string{"dateTime": ev.EndAt.Format(time.RFC3339)}
	}

	if ev.RecurrenceRule != "" {
		payload["recurrence"] = strings.Split(ev.RecurrenceRule, "\n")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal update event payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	if ev.ETag != "" {
		req.Header.Set("If-Match", ev.ETag)
	}

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("update event failed status %d: %s", resp.StatusCode, string(body))
	}

	var updatedItem googleEvent
	if err := json.NewDecoder(resp.Body).Decode(&updatedItem); err != nil {
		return nil, fmt.Errorf("decode updated event: %w", err)
	}

	updatedModel, err := convertGoogleEventToModel(calendarID, updatedItem)
	if err != nil {
		return nil, err
	}
	updatedModel.ID = ev.ID
	return &updatedModel, nil
}

// DeleteEvent removes an event from Google Calendar.
func (c *GoogleCalendarClient) DeleteEvent(ctx context.Context, accessToken, calendarID, providerEventID string) error {
	calEscaped := url.PathEscape(calendarID)
	evEscaped := url.PathEscape(providerEventID)
	endpoint := fmt.Sprintf("%s/calendars/%s/events/%s", c.baseURL, calEscaped, evEscaped)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.executeWithRetry(ctx, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil // Already deleted remotely
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("delete event failed status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
