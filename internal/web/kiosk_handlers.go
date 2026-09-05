package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func (s *Server) handleKiosk(w http.ResponseWriter, r *http.Request) {
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

	data := map[string]interface{}{
		"Title":      deviceName,
		"Theme":      theme,
		"DeviceName": deviceName,
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

	if events == nil {
		events = []models.Event{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}

func (s *Server) handleAPICreateEvent(w http.ResponseWriter, r *http.Request) {
	var ev models.Event
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}

	if ev.Title == "" || ev.CalendarID == "" {
		http.Error(w, "title and calendar_id are required", http.StatusBadRequest)
		return
	}

	cal, err := s.store.GetCalendar(r.Context(), ev.CalendarID)
	if err != nil || cal == nil {
		http.Error(w, "calendar not found", http.StatusBadRequest)
		return
	}

	now := time.Now()
	ev.ID = fmt.Sprintf("evt-local-%d", now.UnixNano())
	ev.Provider = cal.Provider
	ev.Source = "local_ui"
	ev.Status = "confirmed"
	ev.LocalCreatedAt = now
	ev.LocalUpdatedAt = now

	// 1. Write to local database
	if err := s.store.UpsertEvent(r.Context(), &ev); err != nil {
		http.Error(w, "save event locally: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Enqueue into durable outbound mutation queue
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

	existing.Title = update.Title
	existing.Description = update.Description
	existing.Location = update.Location
	existing.StartAt = update.StartAt
	existing.EndAt = update.EndAt
	existing.AllDay = update.AllDay
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
		IdempotencyKey: fmt.Sprintf("idemp-del-%s", existing.ID),
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

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"deleted"}`))
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
