package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strings"
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
	host := remoteIP(r)
	ip := net.ParseIP(host)
	if ip == nil {
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	}
	return ip.IsLoopback()
}

// remoteIP normalizes RemoteAddr so rate limiting cannot be bypassed simply by
// opening a new TCP connection with a different ephemeral source port.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// generateKioskToken creates an HMAC-signed token validating kiosk capability.
func (s *Server) generateKioskToken() string {
	secret := s.cfg.Security.SessionSecret
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
			SameSite: http.SameSiteStrictMode,
			Secure:   s.cfg.Security.CookieSecure,
		})
	}
}

// generatePairingToken creates an HMAC-signed, time-bounded pairing token for mobile QR pairing.
func (s *Server) generatePairingToken(ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	msg := fmt.Sprintf("pair:%d", exp)
	h := hmac.New(sha256.New, []byte(s.cfg.Security.SessionSecret))
	h.Write([]byte(msg))
	sig := hex.EncodeToString(h.Sum(nil))
	return fmt.Sprintf("%d.%s", exp, sig)
}

// verifyPairingToken validates the mobile pairing token signature and ensures it has not expired.
func (s *Server) verifyPairingToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	var exp int64
	if _, err := fmt.Sscanf(parts[0], "%d", &exp); err != nil {
		return false
	}
	if time.Now().Unix() > exp {
		return false
	}
	msg := fmt.Sprintf("pair:%d", exp)
	h := hmac.New(sha256.New, []byte(s.cfg.Security.SessionSecret))
	h.Write([]byte(msg))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// issueMobileKioskCapabilityCookie grants trusted household write capability to a paired smartphone.
func (s *Server) issueMobileKioskCapabilityCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     kioskCapabilityCookieName,
		Value:    s.generateKioskToken(),
		Path:     "/",
		MaxAge:   86400 * 365, // 1 year trusted household phone
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode, // Lax allows top-level navigation from camera QR scan / PWA icon
		Secure:   s.cfg.Security.CookieSecure,
	})
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

	// Check the absolute lifetime as well as the 15-minute inactivity lock.
	if time.Now().After(sess.ExpiresAt) || time.Since(sess.LastActiveAt) > adminAutoLockDuration {
		_ = s.store.DeleteSession(r.Context(), tokenHash)
		return nil, false
	}

	return sess, true
}

// requireKioskWriteAccess enforces the privilege boundary for write mutations (POST/PUT/DELETE).
// Physical localhost kiosk or devices with kiosk capability token or admin session are permitted.
func (s *Server) requireKioskWriteAccess(next http.HandlerFunc) http.HandlerFunc {
	return s.requireApplianceAccess(next)
}

// requireApplianceAccess protects household-sensitive appliance APIs. Access is
// granted to the physical loopback kiosk, a signed kiosk capability, or an
// authenticated administrator.
func (s *Server) requireApplianceAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, private")
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
		fmt.Fprint(w, `{"error":"unauthorized","message":"This appliance operation requires admin login or the physical kiosk."}`)
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
