package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func (s *Server) handleKiosk(w http.ResponseWriter, r *http.Request) {
	// Issue physical localhost capability cookie if request is on loopback
	s.issueKioskCapabilityCookie(w, r)

	// Check if appliance needs first-run wizard setup
	meta, err := s.store.GetDeviceMetadata(r.Context())
	if err != nil || meta == nil {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}

	theme := s.cfg.Kiosk.Theme
	if theme == "auto" {
		nowStr := time.Now().Format("15:04")
		if nowStr >= s.cfg.Kiosk.NightStartsAt || nowStr < s.cfg.Kiosk.DayStartsAt {
			theme = "dark"
		} else {
			theme = "light"
		}
	}

	deviceName := s.cfg.Server.DeviceName
	if meta.DeviceName != "" {
		deviceName = meta.DeviceName
	}

	calendars, _ := s.store.ListDisplayCalendars(r.Context())
	people, _ := s.store.ListPeople(r.Context())
	templates, _ := s.store.ListQuickTemplates(r.Context())

	defaultView, _ := s.store.GetSetting(r.Context(), "kiosk_default_view")
	if defaultView == "" {
		defaultView = s.cfg.Kiosk.DefaultView
	}
	if defaultView == "" {
		defaultView = "hybrid"
	}

	timeFormat, _ := s.store.GetSetting(r.Context(), "kiosk_time_format")
	if timeFormat == "" {
		timeFormat = "12h"
	}

	birthdayHighlight, _ := s.store.GetSetting(r.Context(), "kiosk_birthday_highlight")
	if birthdayHighlight == "" {
		birthdayHighlight = "true"
	}
	hotspotPassword := ""
	if s.hasKioskCapability(r) {
		hotspotPassword = s.cfg.Security.HotspotPassword
	}

	data := map[string]interface{}{
		"Title":             deviceName,
		"Theme":             theme,
		"DeviceName":        deviceName,
		"Calendars":         calendars,
		"People":            people,
		"QuickTemplates":    templates,
		"DefaultView":       defaultView,
		"TimeFormat":        timeFormat,
		"BirthdayHighlight": birthdayHighlight == "true",
		"IsLocalhost":       s.isLocalhost(r),
		"HotspotPassword":   hotspotPassword,
	}

	s.render(w, "kiosk.html", data)
}

func (s *Server) handleAPICalendars(w http.ResponseWriter, r *http.Request) {
	calendars, err := s.store.ListDisplayCalendars(r.Context())
	if err != nil {
		slog.Error("Failed to list kiosk calendars", "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to load calendars")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(calendars)
}

// KioskEventView provides a stripped, lightweight event representation for the kiosk UI,
// eliminating internal sync metadata (ETags, provider UIDs, versions, internal timestamps).
type KioskEventView struct {
	ID             string    `json:"id"`
	CalendarID     string    `json:"calendar_id"`
	Title          string    `json:"title"`
	StartAt        time.Time `json:"start_at"`
	EndAt          time.Time `json:"end_at"`
	AllDay         bool      `json:"all_day"`
	Location       string    `json:"location,omitempty"`
	Description    string    `json:"description,omitempty"`
	RecurrenceRule string    `json:"recurrence_rule,omitempty"`
	Status         string    `json:"status"`
}

func (s *Server) handleAPIGetEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	startStr := q.Get("start")
	endStr := q.Get("end")

	startAt := time.Now().AddDate(0, -1, 0)
	if startStr != "" {
		var err error
		startAt, err = time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeHardwareError(w, http.StatusBadRequest, "start must use RFC3339 format")
			return
		}
	}
	endAt := time.Now().AddDate(0, 2, 0)
	if endStr != "" {
		var err error
		endAt, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeHardwareError(w, http.StatusBadRequest, "end must use RFC3339 format")
			return
		}
	}
	if !endAt.After(startAt) || endAt.Sub(startAt) > 366*24*time.Hour {
		writeHardwareError(w, http.StatusBadRequest, "event range must be positive and no longer than 366 days")
		return
	}

	events, err := s.store.ListEventsRange(r.Context(), startAt, endAt)
	if err != nil {
		slog.Error("Failed to list kiosk events", "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to load events")
		return
	}

	views := make([]KioskEventView, len(events))
	for i, ev := range events {
		views[i] = KioskEventView{
			ID:             ev.ID,
			CalendarID:     ev.CalendarID,
			Title:          ev.Title,
			StartAt:        ev.StartAt,
			EndAt:          ev.EndAt,
			AllDay:         ev.AllDay,
			Location:       ev.Location,
			Description:    ev.Description,
			RecurrenceRule: ev.RecurrenceRule,
			Status:         ev.Status,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	_ = json.NewEncoder(w).Encode(views)
}

func (s *Server) handleAPICreateEvent(w http.ResponseWriter, r *http.Request) {
	var ev models.Event
	if !decodeRequestJSON(w, r, &ev) {
		return
	}

	if err := validateEventInput(&ev); err != nil {
		writeHardwareError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Enforce writable calendar privilege boundary
	cal, err := s.isCalendarWritable(r.Context(), ev.CalendarID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	now := time.Now()
	if ev.ID == "" {
		ev.ID = fmt.Sprintf("evt-local-%d", now.UnixNano())
	}
	ev.Provider = cal.Provider
	ev.Source = "kiosk"
	ev.Status = "confirmed"
	ev.LocalCreatedAt = now
	ev.LocalUpdatedAt = now

	// 1. Write to local SQLite database
	if err := s.store.UpsertEvent(r.Context(), &ev); err != nil {
		slog.Error("Failed to save kiosk event", "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to save event locally")
		return
	}

	// 2. Enqueue into durable outbound mutation queue for Google / Microsoft sync
	payload, _ := json.Marshal(ev)
	mutation := &models.MutationQueueItem{
		ID:             fmt.Sprintf("mut-%d", now.UnixNano()),
		Operation:      "create",
		Provider:       cal.Provider,
		CalendarID:     cal.ID,
		LocalEventID:   ev.ID,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("idemp-%s", ev.ID),
		AttemptCount:   0,
		NextAttemptAt:  now,
		State:          models.QueuePending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	_ = s.store.EnqueueMutation(r.Context(), mutation)

	// 3. Immediately trigger background queue processing & SSE broadcast
	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(ev)
}

func (s *Server) handleAPIUpdateEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetEvent(r.Context(), id)
	if err != nil || existing == nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	var request struct {
		CalendarID     *string    `json:"calendar_id"`
		Title          *string    `json:"title"`
		Description    *string    `json:"description"`
		Location       *string    `json:"location"`
		StartAt        *time.Time `json:"start_at"`
		EndAt          *time.Time `json:"end_at"`
		AllDay         *bool      `json:"all_day"`
		RecurrenceRule *string    `json:"recurrence_rule"`
	}
	if !decodeRequestJSON(w, r, &request) {
		return
	}
	update := *existing
	if request.CalendarID != nil {
		update.CalendarID = *request.CalendarID
	}
	if request.Title != nil {
		update.Title = *request.Title
	}
	if request.Description != nil {
		update.Description = *request.Description
	}
	if request.Location != nil {
		update.Location = *request.Location
	}
	if request.StartAt != nil {
		update.StartAt = *request.StartAt
	}
	if request.EndAt != nil {
		update.EndAt = *request.EndAt
	}
	if request.AllDay != nil {
		update.AllDay = *request.AllDay
	}
	if request.RecurrenceRule != nil {
		update.RecurrenceRule = *request.RecurrenceRule
	}
	if err := validateEventInput(&update); err != nil {
		writeHardwareError(w, http.StatusBadRequest, err.Error())
		return
	}

	if update.CalendarID != existing.CalendarID {
		cal, err := s.isCalendarWritable(r.Context(), update.CalendarID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		existing.CalendarID = cal.ID
		existing.Provider = cal.Provider
	}

	existing.Title = update.Title
	existing.Description = update.Description
	existing.Location = update.Location
	existing.StartAt = update.StartAt
	existing.EndAt = update.EndAt
	existing.AllDay = update.AllDay
	existing.RecurrenceRule = update.RecurrenceRule
	existing.Source = "kiosk"
	existing.LocalUpdatedAt = time.Now()

	if err := s.store.UpsertEvent(r.Context(), existing); err != nil {
		slog.Error("Failed to update kiosk event", "event_id", id, "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to update event locally")
		return
	}

	payload, _ := json.Marshal(existing)
	mutation := &models.MutationQueueItem{
		ID:             fmt.Sprintf("mut-%d", time.Now().UnixNano()),
		Operation:      "update",
		Provider:       existing.Provider,
		CalendarID:     existing.CalendarID,
		LocalEventID:   existing.ID,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("idemp-upd-%s-%d", existing.ID, existing.LocalUpdatedAt.UnixNano()),
		AttemptCount:   0,
		NextAttemptAt:  time.Now(),
		State:          models.QueuePending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = s.store.EnqueueMutation(r.Context(), mutation)

	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(existing)
}

func (s *Server) handleAPIDeleteEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	existing, err := s.store.GetEvent(r.Context(), id)
	if err != nil || existing == nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	if err := s.store.SoftDeleteEvent(r.Context(), id); err != nil {
		slog.Error("Failed to delete kiosk event", "event_id", id, "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to delete event locally")
		return
	}

	payload, _ := json.Marshal(existing)
	mutation := &models.MutationQueueItem{
		ID:             fmt.Sprintf("mut-%d", time.Now().UnixNano()),
		Operation:      "delete",
		Provider:       existing.Provider,
		CalendarID:     existing.CalendarID,
		LocalEventID:   existing.ID,
		Payload:        string(payload),
		IdempotencyKey: fmt.Sprintf("idemp-del-%s-%d", existing.ID, time.Now().UnixNano()),
		AttemptCount:   0,
		NextAttemptAt:  time.Now(),
		State:          models.QueuePending,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	_ = s.store.EnqueueMutation(r.Context(), mutation)

	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "deleted", "id": id, "event": existing})
}

func (s *Server) handleAPIRestoreEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	if err := s.store.RestoreEvent(r.Context(), id); err != nil {
		slog.Error("Failed to restore kiosk event", "event_id", id, "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to restore event locally")
		return
	}

	restored, err := s.store.GetEvent(r.Context(), id)
	if err == nil && restored != nil {
		payload, _ := json.Marshal(restored)
		mutation := &models.MutationQueueItem{
			ID:             fmt.Sprintf("mut-%d", time.Now().UnixNano()),
			Operation:      "create",
			Provider:       restored.Provider,
			CalendarID:     restored.CalendarID,
			LocalEventID:   restored.ID,
			Payload:        string(payload),
			IdempotencyKey: fmt.Sprintf("idemp-rest-%s-%d", restored.ID, time.Now().UnixNano()),
			AttemptCount:   0,
			NextAttemptAt:  time.Now(),
			State:          models.QueuePending,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		_ = s.store.EnqueueMutation(r.Context(), mutation)
	}

	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "restored", "id": id, "event": restored})
}

func (s *Server) handleAPIGetTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.store.ListQuickTemplates(r.Context())
	if err != nil {
		slog.Error("Failed to list quick templates", "err", err)
		writeHardwareError(w, http.StatusInternalServerError, "failed to load quick templates")
		return
	}
	if templates == nil {
		templates = []models.QuickTemplate{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(templates)
}

type parseNaturalRequest struct {
	Text string `json:"text"`
}

type parseNaturalResponse struct {
	Title            string    `json:"title"`
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	AllDay           bool      `json:"all_day"`
	Location         string    `json:"location"`
	Confidence       float64   `json:"confidence"`
	AmbiguityReasons []string  `json:"ambiguity_reasons,omitempty"`
}

func (s *Server) handleAPIParseNatural(w http.ResponseWriter, r *http.Request) {
	var req parseNaturalRequest
	if !decodeRequestJSON(w, r, &req) {
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > 1000 {
		writeHardwareError(w, http.StatusBadRequest, "text must be between 1 and 1000 characters")
		return
	}

	parsed, err := s.nlParser.Parse(req.Text, "", time.Now())
	if err != nil {
		http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
		return
	}

	resp := parseNaturalResponse{
		Title:            parsed.Title,
		StartAt:          parsed.StartAt,
		EndAt:            parsed.EndAt,
		AllDay:           parsed.AllDay,
		Location:         parsed.Location,
		Confidence:       parsed.Confidence,
		AmbiguityReasons: parsed.AmbiguityReasons,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func validateEventInput(event *models.Event) error {
	event.Title = strings.TrimSpace(event.Title)
	event.CalendarID = strings.TrimSpace(event.CalendarID)
	event.Location = strings.TrimSpace(event.Location)
	event.Description = strings.TrimSpace(event.Description)
	if event.Title == "" || event.CalendarID == "" {
		return fmt.Errorf("event title and destination calendar are required")
	}
	if len(event.Title) > 200 {
		return fmt.Errorf("event title must be at most 200 characters")
	}
	if len(event.Location) > 500 || len(event.Description) > 10000 || len(event.RecurrenceRule) > 2000 {
		return fmt.Errorf("event details exceed the supported length")
	}
	if event.StartAt.IsZero() || event.EndAt.IsZero() || !event.EndAt.After(event.StartAt) {
		return fmt.Errorf("event end time must be after its start time")
	}
	return nil
}

func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientCh := make(chan string, 5)

	s.sseMu.Lock()
	s.sseClients[clientCh] = struct{}{}
	s.sseMu.Unlock()

	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, clientCh)
		s.sseMu.Unlock()
		close(clientCh)
	}()

	// Send initial ping
	fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-clientCh:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
