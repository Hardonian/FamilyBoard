package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/auth"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
)

const sessionCookieName = "fb_session"

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := s.isAuthenticatedAdmin(r)
		if !ok || sess == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			})
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
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

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	csrfToken, _ := auth.GenerateSecureToken(16)
	s.render(w, "login.html", map[string]interface{}{
		"Title":     "Admin Login",
		"Theme":     "dark",
		"CSRFToken": csrfToken,
		"Username":  s.cfg.Auth.AdminUsername,
	})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr
	if !s.rateLimiter.Allow(ip) {
		s.render(w, "login.html", map[string]interface{}{
			"Title": "Admin Login",
			"Theme": "dark",
			"Error": "Too many failed attempts. Please try again later.",
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
			"Title":    "Admin Login",
			"Theme":    "dark",
			"Username": username,
			"Error":    "Invalid username or password",
		})
		return
	}

	s.rateLimiter.Reset(ip)

	// Create session
	token, _ := auth.GenerateSecureToken(32)
	tokenHash := auth.HashToken(token)
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	_ = s.store.CreateSession(r.Context(), &models.Session{
		TokenHash:    tokenHash,
		UserID:       username,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		LastActiveAt: now,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
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

	next := r.URL.Query().Get("next")
	if next == "" || !strings.HasPrefix(next, "/") {
		next = "/admin"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		tokenHash := auth.HashToken(cookie.Value)
		_ = s.store.DeleteSession(r.Context(), tokenHash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupGet(w http.ResponseWriter, r *http.Request) {
	meta, err := s.store.GetDeviceMetadata(r.Context())
	if err == nil && meta != nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	csrfToken, _ := auth.GenerateSecureToken(16)
	s.render(w, "setup.html", map[string]interface{}{
		"Title":          "First-Run Setup",
		"Theme":          "dark",
		"CSRFToken":      csrfToken,
		"DeviceName":     s.cfg.Server.DeviceName,
		"Timezone":       s.cfg.Server.Timezone,
		"GoogleClientID": s.cfg.Google.ClientID,
	})
}

func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	deviceName := strings.TrimSpace(r.FormValue("device_name"))
	tz := strings.TrimSpace(r.FormValue("timezone"))
	password := r.FormValue("admin_password")
	passwordConfirm := r.FormValue("admin_password_confirm")
	googleClientID := strings.TrimSpace(r.FormValue("google_client_id"))
	googleSecret := strings.TrimSpace(r.FormValue("google_client_secret"))

	if password != passwordConfirm {
		s.render(w, "setup.html", map[string]interface{}{
			"Title": "First-Run Setup", "Theme": "dark", "Error": "Passwords do not match",
			"DeviceName": deviceName, "Timezone": tz, "GoogleClientID": googleClientID,
		})
		return
	}

	if len(password) < 8 {
		s.render(w, "setup.html", map[string]interface{}{
			"Title": "First-Run Setup", "Theme": "dark", "Error": "Password must be at least 8 characters",
			"DeviceName": deviceName, "Timezone": tz, "GoogleClientID": googleClientID,
		})
		return
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		http.Error(w, "hash password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update configuration in memory and save
	s.cfg.Server.DeviceName = deviceName
	s.cfg.Server.Timezone = tz
	s.cfg.Auth.AdminPasswordHash = hashedPassword
	if googleClientID != "" {
		s.cfg.Google.ClientID = googleClientID
	}
	if googleSecret != "" {
		s.cfg.Google.ClientSecret = googleSecret
	}

	_ = s.cfg.Save("config.toml")

	ctx := r.Context()
	now := time.Now()

	// Mark device initialized in DB
	_ = s.store.SetDeviceMetadata(ctx, &models.DeviceMetadata{
		DeviceName:    deviceName,
		Timezone:      tz,
		InitializedAt: now,
		Version:       "v1.0.0",
		KioskSettings: "{}",
	})

	// Process dynamic family members and configure calendar sync targets
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
		_ = s.store.UpsertPerson(ctx, &models.Person{
			ID:          "person-1",
			DisplayName: member1Name,
			Email:       member1Email,
			Enabled:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		prov1 := DetectEmailProvider(member1Email)
		_ = s.store.UpsertCalendar(ctx, &models.Calendar{
			ID:                 "cal-member-1",
			Provider:           prov1,
			ProviderCalendarID: member1Email,
			Name:               member1Name,
			DisplayColor:       "#38bdf8",
			EnabledForDisplay:  true,
			Writable:           true,
			DefaultWriteTarget: true,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	if member2Email != "" {
		_ = s.store.UpsertPerson(ctx, &models.Person{
			ID:          "person-2",
			DisplayName: member2Name,
			Email:       member2Email,
			Enabled:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		prov2 := DetectEmailProvider(member2Email)
		_ = s.store.UpsertCalendar(ctx, &models.Calendar{
			ID:                 "cal-member-2",
			Provider:           prov2,
			ProviderCalendarID: member2Email,
			Name:               member2Name,
			DisplayColor:       "#fb7185",
			EnabledForDisplay:  true,
			Writable:           true,
			DefaultWriteTarget: false,
			CreatedAt:          now,
			UpdatedAt:          now,
		})
	}

	// Create initial admin session
	token, _ := auth.GenerateSecureToken(32)
	tokenHash := auth.HashToken(token)
	expiresAt := now.Add(24 * time.Hour)

	_ = s.store.CreateSession(ctx, &models.Session{
		TokenHash:    tokenHash,
		UserID:       s.cfg.Auth.AdminUsername,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
		LastActiveAt: now,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.Security.CookieSecure,
	})

	http.Redirect(w, r, "/admin?tab=accounts", http.StatusSeeOther)
}

// DetectEmailProvider inspects an email domain and returns "microsoft" for Outlook/Hotmail/Live, or "google" for others.
func DetectEmailProvider(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if strings.HasSuffix(email, "@outlook.com") || strings.HasSuffix(email, "@hotmail.com") ||
		strings.HasSuffix(email, "@live.com") || strings.HasSuffix(email, "@msn.com") ||
		strings.HasSuffix(email, "@office365.com") {
		return "microsoft"
	}
	return "google"
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	if tab == "" {
		tab = "accounts"
	}

	ctx := r.Context()
	people, _ := s.store.ListPeople(ctx)
	accounts, _ := s.store.ListOAuthAccounts(ctx)
	accountsMap := make(map[string]models.OAuthAccount)
	for _, a := range accounts {
		accountsMap[a.PersonID] = a
	}

	providerMap := make(map[string]string)
	for _, p := range people {
		providerMap[p.ID] = DetectEmailProvider(p.Email)
	}

	calendars, _ := s.store.ListCalendars(ctx)
	candidates, _ := s.store.ListCandidates(ctx, models.CandidatePending, 50, 0)
	queueItems, _ := s.store.ListAllMutations(ctx, 50, 0)
	pendingCandidateCount, _ := s.store.CountPendingCandidates(ctx)
	pendingQueueCount, _ := s.store.CountPendingMutations(ctx)
	activeEventCount, _ := s.store.CountActiveEvents(ctx)
	conflicts, _ := s.store.ListPendingConflicts(ctx)

	csrfToken, _ := auth.GenerateSecureToken(16)

	data := map[string]interface{}{
		"Title":                 "Admin Dashboard",
		"Theme":                 "dark",
		"ActiveTab":             tab,
		"CSRFToken":             csrfToken,
		"People":                people,
		"AccountsMap":           accountsMap,
		"ProviderMap":           providerMap,
		"Calendars":             calendars,
		"Candidates":            candidates,
		"QueueItems":            queueItems,
		"PendingCandidateCount": pendingCandidateCount,
		"PendingQueueCount":     pendingQueueCount,
		"PendingConflictCount":  len(conflicts),
		"ActiveEventCount":      activeEventCount,
		"Message":               r.URL.Query().Get("msg"),
		"Error":                 r.URL.Query().Get("err"),
	}

	s.render(w, "admin.html", data)
}

func (s *Server) handleAdminSyncNow(w http.ResponseWriter, r *http.Request) {
	go func() {
		_ = s.syncEngine.SyncAll(context.Background())
		s.BroadcastReload()
	}()

	http.Redirect(w, r, "/admin?tab=calendars&msg=Sync+initiated+in+background", http.StatusSeeOther)
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

	go func() {
		_ = s.queueEngine.ProcessBatch(context.Background(), 5)
		s.BroadcastReload()
	}()

	http.Redirect(w, r, "/admin?tab=candidates&msg=Candidate+approved+and+scheduled", http.StatusSeeOther)
}

func (s *Server) handleAdminCandidateReject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = s.store.UpdateCandidateState(r.Context(), id, models.CandidateRejected, "admin")
	http.Redirect(w, r, "/admin?tab=candidates&msg=Candidate+rejected", http.StatusSeeOther)
}

func (s *Server) handleAdminOAuthStart(w http.ResponseWriter, r *http.Request) {
	personID := r.URL.Query().Get("person_id")
	provider := r.URL.Query().Get("provider")
	flow := r.URL.Query().Get("flow")
	if flow == "" {
		flow = "calendar"
	}

	state, err := s.stateManager.GenerateState(personID, provider, flow, 10*time.Minute)
	if err != nil {
		http.Error(w, "generate oauth state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if provider == "google" && s.googleOAuth != nil {
		// Retrieve person email for login_hint
		people, _ := s.store.ListPeople(r.Context())
		loginHint := ""
		for _, p := range people {
			if p.ID == personID {
				loginHint = p.Email
				break
			}
		}
		authURL := s.googleOAuth.AuthURL(state, flow, loginHint)
		http.Redirect(w, r, authURL, http.StatusSeeOther)
		return
	}

	if provider == "microsoft" && s.msOAuth != nil {
		authURL := s.msOAuth.AuthURL(state)
		http.Redirect(w, r, authURL, http.StatusSeeOther)
		return
	}

	http.Error(w, "provider not configured", http.StatusBadRequest)
}

func (s *Server) handleOAuthCallbackGoogle(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	parsedState, err := s.stateManager.VerifyState(stateStr)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+state", http.StatusSeeOther)
		return
	}

	token, userInfo, err := s.googleOAuth.Exchange(r.Context(), code)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err="+err.Error(), http.StatusSeeOther)
		return
	}

	// CRITICAL REQUIREMENT: Strict Account Isolation Check!
	// Retrieve expected email for the personID
	people, _ := s.store.ListPeople(r.Context())
	var expectedEmail string
	for _, p := range people {
		if p.ID == parsedState.PersonID {
			expectedEmail = p.Email
			break
		}
	}

	if err := oauth.VerifyAccountIsolation(expectedEmail, userInfo.Email); err != nil {
		slog.Error("OAuth cross-assignment attempted and prevented", "expected", expectedEmail, "authenticated", userInfo.Email)
		http.Redirect(w, r, fmt.Sprintf("/admin?tab=accounts&err=Account+mismatch:+Authenticated+as+%s+but+expected+%s.+Cross-assignment+prevented.", userInfo.Email, expectedEmail), http.StatusSeeOther)
		return
	}

	accountID := fmt.Sprintf("acc-g-%s", userInfo.Sub)

	// Save token securely in 0600 storage
	_ = s.tokenStorage.Save(accountID, token)

	scopes := oauth.ScopeGoogleCalendar
	if parsedState.Flow == "mail" {
		scopes = oauth.ScopeGoogleGmail
	}

	oauthAccount := &models.OAuthAccount{
		ID:                  accountID,
		PersonID:            parsedState.PersonID,
		Provider:            "google",
		ProviderAccountID:   userInfo.Sub,
		VerifiedEmail:       userInfo.Email,
		Scopes:              scopes,
		CredentialReference: accountID,
		ExpiresAt:           token.Expiry,
		Status:              "active",
	}

	_ = s.store.UpsertOAuthAccount(r.Context(), oauthAccount)

	// Register primary calendar if flow was calendar
	if parsedState.Flow == "calendar" {
		name := userInfo.Name
		if name == "" {
			name = userInfo.Email
		}
		color := "#3b82f6"
		if person, err := s.store.GetPerson(r.Context(), parsedState.PersonID); err == nil && person != nil && person.Color != "" {
			color = person.Color
		} else if parsedState.PersonID == "person-2" {
			color = "#f472b6"
		}

		primaryCal := &models.Calendar{
			ID:                 fmt.Sprintf("cal-g-%s", userInfo.Sub),
			OAuthAccountID:     accountID,
			Provider:           "google",
			ProviderCalendarID: userInfo.Email,
			Name:               name,
			Timezone:           s.cfg.Server.Timezone,
			DisplayColor:       color,
			EnabledForDisplay:  true,
			Writable:           true,
			DefaultWriteTarget: true,
		}
		_ = s.store.UpsertCalendar(r.Context(), primaryCal)

		go func() {
			_ = s.syncEngine.SyncAll(context.Background())
			s.BroadcastReload()
		}()
	}

	// Enable mail source if flow was mail
	if parsedState.Flow == "mail" {
		_ = s.store.UpsertMailSource(r.Context(), &models.MailSource{
			ID:                fmt.Sprintf("ms-g-%s", userInfo.Sub),
			PersonID:          parsedState.PersonID,
			Provider:          "gmail",
			ProviderAccountID: userInfo.Sub,
			Enabled:           true,
			FilterMode:        "label",
			FilterConfig:      s.cfg.Mail.GmailLabel,
			Status:            "active",
		})
	}

	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "oauth_connect",
		ResourceType: "account",
		ResourceID:   accountID,
		Details:      fmt.Sprintf("Successfully connected Google identity %s for %s (%s)", userInfo.Email, parsedState.PersonID, parsedState.Flow),
		IPAddress:    r.RemoteAddr,
	})

	http.Redirect(w, r, "/admin?tab=accounts&msg=Successfully+connected+Google+account", http.StatusSeeOther)
}

func (s *Server) handleOAuthCallbackMicrosoft(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	parsedState, err := s.stateManager.VerifyState(stateStr)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+state", http.StatusSeeOther)
		return
	}

	token, profile, err := s.msOAuth.Exchange(r.Context(), code)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err="+err.Error(), http.StatusSeeOther)
		return
	}

	accountID := fmt.Sprintf("acc-ms-%s", profile.ID)
	_ = s.tokenStorage.Save(accountID, token)

	oauthAccount := &models.OAuthAccount{
		ID:                  accountID,
		PersonID:            parsedState.PersonID,
		Provider:            "microsoft",
		ProviderAccountID:   profile.ID,
		VerifiedEmail:       token.Email,
		Scopes:              "Mail.Read",
		CredentialReference: accountID,
		ExpiresAt:           token.Expiry,
		Status:              "active",
	}

	_ = s.store.UpsertOAuthAccount(r.Context(), oauthAccount)

	http.Redirect(w, r, "/admin?tab=mail&msg=Successfully+connected+Microsoft+Outlook", http.StatusSeeOther)
}

func (s *Server) handleAdminBackupDownload(w http.ResponseWriter, r *http.Request) {
	tmpDir, err := os.MkdirTemp("", "fb-backup-*")
	if err != nil {
		http.Error(w, "create temp backup dir: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tmpDir)

	backupFile := filepath.Join(tmpDir, "backup.db")
	escapedPath := strings.ReplaceAll(backupFile, "'", "''")

	// Use SQLite VACUUM INTO for clean, transactionally consistent online snapshot
	_, err = s.store.DB().ExecContext(r.Context(), fmt.Sprintf("VACUUM INTO '%s';", escapedPath))
	if err != nil {
		http.Error(w, "vacuum into backup: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=familyboard-backup-%s.db", time.Now().Format("20060102-150405")))
	http.ServeFile(w, r, backupFile)
}
