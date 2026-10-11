package tests

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func extractCSRFToken(body string) string {
	const marker = `name="csrf_token" value="`
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.IndexByte(body[start:], '"')
	if end < 0 {
		return ""
	}
	return html.UnescapeString(body[start : start+end])
}

func TestAdminSystemTabAndStorageEndpoints(t *testing.T) {
	srv, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	cookie := createAdminCookie(t, store)

	// 1. GET /admin?tab=system loads diagnostics, clock health, storage cards, and reload boundaries
	getReq := httptest.NewRequest("GET", "/admin?tab=system", nil)
	getReq.AddCookie(cookie)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for /admin?tab=system, got: %d (%s)", getRec.Code, getRec.Body.String())
	}
	body := getRec.Body.String()
	if !strings.Contains(body, "Clock &amp; NTP Health") {
		t.Errorf("system tab missing Clock & NTP Health card")
	}
	if !strings.Contains(body, "Disk Capacity &amp; Storage Maintenance") {
		t.Errorf("system tab missing Disk Capacity card")
	}
	if !strings.Contains(body, "Configuration Reload Boundaries") {
		t.Errorf("system tab missing Configuration Reload Boundaries table")
	}

	// Obtain CSRF token
	csrfCookie, csrfToken := csrfForTest(t, srv, "/login")

	// 2. POST /admin/database/vacuum compacts and optimizes DB
	vacForm := url.Values{}
	vacForm.Set("csrf_token", csrfToken)
	vacReq := httptest.NewRequest("POST", "/admin/database/vacuum", strings.NewReader(vacForm.Encode()))
	vacReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	vacReq.AddCookie(cookie)
	vacReq.AddCookie(csrfCookie)
	vacRec := httptest.NewRecorder()
	srv.ServeHTTP(vacRec, vacReq)

	if vacRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for vacuum, got: %d", vacRec.Code)
	}
	if loc := vacRec.Header().Get("Location"); !strings.Contains(loc, "tab=system") || !strings.Contains(loc, "msg=") {
		t.Errorf("expected redirect to system tab with msg, got: %s", loc)
	}

	// 3. POST /admin/storage/cleanup prunes expired entries and vacuums
	cleanForm := url.Values{}
	cleanForm.Set("csrf_token", csrfToken)
	cleanReq := httptest.NewRequest("POST", "/admin/storage/cleanup", strings.NewReader(cleanForm.Encode()))
	cleanReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cleanReq.AddCookie(cookie)
	cleanReq.AddCookie(csrfCookie)
	cleanRec := httptest.NewRecorder()
	srv.ServeHTTP(cleanRec, cleanReq)

	if cleanRec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for cleanup, got: %d", cleanRec.Code)
	}
	if loc := cleanRec.Header().Get("Location"); !strings.Contains(loc, "tab=system") || !strings.Contains(loc, "msg=") {
		t.Errorf("expected redirect to system tab with msg, got: %s", loc)
	}
}
