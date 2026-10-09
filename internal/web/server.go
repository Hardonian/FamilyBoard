package web

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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

	if err := s.initTemplates(); err != nil {
		return nil, fmt.Errorf("init templates: %w", err)
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

func (s *Server) routes() {
	// Embedded static assets with versioned immutable caching
	staticFS, err := fs.Sub(webassets.Content, "static")
	if err != nil {
		panic(err)
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	s.mux.Handle("GET /static/", withStaticCache("1.0.0", staticHandler))

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
	s.mux.HandleFunc("POST /setup", s.handleSetupPost)
	s.mux.HandleFunc("GET /login", s.handleLoginGet)
	s.mux.HandleFunc("POST /login", s.handleLoginPost)
	s.mux.HandleFunc("GET /logout", s.handleLogout)

	// Admin Dashboard
	s.mux.HandleFunc("GET /admin", s.requireAuth(s.handleAdminDashboard))
	s.mux.HandleFunc("GET /admin/lock", s.handleAdminLock)
	s.mux.HandleFunc("POST /admin/sync-now", s.requireAuth(s.handleAdminSyncNow))
	s.mux.HandleFunc("POST /admin/calendars/refresh", s.requireAuth(s.handleAdminCalendarsRefresh))
	s.mux.HandleFunc("POST /admin/calendars/{id}/preferences", s.requireAuth(s.handleAdminCalendarUpdatePreferences))
	s.mux.HandleFunc("POST /admin/calendars/{id}/set-default", s.requireAuth(s.handleAdminCalendarSetDefault))
	s.mux.HandleFunc("POST /admin/people/{id}/update", s.requireAuth(s.handleAdminPersonUpdate))
	s.mux.HandleFunc("POST /admin/candidates/{id}/approve", s.requireAuth(s.handleAdminCandidateApprove))
	s.mux.HandleFunc("POST /admin/candidates/{id}/reject", s.requireAuth(s.handleAdminCandidateReject))
	s.mux.HandleFunc("GET /admin/oauth/start", s.requireAuth(s.handleAdminOAuthStart))
	s.mux.HandleFunc("GET /admin/backup/download", s.requireAuth(s.handleAdminBackupDownload))

	// OAuth Callbacks
	s.mux.HandleFunc("GET /oauth/callback/google", s.handleOAuthCallbackGoogle)
	s.mux.HandleFunc("GET /oauth/callback/microsoft", s.handleOAuthCallbackMicrosoft)

	// Radxa Dragon Appliance Hardware & Network Endpoints
	s.mux.HandleFunc("GET /api/system/hardware", s.handleAPIGetHardware)
	s.mux.HandleFunc("GET /api/system/wifi", s.handleAPIGetWifi)
	s.mux.HandleFunc("GET /api/system/wifi/scan", s.handleAPIScanWifi)
	s.mux.HandleFunc("POST /api/system/wifi/connect", s.requireKioskWriteAccess(s.handleAPIConnectWifi))
	s.mux.HandleFunc("POST /api/system/wifi/hotspot", s.requireKioskWriteAccess(s.handleAPIToggleHotspot))
	s.mux.HandleFunc("GET /api/system/bluetooth", s.handleAPIGetBluetooth)
	s.mux.HandleFunc("POST /api/system/bluetooth/power", s.requireKioskWriteAccess(s.handleAPISetBluetoothPower))
	s.mux.HandleFunc("GET /api/system/bluetooth/scan", s.handleAPIScanBluetooth)
	s.mux.HandleFunc("GET /api/system/presence", s.handleAPIGetPresence)
	s.mux.HandleFunc("POST /api/system/presence/beacon", s.requireKioskWriteAccess(s.handleAPIRegisterBeacon))
	s.mux.HandleFunc("DELETE /api/system/presence/beacon/{id}", s.requireKioskWriteAccess(s.handleAPIDeleteBeacon))
	s.mux.HandleFunc("POST /api/system/power", s.requireAuth(s.handleAPISystemPower))

	// FotoCube Ambient Photo Frame & NVMe Migration
	s.mux.HandleFunc("GET /api/photos", s.handleAPIGetPhotos)
	photosDir := filepath.Join(filepath.Dir(s.cfg.Database.Path), "photos")
	_ = os.MkdirAll(photosDir, 0755)
	s.mux.Handle("GET /photos/", http.StripPrefix("/photos/", http.FileServer(http.Dir(photosDir))))
	s.mux.HandleFunc("POST /api/system/storage/migrate-nvme", s.requireAuth(s.handleAPIMigrateNVMe))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Middleware: Security headers & CSP
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none';")

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
	account, err := s.store.GetOAuthAccount(ctx, personID, provider)
	if err != nil || account == nil {
		return "", fmt.Errorf("oauth account not found for person %s provider %s", personID, provider)
	}

	stored, err := s.tokenStorage.Load(account.ID)
	if err != nil {
		return "", fmt.Errorf("load stored token for %s: %w", account.ID, err)
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
			_ = s.tokenStorage.Save(account.ID, stored)
		}
	}

	return stored.AccessToken, nil
}
