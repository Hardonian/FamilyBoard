package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestGoogleClient_AuthURL_Scenarios(t *testing.T) {
	client := NewGoogleClient("client-id-1", "client-secret-1", "http://localhost/callback")

	// Calendar flow with custom domain email
	calURL := client.AuthURL("state-1", "calendar", "parent@customdomain.family")
	parsed, err := url.Parse(calURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	q := parsed.Query()
	if q.Get("hd") != "customdomain.family" {
		t.Errorf("expected hd=customdomain.family, got %q", q.Get("hd"))
	}
	if !strings.Contains(q.Get("prompt"), "select_account") {
		t.Errorf("expected prompt to contain select_account, got %q", q.Get("prompt"))
	}
	if !strings.Contains(q.Get("scope"), ScopeGoogleCalendar) {
		t.Errorf("expected calendar scope, got %q", q.Get("scope"))
	}

	// Mail flow with gmail.com
	mailURL := client.AuthURL("state-2", "mail", "user@gmail.com")
	parsedMail, _ := url.Parse(mailURL)
	qMail := parsedMail.Query()
	if qMail.Get("hd") != "" {
		t.Errorf("expected empty hd for @gmail.com, got %q", qMail.Get("hd"))
	}
	if !strings.Contains(qMail.Get("scope"), ScopeGoogleGmail) {
		t.Errorf("expected gmail scope, got %q", qMail.Get("scope"))
	}
}

func TestGoogleClient_ExchangeAndUserInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "google-acc-token-123",
				"token_type":    "Bearer",
				"refresh_token": "google-ref-token-123",
				"expires_in":    3600,
			})
		case "/userinfo":
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer google-acc-token-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(GoogleUserInfo{
				Sub:           "google-sub-999",
				Email:         "alex@family.com",
				EmailVerified: true,
				Name:          "Alex Family",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewGoogleClient("id", "secret", "http://localhost/callback")
	client.SetEndpoints(server.URL+"/auth", server.URL+"/token", server.URL+"/userinfo", server.Client())

	tok, info, err := client.Exchange(context.Background(), "auth-code-123")
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}

	if tok.AccessToken != "google-acc-token-123" {
		t.Errorf("unexpected access token: %q", tok.AccessToken)
	}
	if tok.RefreshToken != "google-ref-token-123" {
		t.Errorf("unexpected refresh token: %q", tok.RefreshToken)
	}
	if info.Sub != "google-sub-999" || info.Email != "alex@family.com" {
		t.Errorf("unexpected user info: %+v", info)
	}
}

func TestGoogleClient_RefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-acc-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	client := NewGoogleClient("id", "secret", "http://localhost/callback")
	client.SetEndpoints(server.URL+"/auth", server.URL+"/token", server.URL+"/userinfo", server.Client())

	tok, err := client.RefreshToken(context.Background(), "google-ref-token")
	if err != nil {
		t.Fatalf("RefreshToken failed: %v", err)
	}
	if tok.AccessToken != "refreshed-acc-token" {
		t.Errorf("unexpected refreshed access token: %q", tok.AccessToken)
	}
}

func TestMicrosoftClient_AuthURL_Scenarios(t *testing.T) {
	// 1. Default tenant
	client := NewMicrosoftClient("ms-client-id", "ms-secret", "http://localhost/callback")
	authURL := client.AuthURLWithFlow("state-1", "calendar", "parent@outlook.com")
	if !strings.Contains(authURL, "login.microsoftonline.com/common/oauth2/v2.0/authorize") {
		t.Errorf("expected common tenant in auth url, got %q", authURL)
	}
	if !strings.Contains(authURL, "prompt=select_account") {
		t.Errorf("expected prompt=select_account, got %q", authURL)
	}
	if !strings.Contains(authURL, "login_hint=parent%40outlook.com") {
		t.Errorf("expected login_hint in auth url, got %q", authURL)
	}

	// 2. Custom tenant
	customClient := NewMicrosoftClient("ms-client-id", "ms-secret", "http://localhost/callback", "contoso-uuid-123")
	customAuthURL := customClient.AuthURL("state-2")
	if !strings.Contains(customAuthURL, "login.microsoftonline.com/contoso-uuid-123/oauth2/v2.0/authorize") {
		t.Errorf("expected custom tenant UUID in auth url, got %q", customAuthURL)
	}
}

func TestMicrosoftClient_ExchangeAndProfile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "ms-acc-token-123",
				"token_type":    "Bearer",
				"refresh_token": "ms-ref-token-123",
				"expires_in":    3600,
			})
		case "/profile":
			authHeader := r.Header.Get("Authorization")
			if authHeader != "Bearer ms-acc-token-123" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(MicrosoftUserProfile{
				ID:                "ms-user-id-555",
				DisplayName:       "Jordan Exchange",
				UserPrincipalName: "jordan@office365.com",
				Mail:              "jordan@office365.com",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewMicrosoftClient("id", "secret", "http://localhost/callback")
	client.SetEndpoints(server.URL+"/auth", server.URL+"/token", server.URL+"/profile", server.Client())

	tok, profile, err := client.Exchange(context.Background(), "auth-code-ms")
	if err != nil {
		t.Fatalf("Exchange failed: %v", err)
	}

	if tok.AccessToken != "ms-acc-token-123" {
		t.Errorf("unexpected access token: %q", tok.AccessToken)
	}
	if tok.Email != "jordan@office365.com" {
		t.Errorf("unexpected profile email: %q", tok.Email)
	}
	if profile.ID != "ms-user-id-555" {
		t.Errorf("unexpected profile ID: %q", profile.ID)
	}
}

func TestMicrosoftClient_RefreshToken_Revocation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate revoked refresh token
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	client := NewMicrosoftClient("id", "secret", "http://localhost/callback")
	client.SetEndpoints(server.URL+"/auth", server.URL+"/token", server.URL+"/profile", server.Client())

	_, err := client.RefreshToken(context.Background(), "revoked-ref-token")
	if !errors.Is(err, ErrAccountRevoked) {
		t.Errorf("expected ErrAccountRevoked on 400 invalid_grant, got %v", err)
	}
}
