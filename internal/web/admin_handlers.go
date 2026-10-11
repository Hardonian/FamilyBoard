package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/diagnostics"
	"github.com/Hardonian/FamilyBoard/internal/hardware"
	"github.com/Hardonian/FamilyBoard/internal/maintenance"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
	"github.com/Hardonian/FamilyBoard/internal/timezone"
)

const sessionCookieName = "fb_session"

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, private")
		sess, ok := s.isAuthenticatedAdmin(r)
		if !ok || sess == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
				Secure:   s.cfg.Security.CookieSecure,
			})
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}

		cookie, _ := r.Cookie(sessionCookieName)
		if cookie != nil {
			_ = s.store.TouchSession(r.Context(), auth.HashToken(cookie.Value))
		}
		next(w, r)
	}
}

func (s *Server) handleAdminLock(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		tokenHash := auth.HashToken(cookie.Value)
		_ = s.store.DeleteSession(r.Context(), tokenHash)
	}
	s.recordAudit(r, "admin_lock", "session", "current", "Locked admin controls and returned to kiosk")

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.Security.CookieSecure,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	csrfToken := s.issueCSRFToken(w, r)
	s.render(w, "login.html", map[string]interface{}{
		"Title":     "Admin Login",
		"Theme":     "dark",
		"CSRFToken": csrfToken,
		"Username":  s.cfg.Auth.AdminUsername,
		"Next":      safeLocalRedirect(r.URL.Query().Get("next"), "/admin"),
	})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	ip := remoteIP(r)
	next := safeLocalRedirect(r.FormValue("next"), "/admin")
	csrfToken := r.FormValue("csrf_token")
	if !s.rateLimiter.Allow(ip) {
		w.WriteHeader(http.StatusTooManyRequests)
		s.render(w, "login.html", map[string]interface{}{
			"Title": "Admin Login", "Theme": "dark", "CSRFToken": csrfToken,
			"Next": next, "Error": "Too many failed attempts. Please try again later.",
		})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username != s.cfg.Auth.AdminUsername || !auth.VerifyPassword(s.cfg.Auth.AdminPasswordHash, password) {
		s.rateLimiter.RecordFailure(ip)
		slog.Warn("Failed admin login attempt", "username", username, "ip", ip)
		_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
			ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
			Actor:        username,
			Action:       "login_failed",
			ResourceType: "auth",
			Details:      "Invalid credentials",
			IPAddress:    ip,
		})

		s.render(w, "login.html", map[string]interface{}{
			"Title": "Admin Login", "Theme": "dark", "CSRFToken": csrfToken,
			"Username": username, "Next": next, "Error": "Invalid username or password",
		})
		return
	}

	s.rateLimiter.Reset(ip)

	// Create session
	token, err := auth.GenerateSecureToken(32)
	if err != nil {
		slog.Error("Failed to generate admin session token", "err", err)
		http.Error(w, "could not create admin session", http.StatusInternalServerError)
		return
	}
	tokenHash := auth.HashToken(token)
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	if err := s.store.CreateSession(r.Context(), &models.Session{
		TokenHash:    tokenHash,
		UserID:       username,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		LastActiveAt: now,
	}); err != nil {
		slog.Error("Failed to persist admin session", "err", err)
		http.Error(w, "could not create admin session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.Security.CookieSecure,
	})

	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        username,
		Action:       "login_success",
		ResourceType: "auth",
		Details:      "Successful admin session creation",
		IPAddress:    ip,
	})

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func safeLocalRedirect(candidate, fallback string) string {
	if !strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "//") || strings.Contains(candidate, "\\") {
		return fallback
	}
	parsed, err := url.Parse(candidate)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return fallback
	}
	return candidate
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		tokenHash := auth.HashToken(cookie.Value)
		_ = s.store.DeleteSession(r.Context(), tokenHash)
	}
	s.recordAudit(r, "logout", "session", "current", "Signed out of administration")

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.Security.CookieSecure,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	meta, err := s.store.GetDeviceMetadata(r.Context())
	if err == nil && meta != nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	csrfToken := s.issueCSRFToken(w, r)
	s.render(w, "setup.html", map[string]interface{}{
		"Title":             "First-Run Setup",
		"Theme":             "dark",
		"CSRFToken":         csrfToken,
		"DeviceName":        s.cfg.Server.DeviceName,
		"Timezone":          s.cfg.Server.Timezone,
		"GoogleClientID":    s.cfg.Google.ClientID,
		"GoogleRedirectURL": s.cfg.Google.RedirectURL,
	})
}

func (s *Server) handleSetupDiagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	report, err := diagnostics.Generate(r.Context(), s.store, s.cfg.Database.Path)
	if err != nil {
		http.Error(w, "generate diagnostics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"familyboard-setup-diagnostics-%s.json\"", time.Now().Format("20060102-150405")))
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, private")
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	meta, err := s.store.GetDeviceMetadata(r.Context())
	if err != nil {
		http.Error(w, "failed to check appliance setup state", http.StatusInternalServerError)
		return
	}
	if meta != nil {
		http.Error(w, "appliance setup has already been completed", http.StatusConflict)
		return
	}

	deviceName := strings.TrimSpace(r.FormValue("device_name"))
	tz := strings.TrimSpace(r.FormValue("timezone"))
	password := r.FormValue("admin_password")
	passwordConfirm := r.FormValue("admin_password_confirm")
	googleClientID := strings.TrimSpace(r.FormValue("google_client_id"))
	googleSecret := strings.TrimSpace(r.FormValue("google_client_secret"))

	if password != passwordConfirm {
		s.render(w, "setup.html", map[string]interface{}{
			"Title": "First-Run Setup", "Theme": "dark", "Error": "Passwords do not match",
			"DeviceName": deviceName, "Timezone": tz, "GoogleClientID": googleClientID, "GoogleRedirectURL": s.cfg.Google.RedirectURL,
			"CSRFToken": r.FormValue("csrf_token"),
		})
		return
	}

	if len(password) < 8 {
		s.render(w, "setup.html", map[string]interface{}{
			"Title": "First-Run Setup", "Theme": "dark", "Error": "Password must be at least 8 characters",
			"DeviceName": deviceName, "Timezone": tz, "GoogleClientID": googleClientID, "GoogleRedirectURL": s.cfg.Google.RedirectURL,
			"CSRFToken": r.FormValue("csrf_token"),
		})
		return
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "hash password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Validate and persist a copy so a failed save cannot leave the running
	// server with a partially applied first-run configuration.
	updatedCfg := *s.cfg
	updatedCfg.Server.DeviceName = deviceName
	updatedCfg.Server.Timezone = tz
	updatedCfg.Auth.AdminPasswordHash = hashedPassword
	if googleClientID != "" {
		updatedCfg.Google.ClientID = googleClientID
	}
	if googleSecret != "" {
		updatedCfg.Google.ClientSecret = googleSecret
	}

	if err := updatedCfg.Save(s.cfg.SourcePath("config.toml")); err != nil {
		http.Error(w, "failed to save appliance configuration", http.StatusInternalServerError)
		return
	}
	*s.cfg = updatedCfg
	s.googleOAuth = oauth.NewGoogleClient(
		updatedCfg.Google.ClientID,
		updatedCfg.Google.ClientSecret,
		updatedCfg.Google.RedirectURL,
	)

	ctx := r.Context()
	now := time.Now()

	// Process dynamic family members. Provider calendars are discovered only
	// after OAuth/CalDAV connection, when a valid owning account exists.
	member1Name := strings.TrimSpace(r.FormValue("member1_name"))
	member1Email := strings.TrimSpace(r.FormValue("member1_email"))
	member2Name := strings.TrimSpace(r.FormValue("member2_name"))
	member2Email := strings.TrimSpace(r.FormValue("member2_email"))

	if member1Name == "" {
		member1Name = "Family Member 1"
	}
	if member2Name == "" {
		member2Name = "Family Member 2"
	}

	if member1Email != "" {
		if err := s.store.UpsertPerson(ctx, &models.Person{
			ID:          "person-1",
			DisplayName: member1Name,
			Email:       member1Email,
			Enabled:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			http.Error(w, "failed to save first family member", http.StatusInternalServerError)
			return
		}
	}

	if member2Email != "" {
		if err := s.store.UpsertPerson(ctx, &models.Person{
			ID:          "person-2",
			DisplayName: member2Name,
			Email:       member2Email,
			Enabled:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		}); err != nil {
			http.Error(w, "failed to save second family member", http.StatusInternalServerError)
			return
		}
	}

	// Write the initialized marker last. If an earlier write fails, setup stays
	// retryable instead of trapping the appliance in a partially initialized state.
	if err := s.store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    deviceName,
		Timezone:      tz,
		InitializedAt: now,
		Version:       buildinfo.Version,
		KioskSettings: "{}",
	}); err != nil {
		http.Error(w, "failed to finish appliance initialization", http.StatusInternalServerError)
		return
	}

	// Provision default sovereign local family calendar if no calendars exist yet.
	// This ensures the kitchen touchscreen and mobile devices have a fully functional,
	// zero-cloud private calendar available immediately upon completion.
	existingCals, _ := s.store.ListCalendars(ctx)
	if len(existingCals) == 0 && member1Email != "" {
		localAccount := &models.OAuthAccount{
			ID:                  "acc-local-household",
			PersonID:            "person-1",
			Provider:            "local",
			ProviderAccountID:   "household",
			VerifiedEmail:       member1Email,
			Scopes:              "local",
			CredentialReference: "none",
			ExpiresAt:           now.AddDate(100, 0, 0),
			Status:              "active",
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := s.store.UpsertOAuthAccount(ctx, localAccount); err != nil {
			slog.Warn("Failed to provision default household account during setup", "err", err)
		} else {
			defaultCal := &models.Calendar{
				ID:                 "cal-family-default",
				OAuthAccountID:     localAccount.ID,
				Provider:           "local",
				ProviderCalendarID: "local-family",
				Name:               "Family",
				Timezone:           tz,
				DisplayColor:       "#3b82f6",
				EnabledForDisplay:  true,
				Writable:           true,
				DefaultWriteTarget: true,
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			if err := s.store.UpsertCalendar(ctx, defaultCal); err != nil {
				slog.Warn("Failed to provision default family calendar during setup", "err", err)
			}
		}
	}

	// Create initial admin session
	token, err := auth.GenerateSecureToken(32)
	if err != nil {
		slog.Error("Failed to generate setup session token", "err", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tokenHash := auth.HashToken(token)
	expiresAt := now.Add(24 * time.Hour)

	if err := s.store.CreateSession(ctx, &models.Session{
		TokenHash:    tokenHash,
		UserID:       s.cfg.Auth.AdminUsername,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		LastActiveAt: now,
	}); err != nil {
		slog.Error("Failed to persist setup session", "err", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   s.cfg.Security.CookieSecure,
	})

	s.issueKioskCapabilityCookie(w, r)
	http.Redirect(w, r, "/admin?tab=accounts", http.StatusSeeOther)
}

// DetectEmailProvider inspects an email domain and returns "apple", "microsoft", or "google".
func DetectEmailProvider(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if strings.HasSuffix(email, "@icloud.com") || strings.HasSuffix(email, "@me.com") || strings.HasSuffix(email, "@mac.com") {
		return "apple"
	}
	if strings.HasSuffix(email, "@outlook.com") || strings.HasSuffix(email, "@hotmail.com") ||
		strings.HasSuffix(email, "@live.com") || strings.HasSuffix(email, "@msn.com") ||
		strings.HasSuffix(email, "@office365.com") || strings.HasSuffix(email, "@onmicrosoft.com") ||
		strings.Contains(email, "exchange") {
		return "microsoft"
	}
	return "google"
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	switch tab {
	case "", "accounts", "calendars", "mail", "candidates", "conflicts", "queue", "senders", "audit", "privacy", "hardware", "system":
	default:
		tab = "accounts"
	}
	if tab == "" {
		tab = "accounts"
	}

	ctx := r.Context()
	people, _ := s.store.ListPeople(ctx)
	peopleMap := make(map[string]models.Person, len(people))
	for _, person := range people {
		peopleMap[person.ID] = person
	}
	accounts, _ := s.store.ListOAuthAccounts(ctx)
	accountsMap := make(map[string]models.OAuthAccount)
	accountsByPerson := make(map[string][]models.OAuthAccount)
	for _, a := range accounts {
		accountsMap[a.PersonID] = a
		accountsByPerson[a.PersonID] = append(accountsByPerson[a.PersonID], a)
	}

	providerMap := make(map[string]string)
	for _, p := range people {
		providerMap[p.ID] = DetectEmailProvider(p.Email)
	}

	calendars, _ := s.store.ListCalendars(ctx)
	mailSources, _ := s.store.ListMailSources(ctx)
	trustedSenders, _ := s.store.ListTrustedSenders(ctx)
	candidates, _ := s.store.ListCandidates(ctx, models.CandidatePending, 50, 0)
	queueItems, _ := s.store.ListAllMutations(ctx, 50, 0)
	pendingCandidateCount, _ := s.store.CountPendingCandidates(ctx)
	pendingQueueCount, _ := s.store.CountPendingMutations(ctx)
	deadLetterCount, _ := s.store.CountDeadLetterMutations(ctx)
	activeEventCount, _ := s.store.CountActiveEvents(ctx)
	conflicts, _ := s.store.ListPendingConflicts(ctx)
	pairedDevices, _ := s.store.ListPairedDevices(ctx)
	deletedEventRetention, _ := s.store.GetSetting(ctx, "deleted_event_retention_days")
	if deletedEventRetention == "" {
		deletedEventRetention = "365"
	}
	auditRetention, _ := s.store.GetSetting(ctx, "audit_retention_days")
	if auditRetention == "" {
		auditRetention = "730"
	}
	var auditEntries []models.AuditEntry
	if tab == "audit" {
		auditEntries, _ = s.store.ListAuditLogs(ctx, 100, 0)
	}
	var diagnosticReport *diagnostics.DiagnosticReport
	var clockHealth timezone.ClockHealth
	var metricsSnapshot metrics.MetricsSnapshot
	var databaseSizeMB = "--"
	var memoryAllocMB = "--"
	if tab == "system" {
		diagnosticReport, _ = diagnostics.Generate(ctx, s.store, s.cfg.Database.Path)
		clockHealth = timezone.CheckClockHealth(s.cfg.Server.Timezone)
		if diagnosticReport != nil {
			databaseSizeMB = fmt.Sprintf("%.2f MB", float64(diagnosticReport.Database.SizeBytes)/(1024*1024))
			metrics.Default().RecordDatabaseSize(diagnosticReport.Database.SizeBytes)
		}
		metricsSnapshot = metrics.Default().Snapshot()
		if metricsSnapshot.MemoryAllocBytes > 0 {
			memoryAllocMB = fmt.Sprintf("%.1f MB", float64(metricsSnapshot.MemoryAllocBytes)/(1024*1024))
		}
	}

	csrfToken := s.issueCSRFToken(w, r)

	hwInfo := s.hwMgr.GetSystemInfo(ctx)
	wifiStatus := s.hwMgr.Wifi().GetStatus(ctx)
	btStatus := s.hwMgr.Bluetooth().GetStatus(ctx)
	beacons := s.hwMgr.Bluetooth().ListBeacons()
	var wifiAPs []hardware.WifiAccessPoint
	var btDevices []hardware.BluetoothDevice
	if tab == "hardware" {
		wifiAPs, _ = s.hwMgr.Wifi().ScanNetworks(ctx)
		btDevices, _ = s.hwMgr.Bluetooth().ScanDevices(ctx)
	}

	data := map[string]interface{}{
		"Title":                 "Admin Dashboard",
		"Theme":                 "dark",
		"ActiveTab":             tab,
		"CSRFToken":             csrfToken,
		"People":                people,
		"PeopleMap":             peopleMap,
		"AccountsMap":           accountsMap,
		"AccountsByPerson":      accountsByPerson,
		"ProviderMap":           providerMap,
		"Calendars":             calendars,
		"MailSources":           mailSources,
		"MailSourceCount":       len(mailSources),
		"TrustedSenders":        trustedSenders,
		"Conflicts":             conflicts,
		"PairedDevices":         pairedDevices,
		"DeletedEventRetention": deletedEventRetention,
		"AuditRetention":        auditRetention,
		"AuditEntries":          auditEntries,
		"Candidates":            candidates,
		"QueueItems":            queueItems,
		"PendingCandidateCount": pendingCandidateCount,
		"PendingQueueCount":     pendingQueueCount,
		"DeadLetterCount":       deadLetterCount,
		"PendingConflictCount":  len(conflicts),
		"ActiveEventCount":      activeEventCount,
		"BuildVersion":          buildinfo.Version,
		"Diagnostics":           diagnosticReport,
		"ClockHealth":           clockHealth,
		"MetricsSnapshot":       metricsSnapshot,
		"DatabaseSizeMB":        databaseSizeMB,
		"MemoryAllocMB":         memoryAllocMB,
		"HardwareInfo":          hwInfo,
		"WifiStatus":            wifiStatus,
		"WifiAPs":               wifiAPs,
		"BluetoothStatus":       btStatus,
		"BluetoothDevices":      btDevices,
		"FamilyBeacons":         beacons,
		"Message":               r.URL.Query().Get("msg"),
		"Error":                 r.URL.Query().Get("err"),
		"HotspotPassword":       s.cfg.Security.HotspotPassword,
	}

	s.render(w, "admin.html", data)
}

func (s *Server) handleAdminSyncNow(w http.ResponseWriter, r *http.Request) {
	s.recordAudit(r, "sync_start", "calendar", "*", "Requested immediate synchronization")
	go func() {
		_ = s.syncEngine.SyncAll(context.Background())
		s.BroadcastReload()
	}()

	http.Redirect(w, r, "/admin?tab=calendars&msg=Sync+initiated+in+background", http.StatusSeeOther)
}

func (s *Server) handleAdminCalendarsRefresh(w http.ResponseWriter, r *http.Request) {
	s.recordAudit(r, "calendar_discovery", "calendar", "*", "Requested calendar discovery and synchronization")
	go func() {
		bgCtx := context.Background()
		_ = s.syncEngine.DiscoverCalendars(bgCtx)
		_ = s.syncEngine.SyncAll(bgCtx)
		s.BroadcastReload()
	}()

	http.Redirect(w, r, "/admin?tab=calendars&msg=Calendar+discovery+and+synchronization+initiated+in+background", http.StatusSeeOther)
}

func (s *Server) handleAdminCalendarUpdatePreferences(w http.ResponseWriter, r *http.Request) {
	calendarID := r.PathValue("id")
	if calendarID == "" {
		http.Redirect(w, r, "/admin?tab=calendars&err=Calendar+ID+required", http.StatusSeeOther)
		return
	}

	color := strings.TrimSpace(r.FormValue("display_color"))
	if color == "" {
		color = "#3b82f6"
	}
	enabled := r.FormValue("enabled_for_display") == "1" || r.FormValue("enabled_for_display") == "true" || r.FormValue("enabled_for_display") == "on"
	writable := r.FormValue("writable") == "1" || r.FormValue("writable") == "true" || r.FormValue("writable") == "on"

	if err := s.store.UpdateCalendarPreferences(r.Context(), calendarID, enabled, writable, color); err != nil {
		http.Redirect(w, r, "/admin?tab=calendars&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	s.BroadcastReload()
	s.recordAudit(r, "calendar_preferences", "calendar", calendarID, "Updated visibility, writability, or display color")
	http.Redirect(w, r, "/admin?tab=calendars&msg=Calendar+preferences+updated", http.StatusSeeOther)
}

func (s *Server) handleAdminCalendarSetDefault(w http.ResponseWriter, r *http.Request) {
	calendarID := r.PathValue("id")
	if calendarID == "" {
		http.Redirect(w, r, "/admin?tab=calendars&err=Calendar+ID+required", http.StatusSeeOther)
		return
	}

	if err := s.store.SetDefaultWriteCalendar(r.Context(), calendarID); err != nil {
		http.Redirect(w, r, "/admin?tab=calendars&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	s.BroadcastReload()
	s.recordAudit(r, "calendar_default", "calendar", calendarID, "Changed default write target")
	http.Redirect(w, r, "/admin?tab=calendars&msg=Default+write+calendar+updated", http.StatusSeeOther)
}

func (s *Server) handleAdminPersonUpdate(w http.ResponseWriter, r *http.Request) {
	personID := r.PathValue("id")
	person, err := s.store.GetPerson(r.Context(), personID)
	if err != nil || person == nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Family+member+not+found", http.StatusSeeOther)
		return
	}

	newName := strings.TrimSpace(r.FormValue("display_name"))
	newEmail := strings.TrimSpace(r.FormValue("email"))
	if newName != "" {
		person.DisplayName = newName
	}
	if newEmail != "" {
		person.Email = newEmail
	}

	if err := s.store.UpsertPerson(r.Context(), person); err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "person_update", "person", personID, "Updated family member identity")

	http.Redirect(w, r, "/admin?tab=accounts&msg=Family+member+profile+updated", http.StatusSeeOther)
}

func (s *Server) handleAdminCandidateApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cand, err := s.store.GetCandidate(r.Context(), id)
	if err != nil || cand == nil {
		http.Redirect(w, r, "/admin?tab=candidates&err=Candidate+not+found", http.StatusSeeOther)
		return
	}

	defaultCal, _ := s.store.GetDefaultWriteCalendar(r.Context())
	calendarID := cand.SuggestedCalendarID
	if calendarID == "" && defaultCal != nil {
		calendarID = defaultCal.ID
	}

	if calendarID == "" {
		http.Redirect(w, r, "/admin?tab=candidates&err=No+writable+calendar+configured", http.StatusSeeOther)
		return
	}

	// Create local event
	now := time.Now()
	eventID := fmt.Sprintf("evt-local-%d", now.UnixNano())
	localEvent := &models.Event{
		ID:             eventID,
		Provider:       "google",
		CalendarID:     calendarID,
		Title:          cand.Title,
		Description:    cand.Description,
		Location:       cand.Location,
		StartAt:        cand.StartAt,
		EndAt:          cand.EndAt,
		Timezone:       cand.Timezone,
		AllDay:         cand.AllDay,
		Status:         "confirmed",
		Source:         "email_candidate",
		LocalCreatedAt: now,
		LocalUpdatedAt: now,
	}

	_ = s.store.UpsertEvent(r.Context(), localEvent)

	// Enqueue mutation
	eventJSON, _ := json.Marshal(localEvent)
	mutation := &models.MutationQueueItem{
		ID:             fmt.Sprintf("mut-%d", now.UnixNano()),
		Operation:      "create",
		Provider:       "google",
		CalendarID:     calendarID,
		LocalEventID:   eventID,
		Payload:        string(eventJSON),
		IdempotencyKey: fmt.Sprintf("idemp-cand-%s", cand.ID),
		AttemptCount:   0,
		NextAttemptAt:  now,
		State:          models.QueuePending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = s.store.EnqueueMutation(r.Context(), mutation)

	_ = s.store.UpdateCandidateState(r.Context(), cand.ID, models.CandidateApproved, "admin")
	s.recordAudit(r, "candidate_approve", "event_candidate", cand.ID, "Approved and scheduled candidate")

	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	http.Redirect(w, r, "/admin?tab=candidates&msg=Candidate+approved+and+scheduled", http.StatusSeeOther)
}

func (s *Server) handleAdminCandidateReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = s.store.UpdateCandidateState(r.Context(), id, models.CandidateRejected, "admin")
	s.recordAudit(r, "candidate_reject", "event_candidate", id, "Rejected candidate")
	http.Redirect(w, r, "/admin?tab=candidates&msg=Candidate+rejected", http.StatusSeeOther)
}

func (s *Server) handleAdminMailSourceUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sources, err := s.store.ListMailSources(r.Context())
	if err != nil {
		http.Redirect(w, r, "/admin?tab=mail&err="+url.QueryEscape("Could not load mail sources"), http.StatusSeeOther)
		return
	}

	var source *models.MailSource
	for i := range sources {
		if sources[i].ID == id {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		http.Redirect(w, r, "/admin?tab=mail&err=Mail+source+not+found", http.StatusSeeOther)
		return
	}

	filterMode := strings.TrimSpace(r.FormValue("filter_mode"))
	switch filterMode {
	case "label", "sender", "subject", "combined":
	default:
		http.Redirect(w, r, "/admin?tab=mail&err=Choose+a+valid+mail+filter+mode", http.StatusSeeOther)
		return
	}

	source.Enabled = r.FormValue("enabled") == "1" || r.FormValue("enabled") == "true" || r.FormValue("enabled") == "on"
	source.FilterMode = filterMode
	source.FilterConfig = strings.TrimSpace(r.FormValue("filter_config"))
	if source.Status == "" {
		source.Status = "active"
	}
	if err := s.store.UpsertMailSource(r.Context(), source); err != nil {
		http.Redirect(w, r, "/admin?tab=mail&err="+url.QueryEscape("Could not update mail source: "+err.Error()), http.StatusSeeOther)
		return
	}

	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "mail_source_update",
		ResourceType: "mail_source",
		ResourceID:   source.ID,
		Details:      fmt.Sprintf("Updated %s mail intake for person %s (enabled=%t, filter=%s)", source.Provider, source.PersonID, source.Enabled, source.FilterMode),
		IPAddress:    remoteIP(r),
	})
	http.Redirect(w, r, "/admin?tab=mail&msg=Mail+source+updated", http.StatusSeeOther)
}

func (s *Server) handleAdminConflictResolve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	resolution := strings.TrimSpace(r.FormValue("resolution"))
	validResolutions := map[string]string{
		"resolved_local":  "Local record retained after review",
		"resolved_remote": "Remote record accepted after review",
		"keep_both":       "Both local and remote records retained after review",
		"dismissed":       "Conflict dismissed after review",
	}
	details, ok := validResolutions[resolution]
	if id == "" || !ok {
		http.Redirect(w, r, "/admin?tab=conflicts&err=Choose+a+valid+conflict+resolution", http.StatusSeeOther)
		return
	}
	conflicts, err := s.store.ListPendingConflicts(r.Context())
	if err != nil {
		http.Redirect(w, r, "/admin?tab=conflicts&err=Could+not+load+the+conflict", http.StatusSeeOther)
		return
	}
	var conflict *models.Conflict
	for i := range conflicts {
		if conflicts[i].ID == id {
			conflict = &conflicts[i]
			break
		}
	}
	if conflict == nil {
		http.Redirect(w, r, "/admin?tab=conflicts&err=Conflict+not+found+or+already+resolved", http.StatusSeeOther)
		return
	}

	remoteResolved := false
	switch resolution {
	case "keep_both":
		localEvent, err := s.store.GetEvent(r.Context(), conflict.LocalEventID)
		if err != nil || localEvent == nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err=The+local+event+for+this+conflict+is+no+longer+available", http.StatusSeeOther)
			return
		}
		var remoteEvent models.Event
		if err := json.Unmarshal([]byte(conflict.RemotePayload), &remoteEvent); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err=The+remote+event+payload+is+invalid", http.StatusSeeOther)
			return
		}
		// Clone local event so family edit is not lost
		clonedLocal := *localEvent
		clonedLocal.ID = fmt.Sprintf("%s-local-%d", localEvent.ID, time.Now().Unix())
		clonedLocal.Title = localEvent.Title + " (Local Edit)"
		clonedLocal.LocalUpdatedAt = time.Now()
		if err := s.store.UpsertEvent(r.Context(), &clonedLocal); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err="+url.QueryEscape("Could not save local clone: "+err.Error()), http.StatusSeeOther)
			return
		}

		// Apply remote event to original event ID
		remoteEvent.ID = localEvent.ID
		remoteEvent.CalendarID = localEvent.CalendarID
		remoteEvent.LocalCreatedAt = localEvent.LocalCreatedAt
		remoteEvent.LocalUpdatedAt = time.Now()
		remoteEvent.Source = "remote_sync"
		if err := s.store.ApplyRemoteConflictResolution(r.Context(), id, &remoteEvent, details); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err="+url.QueryEscape("Could not apply remote conflict resolution: "+err.Error()), http.StatusSeeOther)
			return
		}
		remoteResolved = true
	case "resolved_remote":
		localEvent, err := s.store.GetEvent(r.Context(), conflict.LocalEventID)
		if err != nil || localEvent == nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err=The+local+event+for+this+conflict+is+no+longer+available", http.StatusSeeOther)
			return
		}
		var remoteEvent models.Event
		if err := json.Unmarshal([]byte(conflict.RemotePayload), &remoteEvent); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err=The+remote+event+payload+is+invalid", http.StatusSeeOther)
			return
		}
		remoteEvent.ID = localEvent.ID
		remoteEvent.CalendarID = localEvent.CalendarID
		remoteEvent.LocalCreatedAt = localEvent.LocalCreatedAt
		remoteEvent.LocalUpdatedAt = time.Now()
		remoteEvent.Source = "remote_sync"
		if err := s.store.ApplyRemoteConflictResolution(r.Context(), id, &remoteEvent, details); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err="+url.QueryEscape("Could not apply remote conflict resolution: "+err.Error()), http.StatusSeeOther)
			return
		}
		remoteResolved = true
	}

	if !remoteResolved {
		if err := s.store.ResolveConflict(r.Context(), id, resolution, details); err != nil {
			http.Redirect(w, r, "/admin?tab=conflicts&err="+url.QueryEscape("Could not resolve conflict: "+err.Error()), http.StatusSeeOther)
			return
		}
	}
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "conflict_resolve",
		ResourceType: "conflict",
		ResourceID:   id,
		Details:      details,
		IPAddress:    remoteIP(r),
	})

	if resolution == "resolved_local" {
		go func() {
			_ = s.queueEngine.ProcessBatch(context.Background(), 5)
			s.BroadcastReload()
		}()
	} else {
		s.BroadcastReload()
	}
	http.Redirect(w, r, "/admin?tab=conflicts&msg=Conflict+resolved", http.StatusSeeOther)
}

func (s *Server) handleAdminTrustedSenderAdd(w http.ResponseWriter, r *http.Request) {
	target := strings.ToLower(strings.TrimSpace(r.FormValue("sender")))
	description := strings.TrimSpace(r.FormValue("description"))
	if target == "" || strings.ContainsAny(target, " \t\n\r") {
		http.Redirect(w, r, "/admin?tab=senders&err=Enter+a+valid+email+address+or+domain", http.StatusSeeOther)
		return
	}

	trusted := &models.TrustedSender{
		ID:          fmt.Sprintf("sender-%d", time.Now().UnixNano()),
		Description: description,
		Enabled:     true,
	}
	if strings.Contains(target, "@") {
		parts := strings.Split(target, "@")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || !strings.Contains(parts[1], ".") {
			http.Redirect(w, r, "/admin?tab=senders&err=Enter+a+valid+email+address", http.StatusSeeOther)
			return
		}
		trusted.Email = target
	} else {
		trusted.Domain = strings.TrimPrefix(target, "@")
		if !strings.Contains(trusted.Domain, ".") {
			http.Redirect(w, r, "/admin?tab=senders&err=Enter+a+valid+domain", http.StatusSeeOther)
			return
		}
	}

	if err := s.store.AddTrustedSender(r.Context(), trusted); err != nil {
		http.Redirect(w, r, "/admin?tab=senders&err="+url.QueryEscape("Could not add trusted sender: "+err.Error()), http.StatusSeeOther)
		return
	}
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "trusted_sender_add",
		ResourceType: "trusted_sender",
		ResourceID:   trusted.ID,
		Details:      fmt.Sprintf("Added trusted sender %s%s", trusted.Email, trusted.Domain),
		IPAddress:    remoteIP(r),
	})
	http.Redirect(w, r, "/admin?tab=senders&msg=Trusted+sender+added", http.StatusSeeOther)
}

func (s *Server) handleAdminTrustedSenderDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Redirect(w, r, "/admin?tab=senders&err=Trusted+sender+ID+required", http.StatusSeeOther)
		return
	}
	if err := s.store.DeleteTrustedSender(r.Context(), id); err != nil {
		http.Redirect(w, r, "/admin?tab=senders&err="+url.QueryEscape("Could not remove trusted sender: "+err.Error()), http.StatusSeeOther)
		return
	}
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "trusted_sender_delete",
		ResourceType: "trusted_sender",
		ResourceID:   id,
		Details:      "Removed trusted sender",
		IPAddress:    remoteIP(r),
	})
	http.Redirect(w, r, "/admin?tab=senders&msg=Trusted+sender+removed", http.StatusSeeOther)
}

func (s *Server) handleAdminQueueRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Redirect(w, r, "/admin?tab=queue&err=Queue+item+ID+required", http.StatusSeeOther)
		return
	}
	retried, err := s.store.RetryDeadLetterMutation(r.Context(), id)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=queue&err="+url.QueryEscape("Could not retry queue item: "+err.Error()), http.StatusSeeOther)
		return
	}
	if !retried {
		http.Redirect(w, r, "/admin?tab=queue&err=Queue+item+is+not+in+dead-letter+state", http.StatusSeeOther)
		return
	}
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "mutation_retry",
		ResourceType: "mutation_queue",
		ResourceID:   id,
		Details:      "Reset dead-letter mutation for a full retry budget",
		IPAddress:    remoteIP(r),
	})
	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()
	http.Redirect(w, r, "/admin?tab=queue&msg=Queue+item+reset+and+retry+started", http.StatusSeeOther)
}

func (s *Server) handleAdminDatabaseVacuum(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slog.Info("Compacting and optimizing SQLite database")
	if _, err := s.store.DB().ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		slog.Error("Database checkpoint failed", "err", err)
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Database checkpoint failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := s.store.DB().ExecContext(ctx, "VACUUM;"); err != nil {
		slog.Error("Database vacuum failed", "err", err)
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Database optimization failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := s.store.DB().ExecContext(ctx, "PRAGMA optimize;"); err != nil {
		slog.Error("Database optimization failed", "err", err)
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Database optimization failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "database_vacuum", "database", "sqlite", "Compacted and optimized database")
	http.Redirect(w, r, "/admin?tab=system&msg=Database+compacted+and+optimized+successfully", http.StatusSeeOther)
}

func (s *Server) handleAdminStorageCleanup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slog.Info("Executing storage retention cleanup pass")
	report, err := maintenance.RunOnce(ctx, s.store)
	if err != nil {
		slog.Error("Storage retention cleanup failed", "err", err)
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Storage cleanup failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := s.store.DB().ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); err != nil {
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Storage checkpoint failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if _, err := s.store.DB().ExecContext(ctx, "VACUUM;"); err != nil {
		http.Redirect(w, r, "/admin?tab=system&err="+url.QueryEscape("Storage compaction failed: "+err.Error()), http.StatusSeeOther)
		return
	}

	s.recordAudit(r, "storage_cleanup", "storage", "*", fmt.Sprintf("Pruned expired data and compacted database; removed %d deleted events and %d old audit records", report.RemovedEvents, report.RemovedAudit))
	http.Redirect(w, r, fmt.Sprintf("/admin?tab=system&msg=Storage+cleanup+complete:+%d+events+and+%d+audit+records+removed", report.RemovedEvents, report.RemovedAudit), http.StatusSeeOther)
}
