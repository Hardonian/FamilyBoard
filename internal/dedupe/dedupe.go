package dedupe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

type Engine struct {
	store *db.Store
}

func NewEngine(store *db.Store) *Engine {
	return &Engine{store: store}
}

// ComputeContentHash returns SHA-256 hexadecimal string of the provided raw content.
func ComputeContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// SemanticFingerprint generates a normalized fingerprint string for title and start time.
func SemanticFingerprint(title string, startAt time.Time, calendarID string) string {
	normTitle := strings.ToLower(strings.TrimSpace(title))
	payload := fmt.Sprintf("%s|%s|%s", normTitle, startAt.UTC().Format(time.RFC3339), calendarID)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

type DedupeResult struct {
	IsDuplicate    bool
	Reason         string
	ExistingEventID string
}

// CheckEventDuplicate checks whether an incoming event or candidate already exists in the target calendar.
func (e *Engine) CheckEventDuplicate(ctx context.Context, calendarID, uid, title string, startAt time.Time) (*DedupeResult, error) {
	// 1. Check by ICS UID
	if uid != "" {
		var existingID string
		row := e.store.DB().QueryRowContext(ctx, "SELECT id FROM events WHERE provider_uid = ? AND deleted_at IS NULL LIMIT 1;", uid)
		if err := row.Scan(&existingID); err == nil {
			return &DedupeResult{
				IsDuplicate:     true,
				Reason:          fmt.Sprintf("event with ics uid %q already exists", uid),
				ExistingEventID: existingID,
			}, nil
		}
	}

	// 2. Semantic matching: events in target calendar starting within 15 minutes with matching title
	timeMin := startAt.Add(-15 * time.Minute)
	timeMax := startAt.Add(15 * time.Minute)

	events, err := e.store.ListEventsRange(ctx, timeMin, timeMax)
	if err != nil {
		return nil, fmt.Errorf("list events for dedupe: %w", err)
	}

	normTarget := strings.ToLower(strings.TrimSpace(title))
	for _, ev := range events {
		if ev.CalendarID == calendarID && strings.EqualFold(strings.TrimSpace(ev.Title), normTarget) {
			return &DedupeResult{
				IsDuplicate:     true,
				Reason:          fmt.Sprintf("event with matching title %q at same time already exists", ev.Title),
				ExistingEventID: ev.ID,
			}, nil
		}
	}

	// 3. Also check pending candidates to avoid approving identical messages in queue
	candidates, err := e.store.ListCandidates(ctx, models.CandidatePending, 100, 0)
	if err == nil {
		for _, c := range candidates {
			if strings.EqualFold(strings.TrimSpace(c.Title), normTarget) && c.StartAt.Equal(startAt) {
				return &DedupeResult{
					IsDuplicate: true,
					Reason:      fmt.Sprintf("candidate %q at same time already pending in approval queue", c.Title),
				}, nil
			}
		}
	}

	return &DedupeResult{IsDuplicate: false}, nil
}
