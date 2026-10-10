package web

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/Hardonian/FamilyBoard/internal/auth"
)

const csrfCookieName = "fb_csrf"
const maxFormRequestBody = 1 << 20

// issueCSRFToken creates a double-submit token for browser forms and JavaScript
// requests. The cookie is HttpOnly so scripts can submit, but cannot read, the
// matching token embedded in the rendered page.
func (s *Server) issueCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if existing, err := r.Cookie(csrfCookieName); err == nil && len(existing.Value) >= 32 {
		return existing.Value
	}
	token, err := auth.GenerateSecureToken(32)
	if err != nil {
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   3600,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.Security.CookieSecure,
	})
	return token
}

func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxFormRequestBody)
		cookie, err := r.Cookie(csrfCookieName)
		provided := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		if provided == "" {
			provided = strings.TrimSpace(r.FormValue("csrf_token"))
		}
		if err != nil || cookie.Value == "" || provided == "" ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(provided)) != 1 {
			http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// rejectCrossSiteMutation is a second line of defense for the localhost kiosk,
// where capability access intentionally does not require an admin CSRF token.
func rejectCrossSiteMutation(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}
	if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
		http.Error(w, "cross-site mutation rejected", http.StatusForbidden)
		return true
	}
	return false
}
