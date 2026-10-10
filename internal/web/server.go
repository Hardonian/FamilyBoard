package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/config"
	"github.com/Hardonian/FamilyBoard/internal/db"
	"github.com/Hardonian/FamilyBoard/internal/hardware"
	"github.com/Hardonian/FamilyBoard/internal/mail/parser"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/queue"
	calsync "github.com/Hardonian/FamilyBoard/internal/sync"
	webassets "github.com/Hardonian/FamilyBoard/web"
)

type Server struct {
	cfg          *config.Config
	store        *db.Store
	hwMgr        *hardware.Manager
	tokenStorage *oauth.TokenStorage
	stateManager *oauth.StateManager
	rateLimiter  *auth.LoginRateLimiter
	googleOAuth  *oauth.GoogleClient
	msOAuth      *oauth.MicrosoftClient
	syncEngine   *calsync.Engine
	queueEngine  *queue.Engine
	nlParser     *parser.NaturalLanguageParser
	templates    map[string]*template.Template
	tokenMu      sync.Mutex
	setupMu      sync.Mutex
	sseMu        sync.Mutex
	sseClients   map[chan string]struct{}
	mux          *http.ServeMux
}

func NewServer(
	cfg *config.Config,
	store *db.Store,
	tokenStorage *oauth.TokenStorage,
	stateManager *oauth.StateManager,
	googleOAuth *oauth.GoogleClient,
	msOAuth *oauth.MicrosoftClient,
	syncEngine *calsync.Engine,
	queueEngine *queue.Engine,
) (*Server, error) {
	if err := cfg.EnsureSessionSecret(); err != nil {
		return nil, fmt.Errorf("initialize session secret: %w", err)
	}
	if err := cfg.EnsureHotspotPassword(); err != nil {
		return nil, fmt.Errorf("initialize hotspot password: %w", err)
	}
	s := &Server{
		cfg:          cfg,
		store:        store,
		hwMgr:        hardware.NewManager(),
		tokenStorage: tokenStorage,
		stateManager: stateManager,
		rateLimiter:  auth.NewLoginRateLimiter(5, 10*time.Minute),
		googleOAuth:  googleOAuth,
		msOAuth:      msOAuth,
		syncEngine:   syncEngine,
		queueEngine:  queueEngine,
		nlParser:     parser.NewNaturalLanguageParser(cfg.Server.Timezone),
		sseClients:   make(map[chan string]struct{}),
		mux:          http.NewServeMux(),
	}
	s.hwMgr.Wifi().SetHotspotPassword(cfg.Security.HotspotPassword)
	s.loadPresenceBeacons(context.Background())

	if err := s.initTemplates(); err != nil {
		return nil, fmt.Errorf("init templates: %w", err)
	}
	photosDir := filepath.Join(filepath.Dir(cfg.Database.Path), "photos")
	if err := os.MkdirAll(photosDir, 0755); err != nil {
		return nil, fmt.Errorf("create photos directory: %w", err)
	}

	s.routes()
	return s, nil
}

// HardwareManager returns the appliance hardware controller.
func (s *Server) HardwareManager() *hardware.Manager {
	return s.hwMgr
}

func (s *Server) initTemplates() error {
	s.templates = make(map[string]*template.Template)

	tmplFuncs := template.FuncMap{
		"mul": func(a, b float64) float64 {
			return a * b
		},
		"asset": assetURL,
	}

	pages := []string{"kiosk.html", "login.html", "setup.html", "admin.html"}
	for _, page := range pages {
		tmpl, err := template.New("").Funcs(tmplFuncs).ParseFS(webassets.Content, "templates/layout.html", "templates/"+page)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", page, err)
		}
		s.templates[page] = tmpl
	}

	return nil
}

// assetURL fingerprints embedded assets so long-lived kiosk browsers can cache
// aggressively without serving stale code after an appliance update.
func assetURL(path string) string {
	contentPath := strings.TrimPrefix(path, "/")
	data, err := webassets.Content.ReadFile(contentPath)
	if err != nil {
		return path
	}
	sum := sha256.Sum256(data)
	return path + "?v=" + hex.EncodeToString(sum[:8])
}

func (s *Server) routes() {
	// Embedded static assets with versioned immutable caching
	staticFS, err := fs.Sub(webassets.Content, "static")
	if err != nil {
		panic(err)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	s.mux.Handle("GET /static/", withStaticCache(staticHandler))

	// Health Endpoints
	s.mux.HandleFunc("GET /health/live", s.handleHealthLive)
	s.mux.HandleFunc("GET /health/ready", s.handleHealthReady)

	// Kiosk Endpoints
	s.mux.HandleFunc("GET /{$}", s.handleKiosk)
	s.mux.HandleFunc("GET /api/calendars", s.handleAPICalendars)
	s.mux.HandleFunc("GET /api/events", s.handleAPIGetEvents)
	s.mux.HandleFunc("POST /api/events", s.requireKioskWriteAccess(s.handleAPICreateEvent))
	s.mux.HandleFunc("PUT /api/events/{id}", s.requireKioskWriteAccess(s.handleAPIUpdateEvent))
	s.mux.HandleFunc("DELETE /api/events/{id}", s.requireKioskWriteAccess(s.handleAPIDeleteEvent))
	s.mux.HandleFunc("POST /api/events/{id}/restore", s.requireKioskWriteAccess(s.handleAPIRestoreEvent))
	s.mux.HandleFunc("GET /api/kiosk/templates", s.handleAPIGetTemplates)
	s.mux.HandleFunc("POST /api/kiosk/parse-natural", s.handleAPIParseNatural)
	s.mux.HandleFunc("GET /api/events/stream", s.handleSSEStream)

	// Auth & Setup
	s.mux.HandleFunc("GET /setup", s.handleSetupGet)
	s.mux.HandleFunc("POST /setup", s.requireCSRF(s.handleSetupPost))
	s.mux.HandleFunc("GET /login", s.handleLoginGet)
	s.mux.HandleFunc("POST /login", s.requireCSRF(s.handleLoginPost))
	s.mux.HandleFunc("POST /logout", s.requireAuth(s.requireCSRF(s.handleLogout)))

	// Admin Dashboard
	s.mux.HandleFunc("GET /admin", s.requireAuth(s.handleAdminDashboard))
	s.mux.HandleFunc("POST /admin/lock", s.requireAuth(s.requireCSRF(s.handleAdminLock)))
	s.mux.HandleFunc("POST /admin/sync-now", s.requireAuth(s.requireCSRF(s.handleAdminSyncNow)))
	s.mux.HandleFunc("POST /admin/calendars/refresh", s.requireAuth(s.requireCSRF(s.handleAdminCalendarsRefresh)))
	s.mux.HandleFunc("POST /admin/calendars/{id}/preferences", s.requireAuth(s.requireCSRF(s.handleAdminCalendarUpdatePreferences)))
	s.mux.HandleFunc("POST /admin/calendars/{id}/set-default", s.requireAuth(s.requireCSRF(s.handleAdminCalendarSetDefault)))
	s.mux.HandleFunc("POST /admin/people/{id}/update", s.requireAuth(s.requireCSRF(s.handleAdminPersonUpdate)))
	s.mux.HandleFunc("POST /admin/candidates/{id}/approve", s.requireAuth(s.requireCSRF(s.handleAdminCandidateApprove)))
	s.mux.HandleFunc("POST /admin/candidates/{id}/reject", s.requireAuth(s.requireCSRF(s.handleAdminCandidateReject)))
	s.mux.HandleFunc("GET /admin/oauth/start", s.requireAuth(s.handleAdminOAuthStart))
	s.mux.HandleFunc("POST /admin/apple/connect", s.requireAuth(s.requireCSRF(s.handleAdminAppleConnect)))
	s.mux.HandleFunc("GET /admin/backup/download", s.requireAuth(s.handleAdminBackupDownload))

	// OAuth Callbacks
	s.mux.HandleFunc("GET /oauth/callback/google", s.handleOAuthCallbackGoogle)
	s.mux.HandleFunc("GET /oauth/callback/microsoft", s.handleOAuthCallbackMicrosoft)

	// Radxa Dragon Appliance Hardware & Network Endpoints
	s.mux.HandleFunc("GET /api/system/hardware", s.requireApplianceAccess(s.handleAPIGetHardware))
	s.mux.HandleFunc("GET /api/system/wifi", s.requireApplianceAccess(s.handleAPIGetWifi))
	s.mux.HandleFunc("GET /api/system/wifi/scan", s.requireApplianceAccess(s.handleAPIScanWifi))
	s.mux.HandleFunc("POST /api/system/wifi/connect", s.requireKioskWriteAccess(s.handleAPIConnectWifi))
	s.mux.HandleFunc("POST /api/system/wifi/hotspot", s.requireKioskWriteAccess(s.handleAPIToggleHotspot))
	s.mux.HandleFunc("GET /api/system/wifi/qrcode", s.requireApplianceAccess(s.handleAPIGetWifiQRCode))
	s.mux.HandleFunc("GET /api/system/bluetooth", s.requireApplianceAccess(s.handleAPIGetBluetooth))
	s.mux.HandleFunc("POST /api/system/bluetooth/power", s.requireKioskWriteAccess(s.handleAPISetBluetoothPower))
	s.mux.HandleFunc("GET /api/system/bluetooth/scan", s.requireApplianceAccess(s.handleAPIScanBluetooth))
	s.mux.HandleFunc("POST /api/system/bluetooth/connect", s.requireKioskWriteAccess(s.handleAPIConnectBluetooth))
	s.mux.HandleFunc("POST /api/system/bluetooth/disconnect", s.requireKioskWriteAccess(s.handleAPIDisconnectBluetooth))
	s.mux.HandleFunc("POST /api/system/bluetooth/remove", s.requireKioskWriteAccess(s.handleAPIRemoveBluetooth))
	s.mux.HandleFunc("GET /api/system/presence", s.requireApplianceAccess(s.handleAPIGetPresence))
	s.mux.HandleFunc("POST /api/system/presence/beacon", s.requireKioskWriteAccess(s.handleAPIRegisterBeacon))
	s.mux.HandleFunc("DELETE /api/system/presence/beacon/{id}", s.requireKioskWriteAccess(s.handleAPIDeleteBeacon))
	s.mux.HandleFunc("POST /api/system/power", s.requireAuth(s.requireCSRF(s.handleAPISystemPower)))

	// Web App Manifest and Service Worker for Cross-Platform Mobile PWA (iPhone & Android)
	s.mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	s.mux.HandleFunc("GET /sw.js", s.handleServiceWorker)

	// Mobile Phone Pairing Endpoints (QR camera pairing)
	s.mux.HandleFunc("GET /api/system/mobile-pair", s.requireApplianceAccess(s.handleAPIGetMobilePair))
	s.mux.HandleFunc("GET /api/system/mobile-pair/qrcode", s.requireApplianceAccess(s.handleAPIGetMobilePairQRCode))

	// FotoCube Ambient Photo Frame & NVMe Migration
	s.mux.HandleFunc("GET /api/photos", s.requireApplianceAccess(s.handleAPIGetPhotos))
	s.mux.HandleFunc("POST /api/photos/upload", s.requireKioskWriteAccess(s.handleAPIUploadPhoto))
	s.mux.HandleFunc("DELETE /api/photos/{filename}", s.requireKioskWriteAccess(s.handleAPIDeletePhoto))
	photosDir := filepath.Join(filepath.Dir(s.cfg.Database.Path), "photos")
	photoHandler := http.StripPrefix("/photos/", http.FileServer(http.Dir(photosDir)))
	s.mux.HandleFunc("GET /photos/", s.requireApplianceAccess(photoHandler.ServeHTTP))
	s.mux.HandleFunc("POST /api/system/storage/migrate-nvme", s.requireAuth(s.requireCSRF(s.handleAPIMigrateNVMe)))
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	data, err := webassets.Content.ReadFile("static/manifest.webmanifest")
	if err != nil {
		http.Error(w, "manifest not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := webassets.Content.ReadFile("static/sw.js")
	if err != nil {
		http.Error(w, "service worker not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Middleware: Security headers & CSP
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
	w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none';")
	if s.cfg.Security.CookieSecure {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
	if rejectCrossSiteMutation(w, r) {
		return
	}

	// Middleware: Logging & Recovery
	defer func() {
		if err := recover(); err != nil {
			slog.Error("HTTP handler panic recovered", "err", err, "path", r.URL.Path)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}()

	withGzip(s.mux).ServeHTTP(w, r)
}

// BroadcastReload notifies all open kiosk clients via SSE to reload event data.
func (s *Server) BroadcastReload() {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseClients {
		select {
		case ch <- "reload":
		default:
		}
	}
}

func (s *Server) render(w http.ResponseWriter, page string, data interface{}) {
	tmpl, ok := s.templates[page]
	if !ok {
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		slog.Error("Template execute failed", "page", page, "err", err)
	}
}

// GetValidAccessToken fulfills queue.TokenProvider interface for background sync and queue engines.
func (s *Server) GetValidAccessToken(ctx context.Context, personID, provider string) (string, error) {
	// Refresh tokens may rotate. Serialize the load/refresh/save sequence so the
	// sync and mutation workers cannot both redeem the same credential.
	s.tokenMu.Lock()
	defer s.tokenMu.Unlock()

	account, err := s.store.GetOAuthAccount(ctx, personID, provider)
	if err != nil || account == nil {
		return "", fmt.Errorf("oauth account not found for person %s provider %s", personID, provider)
	}

	stored, err := s.tokenStorage.Load(account.ID)
	if err != nil {
		return "", fmt.Errorf("load stored token for %s: %w", account.ID, err)
	}

	if provider == "apple" {
		if stored.AccessToken == "" {
			return "", errors.New("empty apple credentials")
		}
		return stored.AccessToken, nil
	}

	// If token expires within 5 minutes, refresh it automatically
	if time.Until(stored.Expiry) < 5*time.Minute && stored.RefreshToken != "" {
		if provider == "google" && s.googleOAuth != nil {
			newTok, err := s.googleOAuth.RefreshToken(ctx, stored.RefreshToken)
			if err != nil {
				return "", fmt.Errorf("refresh google token: %w", err)
			}
			stored.AccessToken = newTok.AccessToken
			stored.Expiry = newTok.Expiry
			if newTok.RefreshToken != "" {
				stored.RefreshToken = newTok.RefreshToken
			}
			if err := s.tokenStorage.Save(account.ID, stored); err != nil {
				return "", fmt.Errorf("save refreshed google token: %w", err)
			}
			account.ExpiresAt = stored.Expiry
			account.Status = "active"
			if err := s.store.UpsertOAuthAccount(ctx, account); err != nil {
				return "", fmt.Errorf("update google token metadata: %w", err)
			}
		} else if provider == "microsoft" && s.msOAuth != nil {
			newTok, err := s.msOAuth.RefreshToken(ctx, stored.RefreshToken)
			if err != nil {
				return "", fmt.Errorf("refresh microsoft token: %w", err)
			}
			stored.AccessToken = newTok.AccessToken
			stored.Expiry = newTok.Expiry
			if newTok.RefreshToken != "" {
				stored.RefreshToken = newTok.RefreshToken
			}
			if err := s.tokenStorage.Save(account.ID, stored); err != nil {
				return "", fmt.Errorf("save refreshed microsoft token: %w", err)
			}
			account.ExpiresAt = stored.Expiry
			account.Status = "active"
			if err := s.store.UpsertOAuthAccount(ctx, account); err != nil {
				return "", fmt.Errorf("update microsoft token metadata: %w", err)
			}
		} else {
			return "", fmt.Errorf("%w: no refresh client configured for provider %s", oauth.ErrTokenExpired, provider)
		}
	}
	if stored.AccessToken == "" || (!stored.Expiry.IsZero() && time.Now().After(stored.Expiry)) {
		return "", oauth.ErrTokenExpired
	}

	return stored.AccessToken, nil
}
