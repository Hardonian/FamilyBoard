package calendar

import (
	"context"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

// SyncResult represents the output of a calendar sync operation.
type SyncResult struct {
	CalendarID    string
	SyncCursor    string
	UpsertedCount int
	DeletedCount  int
	Events        []models.Event
}

// Provider defines the interface for external calendar sources.
type Provider interface {
	Name() string
	ListCalendars(ctx context.Context, accessToken string) ([]models.Calendar, error)
	SyncEvents(ctx context.Context, accessToken string, calendar models.Calendar, syncCursor string) (*SyncResult, error)
	CreateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event, idempotencyKey string) (*models.Event, error)
	UpdateEvent(ctx context.Context, accessToken string, calendarID string, event *models.Event) (*models.Event, error)
	DeleteEvent(ctx context.Context, accessToken string, calendarID string, providerEventID string) error
}
