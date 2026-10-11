package web

import (
	"encoding/json"
	"net/http"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
	"github.com/Hardonian/FamilyBoard/internal/timezone"
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
		ApplianceVersion: buildinfo.Version,
	}

	// Readiness should be cheap enough to poll frequently. Full integrity checks
	// run at startup and via familyboardctl db-check.
	if err := s.store.DB().PingContext(r.Context()); err != nil {
		status.Status = "not_ready"
		status.Database = "unavailable"
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}

	// Verify that the live database matches the migrations embedded in this
	// binary. A stale schema must never be reported as ready.
	ver, err := db.CurrentVersion(r.Context(), s.store.DB())
	latest, latestErr := db.LatestVersion()
	if err != nil || latestErr != nil || ver != latest {
		status.Status = "not_ready"
		status.Database = "pending_migrations"
		status.MigrationVersion = ver
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	status.MigrationVersion = ver

	// 3. Queue health
	pending, err := s.store.CountPendingMutations(r.Context())
	if err != nil {
		status.Status = "not_ready"
		status.Database = "unavailable"
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	status.PendingMutations = pending

	var deadLetters int
	if err := s.store.DB().QueryRowContext(r.Context(), "SELECT COUNT(*) FROM mutation_queue WHERE state = 'dead_letter';").Scan(&deadLetters); err != nil {
		status.Status = "not_ready"
		status.Database = "unavailable"
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(status)
		return
	}
	status.DeadLetterCount = deadLetters

	if deadLetters > 0 {
		status.Status = "degraded" // Degraded state, but still serving local calendar!
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(status)
}

type AdminHealthReport struct {
	Status           string                  `json:"status"`
	ApplianceVersion string                  `json:"version"`
	SchemaVersion    int                     `json:"schema_version"`
	PendingMutations int                     `json:"pending_mutations"`
	DeadLetterCount  int                     `json:"dead_letter_count"`
	ClockHealth      timezone.ClockHealth    `json:"clock_health"`
	Metrics          metrics.MetricsSnapshot `json:"metrics"`
	RepairActions    []string                `json:"repair_actions"`
}

func (s *Server) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	ctx := r.Context()
	ver, _ := db.CurrentVersion(ctx, s.store.DB())
	pending, _ := s.store.CountPendingMutations(ctx)
	deadLetters, _ := s.store.CountDeadLetterMutations(ctx)

	status := "ready"
	if deadLetters > 0 {
		status = "degraded"
	}

	clock := timezone.CheckClockHealth(s.cfg.Server.Timezone)
	if clock.Status == "critical" {
		status = "degraded"
	}

	report := AdminHealthReport{
		Status:           status,
		ApplianceVersion: buildinfo.Version,
		SchemaVersion:    ver,
		PendingMutations: pending,
		DeadLetterCount:  deadLetters,
		ClockHealth:      clock,
		Metrics:          metrics.Default().Snapshot(),
		RepairActions: []string{
			"POST /admin/queue/reset/{id}",
			"POST /admin/sync/all",
			"POST /admin/system/storage/migrate-nvme",
		},
	}

	_ = json.NewEncoder(w).Encode(report)
}

