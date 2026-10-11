package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCorrelationIDMiddleware(t *testing.T) {
	var capturedID string
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = CorrelationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	mw := withCorrelationID(testHandler)

	// Case 1: Auto-generate when missing
	req := httptest.NewRequest("GET", "/api/health", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	respHeader := rec.Header().Get("X-Correlation-ID")
	if respHeader == "" {
		t.Fatal("expected X-Correlation-ID header in response")
	}
	if !strings.HasPrefix(respHeader, "req-") {
		t.Errorf("expected req- prefix, got: %s", respHeader)
	}
	if capturedID != respHeader {
		t.Errorf("context ID %q does not match header ID %q", capturedID, respHeader)
	}

	// Case 2: Respect upstream correlation ID
	upstreamID := "custom-trace-12345"
	req2 := httptest.NewRequest("GET", "/api/health", nil)
	req2.Header.Set("X-Correlation-ID", upstreamID)
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)

	if rec2.Header().Get("X-Correlation-ID") != upstreamID {
		t.Errorf("expected upstream ID %q, got: %q", upstreamID, rec2.Header().Get("X-Correlation-ID"))
	}
	if capturedID != upstreamID {
		t.Errorf("expected captured ID %q, got: %q", upstreamID, capturedID)
	}
}

func TestContextWithCorrelationID(t *testing.T) {
	ctx := context.Background()
	if id := CorrelationIDFromContext(ctx); id != "" {
		t.Errorf("expected empty string for unadorned context, got: %s", id)
	}

	ctx = ContextWithCorrelationID(ctx, "test-id")
	if id := CorrelationIDFromContext(ctx); id != "test-id" {
		t.Errorf("expected test-id, got: %s", id)
	}
}
