package mail

import (
	"context"
	"time"
)

// RawEmail represents a retrieved email message prior to parsing.
type RawEmail struct {
	Provider          string
	ProviderMessageID string
	Sender            string
	Recipients        []string
	Subject           string
	ReceivedAt        time.Time
	RawMIME           []byte
}

// Provider defines the interface for email sources (Gmail, Microsoft Graph).
type Provider interface {
	Name() string
	FetchMessages(ctx context.Context, accessToken string, filterMode, filterConfig, lastCursor string, limit int) ([]RawEmail, string, error)
}
