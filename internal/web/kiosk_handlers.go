package web

import (
	"context"
	"encoding/json"
	"fmt"
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
	if meta != nil && meta.DeviceName != "" {
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
	}

	s.render(w, "kiosk.html", data)
}

func (s *Server) handleAPICalendars(w http.ResponseWriter, r *http.Request) {
	calendars, err := s.store.ListDisplayCalendars(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	startAt, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		startAt = time.Now().AddDate(0, -1, 0)
	}
	endAt, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		endAt = time.Now().AddDate(0, 2, 0)
	}

	events, err := s.store.ListEventsRange(r.Context(), startAt, endAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(ev.Title) == "" || ev.CalendarID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Event title and destination calendar are required."})
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to save event locally: " + err.Error()})
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

	var update models.Event
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if update.CalendarID != "" && update.CalendarID != existing.CalendarID {
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
	existing.Source = "kiosk"
	existing.LocalUpdatedAt = time.Now()

	if err := s.store.UpsertEvent(r.Context(), existing); err != nil {
		http.Error(w, "update event locally: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "delete event locally: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "restore event locally: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
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

func (s *Server) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

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
