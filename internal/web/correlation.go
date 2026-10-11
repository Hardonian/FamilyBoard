package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type correlationIDKey struct{}

// ContextWithCorrelationID returns a new context with the given correlation ID.
func ContextWithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, id)
}

// CorrelationIDFromContext extracts the correlation ID from context, or returns "" if unset.
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateCorrelationID produces a collision-resistant, low-overhead trace identifier.
func GenerateCorrelationID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("req-%d-%s", time.Now().UnixMilli(), hex.EncodeToString(buf[:]))
}

// withCorrelationID extracts or generates an X-Correlation-ID header, attaches it to the
// response, and injects it into the request context for structured tracing.
func withCorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corrID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if corrID == "" {
			corrID = strings.TrimSpace(r.Header.Get("X-Request-ID"))
		}
		if corrID == "" || len(corrID) > 128 {
			corrID = GenerateCorrelationID()
		}

		w.Header().Set("X-Correlation-ID", corrID)
		ctx := ContextWithCorrelationID(r.Context(), corrID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
