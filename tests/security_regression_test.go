package tests

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/web"
)

func TestSecurity_CSRF_Rejection(t *testing.T) {
	srv, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	cookie := createAdminCookie(t, store)

	// Post without CSRF token
	form := url.Values{}
	form.Set("display_name", "Malicious Attacker")
	form.Set("email", "attacker@evil.com")

	req := httptest.NewRequest("POST", "/admin/people", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	// Must be rejected (either 403 Forbidden or redirected with err=invalid+csrf)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected CSRF rejection, got 200 OK")
	}
	if rec.Code == http.StatusSeeOther {
		loc := rec.Header().Get("Location")
		if !strings.Contains(loc, "err=") {
			t.Errorf("expected redirection with CSRF error, got: %s", loc)
		}
	}
}

func TestSecurity_PathTraversal_Rejection(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	traversalPaths := []string{
		"/photos/../../etc/passwd",
		"/photos/..\\..\\windows\\win.ini",
		"/photos/%2e%2e%2f%2e%2e%2fconfig.toml",
	}

	for _, path := range traversalPaths {
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			t.Errorf("path traversal attempt %q succeeded with 200 OK", path)
		}
	}
}

func TestSecurity_PhotoUpload_PolyglotAndMIMEConfusion(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// Fake image with valid extension but malicious script contents
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("photo", "malicious.png")
	part.Write([]byte("<?php echo 'malicious shell'; ?>"))
	writer.Close()

	req := httptest.NewRequest("POST", "/api/photos/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:54321" // Simulate kiosk localhost write access
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("polyglot photo upload succeeded with 200 OK, expected rejection")
	}
}

func TestSecurity_PhotoUpload_ValidImageAccepted(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	// Create valid PNG image
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for x := 0; x < 100; x++ {
		for y := 0; y < 100; y++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("failed to encode valid png: %v", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("photo", "family_dog.png")
	part.Write(pngBuf.Bytes())
	writer.Close()

	req := httptest.NewRequest("POST", "/api/photos/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid photo, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func csrfForTest(t *testing.T, srv *web.Server, path string) (*http.Cookie, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "fb_csrf" && cookie.Value != "" {
			return cookie, cookie.Value
		}
	}
	t.Fatalf("expected CSRF cookie from %s", path)
	return nil, ""
}

func TestSecurity_RateLimit_BruteForceProtection(t *testing.T) {
	srv, _, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	limiterIP := "198.51.100.42"

	// Trigger repeated failed logins
	var lastStatus int
	for i := 0; i < 7; i++ {
		csrfCookie, csrfToken := csrfForTest(t, srv, "/login")

		form := url.Values{}
		form.Set("username", "admin")
		form.Set("password", "wrong-password-guess")
		form.Set("csrf_token", csrfToken)

		req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", limiterIP)
		req.RemoteAddr = limiterIP + ":12345"
		req.AddCookie(csrfCookie)
		rec := httptest.NewRecorder()

		srv.ServeHTTP(rec, req)
		lastStatus = rec.Code
	}

	// Rate limiter should kick in (429 Too Many Requests or redirect with rate-limited error)
	if lastStatus != http.StatusTooManyRequests && lastStatus != http.StatusSeeOther {
		t.Errorf("expected rate-limiting response, got: %d", lastStatus)
	}
}

func TestSecurity_PairingReplayProtection(t *testing.T) {
	_, store, tempDir := setupTestServer(t)
	defer os.RemoveAll(tempDir)

	ctx := context.Background()
	rawCode := "PAIR-123456"
	codeHash := auth.HashToken(rawCode)

	err := store.CreatePairingCode(ctx, codeHash, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatalf("create pairing code: %v", err)
	}

	// 1st consumption -> succeeds
	consumed, err := store.ConsumePairingCode(ctx, codeHash)
	if err != nil || !consumed {
		t.Fatalf("first consumption should succeed: %v", err)
	}

	// 2nd consumption -> replay attack rejected
	replayConsumed, err := store.ConsumePairingCode(ctx, codeHash)
	if err != nil {
		t.Fatalf("replay error: %v", err)
	}
	if replayConsumed {
		t.Fatalf("replay consumption should return false, but returned true")
	}
}

func TestSecurity_AccountIsolation_CrossAssignment(t *testing.T) {
	expectedEmail := "alex@example.com"
	attackerEmail := "jordan@example.com"

	err := oauth.VerifyAccountIsolation(expectedEmail, attackerEmail)
	if err == nil {
		t.Fatalf("expected account isolation error when identities mismatch")
	}

	errMatching := oauth.VerifyAccountIsolation(expectedEmail, expectedEmail)
	if errMatching != nil {
		t.Fatalf("expected no error when identities match: %v", errMatching)
	}
}
