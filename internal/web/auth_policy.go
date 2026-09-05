package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/models"
)

const (
	kioskCapabilityCookieName = "fb_kiosk_cap"
	kioskHeaderName           = "X-Kiosk-Token"
	adminAutoLockDuration     = 15 * time.Minute
)

// isLocalhost reports whether the incoming request originates from loopback (physical kiosk display).
func (s *Server) isLocalhost(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return ip.IsLoopback()
}

// generateKioskToken creates an HMAC-signed token validating kiosk capability.
func (s *Server) generateKioskToken() string {
	secret := s.cfg.Security.SessionSecret
	if secret == "" {
		secret = "familyboard-appliance-secret-key"
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("kiosk:household:v1"))
	return hex.EncodeToString(h.Sum(nil))
}

// hasKioskCapability checks if the request is from localhost or carries a valid kiosk token.
func (s *Server) hasKioskCapability(r *http.Request) bool {
	// 1. Direct physical loopback kiosk
	if s.isLocalhost(r) {
		return true
	}

	expected := s.generateKioskToken()

	// 2. Cookie capability
	if cookie, err := r.Cookie(kioskCapabilityCookieName); err == nil && cookie.Value != "" {
		if hmac.Equal([]byte(cookie.Value), []byte(expected)) {
			return true
		}
	}

	// 3. Header capability
	if hdr := r.Header.Get(kioskHeaderName); hdr != "" {
		if hmac.Equal([]byte(hdr), []byte(expected)) {
			return true
		}
	}

	return false
}

// issueKioskCapabilityCookie sets the signed kiosk capability cookie for localhost requests.
func (s *Server) issueKioskCapabilityCookie(w http.ResponseWriter, r *http.Request) {
	if s.isLocalhost(r) {
		http.SetCookie(w, &http.Cookie{
			Name:     kioskCapabilityCookieName,
			Value:    s.generateKioskToken(),
			Path:     "/",
			MaxAge:   86400 * 365, // 1 year on physical display
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// isAuthenticatedAdmin verifies if the request carries an active, non-expired admin session.
func (s *Server) isAuthenticatedAdmin(r *http.Request) (*models.Session, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil, false
	}

	tokenHash := auth.HashToken(cookie.Value)
	sess, err := s.store.GetSession(r.Context(), tokenHash)
	if err != nil || sess == nil {
		return nil, false
	}

	// Check 15-minute auto-lock
	if time.Since(sess.LastActiveAt) > adminAutoLockDuration {
		_ = s.store.DeleteSession(r.Context(), tokenHash)
		return nil, false
	}

	return sess, true
}

// requireKioskWriteAccess enforces the privilege boundary for write mutations (POST/PUT/DELETE).
// Physical localhost kiosk or devices with kiosk capability token or admin session are permitted.
func (s *Server) requireKioskWriteAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow authenticated admin
		if _, ok := s.isAuthenticatedAdmin(r); ok {
			next(w, r)
			return
		}

		// Allow verified household kiosk
		if s.hasKioskCapability(r) {
			next(w, r)
			return
		}

		// Reject remote LAN clients attempting unauthenticated writes
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"error":"unauthorized","message":"Calendar modifications from remote LAN devices require admin login. Physical kiosk has direct access."}`)
	}
}

// isCalendarWritable verifies whether a target calendar exists, is enabled for display, and is marked writable.
func (s *Server) isCalendarWritable(ctx context.Context, calendarID string) (*models.Calendar, error) {
	cal, err := s.store.GetCalendar(ctx, calendarID)
	if err != nil || cal == nil {
		return nil, fmt.Errorf("calendar %q not found", calendarID)
	}

	if !cal.EnabledForDisplay {
		return nil, fmt.Errorf("calendar %q is disabled for display", cal.Name)
	}

	if !cal.Writable {
		return nil, fmt.Errorf("calendar %q is read-only and cannot accept new events", cal.Name)
	}

	return cal, nil
}
