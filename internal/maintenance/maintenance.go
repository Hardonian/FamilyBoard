package maintenance

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

const (
	DefaultDeletedEventDays = 365
	DefaultAuditDays        = 730
	CompletedMutationAge    = 30 * 24 * time.Hour
	ResolvedCandidateAge    = 90 * 24 * time.Hour
)

type Report struct {
	RemovedEvents int64
	RemovedAudit  int64
}

type Runner struct {
	store    *db.Store
	interval time.Duration
}

func NewRunner(store *db.Store, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &Runner{store: store, interval: interval}
}

// RunOnce applies the live household retention settings and prunes bounded
// operational history. Every data deletion happens in store transactions.
func RunOnce(ctx context.Context, store *db.Store) (Report, error) {
	eventDays, err := retentionDays(ctx, store, "deleted_event_retention_days", DefaultDeletedEventDays, 7)
	if err != nil {
		return Report{}, err
	}
	auditDays, err := retentionDays(ctx, store, "audit_retention_days", DefaultAuditDays, 30)
	if err != nil {
		return Report{}, err
	}

	removedEvents, removedAudit, err := store.ApplyPrivacyRetention(ctx, eventDays, auditDays)
	if err != nil {
		return Report{}, err
	}
	if err := store.PruneRetentionRecords(ctx, CompletedMutationAge, ResolvedCandidateAge, 0); err != nil {
		return Report{}, err
	}
	if _, err := store.DB().ExecContext(ctx, "PRAGMA optimize;"); err != nil {
		return Report{}, fmt.Errorf("optimize database: %w", err)
	}

	report := Report{RemovedEvents: removedEvents, RemovedAudit: removedAudit}
	_ = store.RecordAudit(ctx, &models.AuditEntry{
		ID: models.NewID("aud"), Actor: "system", Action: "retention_maintenance",
		ResourceType: "storage", ResourceID: "local",
		Details: fmt.Sprintf("Scheduled maintenance removed %d deleted events and %d audit records", removedEvents, removedAudit),
	})
	return report, nil
}

func (r *Runner) Start(ctx context.Context) {
	go func() {
		if report, err := RunOnce(ctx, r.store); err != nil {
			slog.Error("Scheduled storage maintenance failed", "err", err)
		} else {
			slog.Info("Scheduled storage maintenance complete", "removed_events", report.RemovedEvents, "removed_audit", report.RemovedAudit)
		}

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if report, err := RunOnce(ctx, r.store); err != nil {
					slog.Error("Scheduled storage maintenance failed", "err", err)
				} else {
					slog.Info("Scheduled storage maintenance complete", "removed_events", report.RemovedEvents, "removed_audit", report.RemovedAudit)
				}
			}
		}
	}()
}

func retentionDays(ctx context.Context, store *db.Store, key string, fallback, minimum int) (int, error) {
	value, err := store.GetSetting(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", key, err)
	}
	if value == "" {
		return fallback, nil
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < minimum || days > 3650 {
		return 0, fmt.Errorf("invalid %s value %q", key, value)
	}
	return days, nil
}
