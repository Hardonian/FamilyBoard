package web

import (
	"encoding/json"
	"net/http"

	"github.com/Hardonian/FamilyBoard/internal/db"
)

type ReadyStatus struct {
	Status           string `json:"status"` // "ready", "degraded", "not_ready"
	Database         string `json:"database"`
	MigrationVersion int    `json:"migration_version"`
	PendingMutations int    `json:"pending_mutations"`
	DeadLetterCount  int    `json:"dead_letter_count"`
	ApplianceVersion string `json:"version"`
}

func (s *Server) handleHealthLive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

func (s *Server) handleHealthReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := ReadyStatus{
		Status:           "ready",
		Database:         "healthy",
		ApplianceVersion: "v0.9.0",
	}

	// 1. Verify database integrity
	if err := s.store.DB().QuickCheck(); err != nil {
		status.Status = "not_ready"
		status.Database = "corrupted: " + err.Error()
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}

	// 2. Verify migration version
	ver, err := db.CurrentVersion(r.Context(), s.store.DB())
	if err != nil || ver < 2 {
		status.Status = "not_ready"
		status.Database = "pending_migrations"
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	status.MigrationVersion = ver

	// 3. Queue health
	pending, _ := s.store.CountPendingMutations(r.Context())
	status.PendingMutations = pending

	var deadLetters int
	_ = s.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM mutation_queue WHERE state = 'dead_letter';").Scan(&deadLetters)
	status.DeadLetterCount = deadLetters

	if deadLetters > 0 {
		status.Status = "degraded" // Degraded state, but still serving local calendar!
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}
