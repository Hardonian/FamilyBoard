package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

func createAdminCookie(t *testing.T, store any) *http.Cookie {
	token := "admin-test-token-fixed-12345"
	now := time.Now()
	type sessionCreator interface {
		CreateSession(ctx context.Context, s *models.Session) error
	}
	sc, ok := store.(sessionCreator)
	if !ok {
		t.Fatalf("store does not implement CreateSession")
	}
	if err := sc.CreateSession(context.Background(), &models.Session{
		TokenHash:    auth.HashToken(token),
		UserID:       "admin",
		ExpiresAt:    now.Add(24 * time.Hour),
		CreatedAt:    now,
		LastActiveAt: now,
	}); err != nil {
		t.Fatalf("failed to create admin test session: %v", err)
	}
	return &http.Cookie{
		Name:  "fb_session",
		Value: token,
	}
}

func TestAdminAPIHealth(t *testing.T) {
	srv, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	cookie := createAdminCookie(t, store)

	req := httptest.NewRequest("GET", "/admin/api/health", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse json response: %v", err)
	}

	if resp["status"] != "ready" && resp["status"] != "degraded" {
		t.Errorf("unexpected status: %v", resp["status"])
	}
	if resp["clock_health"] == nil {
		t.Errorf("missing clock_health in report")
	}
	if resp["metrics"] == nil {
		t.Errorf("missing metrics in report")
	}
}

func TestAdminSupportBundle(t *testing.T) {
	srv, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	cookie := createAdminCookie(t, store)

	req := httptest.NewRequest("GET", "/admin/support/bundle", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %d (%s)", rec.Code, rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/zip" {
		t.Errorf("expected application/zip, got: %s", contentType)
	}

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("failed to read zip bundle: %v", err)
	}

	expectedEntries := map[string]bool{
		"manifest.json":     false,
		"diagnostics.json":  false,
		"metrics.json":      false,
		"clock_health.json": false,
		"config_shape.json": false,
		"recent_audit.json": false,
	}

	for _, f := range zr.File {
		if _, ok := expectedEntries[f.Name]; ok {
			expectedEntries[f.Name] = true
		}
	}

	for name, found := range expectedEntries {
		if !found {
			t.Errorf("missing expected bundle entry: %s", name)
		}
	}
}
