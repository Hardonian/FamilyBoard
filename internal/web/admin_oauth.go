package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/calendar/apple"
	"github.com/Hardonian/FamilyBoard/internal/models"
	"github.com/Hardonian/FamilyBoard/internal/oauth"
)

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
		people, _ := s.store.ListPeople(r.Context())
		loginHint := ""
		for _, p := range people {
			if p.ID == personID {
				loginHint = p.Email
				break
			}
		}
		authURL := s.msOAuth.AuthURLWithFlow(state, flow, loginHint)
		http.Redirect(w, r, authURL, http.StatusSeeOther)
		return
	}

	http.Error(w, "provider not configured", http.StatusBadRequest)
}

func (s *Server) handleOAuthCallbackGoogle(w http.ResponseWriter, r *http.Request) {
	if s.googleOAuth == nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Google+OAuth+is+not+configured", http.StatusSeeOther)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		if errParam == "admin_policy_enforced" {
			http.Redirect(w, r, "/admin?tab=accounts&err=Google+Workspace+admin+policy+blocked+access.+Please+ask+your+Google+Workspace+administrator+to+trust+this+app+in+admin.google.com+(Security+->+API+Controls).", http.StatusSeeOther)
			return
		}
		if errParam == "access_denied" {
			http.Redirect(w, r, "/admin?tab=accounts&err=Access+was+denied+or+cancelled+during+Google+sign-in.", http.StatusSeeOther)
			return
		}
		errMsg := url.QueryEscape(fmt.Sprintf("Google sign-in error (%s)", errParam))
		http.Redirect(w, r, "/admin?tab=accounts&err="+errMsg, http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	parsedState, err := s.stateManager.VerifyState(stateStr)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+state", http.StatusSeeOther)
		return
	}

	token, userInfo, err := s.googleOAuth.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("Google OAuth exchange failed", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Google+authentication+failed", http.StatusSeeOther)
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
		if strings.HasSuffix(strings.ToLower(expectedEmail), "@example.com") {
			// Gracefully adopt verified Google Workspace or Gmail email from setup placeholder
			for _, p := range people {
				if p.ID == parsedState.PersonID {
					p.Email = userInfo.Email
					if strings.Contains(p.DisplayName, "Member") || p.DisplayName == "Alex" || p.DisplayName == "Jordan" {
						if userInfo.Name != "" {
							p.DisplayName = userInfo.Name
						}
					}
					_ = s.store.UpsertPerson(r.Context(), &p)
					break
				}
			}
		} else {
			slog.Error("OAuth cross-assignment attempted and prevented", "expected", expectedEmail, "authenticated", userInfo.Email)
			message := fmt.Sprintf("Account mismatch: authenticated as %s but expected %s. Cross-assignment prevented.", userInfo.Email, expectedEmail)
			http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(message), http.StatusSeeOther)
			return
		}
	}

	accountID := fmt.Sprintf("acc-g-%s", userInfo.Sub)

	// Save token securely in 0600 storage
	if err := s.tokenStorage.Save(accountID, token); err != nil {
		slog.Error("Failed to store Google OAuth token", "account_id", accountID, "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+securely+store+Google+credentials", http.StatusSeeOther)
		return
	}

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

	if err := s.store.UpsertOAuthAccount(r.Context(), oauthAccount); err != nil {
		slog.Error("Failed to store Google OAuth account", "account_id", accountID, "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+save+Google+account", http.StatusSeeOther)
		return
	}

	// Discover and register all calendars if flow was calendar
	if parsedState.Flow == "calendar" {
		name := userInfo.Name
		if name == "" {
			name = userInfo.Email
		}
		color := "#3b82f6"
		if parsedState.PersonID == "person-2" {
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
			bgCtx := context.Background()
			_ = s.syncEngine.DiscoverCalendars(bgCtx)
			_ = s.syncEngine.SyncAll(bgCtx)
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
	if s.msOAuth == nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Microsoft+OAuth+is+not+configured", http.StatusSeeOther)
		return
	}
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		if errParam == "access_denied" {
			http.Redirect(w, r, "/admin?tab=accounts&err=Access+was+denied+or+cancelled+during+Microsoft+sign-in.", http.StatusSeeOther)
			return
		}
		errMsg := url.QueryEscape(fmt.Sprintf("Microsoft sign-in error (%s)", errParam))
		http.Redirect(w, r, "/admin?tab=accounts&err="+errMsg, http.StatusSeeOther)
		return
	}
	code := r.URL.Query().Get("code")
	stateStr := r.URL.Query().Get("state")

	parsedState, err := s.stateManager.VerifyState(stateStr)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+state", http.StatusSeeOther)
		return
	}

	token, profile, err := s.msOAuth.Exchange(r.Context(), code)
	if err != nil {
		slog.Error("Microsoft OAuth exchange failed", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Microsoft+authentication+failed", http.StatusSeeOther)
		return
	}

	accountID := fmt.Sprintf("acc-ms-%s", profile.ID)
	if err := s.tokenStorage.Save(accountID, token); err != nil {
		slog.Error("Failed to store Microsoft OAuth token", "account_id", accountID, "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+securely+store+Microsoft+credentials", http.StatusSeeOther)
		return
	}

	oauthAccount := &models.OAuthAccount{
		ID:                  accountID,
		PersonID:            parsedState.PersonID,
		Provider:            "microsoft",
		ProviderAccountID:   profile.ID,
		VerifiedEmail:       token.Email,
		Scopes:              "User.Read Calendars.ReadWrite Mail.Read",
		CredentialReference: accountID,
		ExpiresAt:           token.Expiry,
		Status:              "active",
	}

	if err := s.store.UpsertOAuthAccount(r.Context(), oauthAccount); err != nil {
		slog.Error("Failed to store Microsoft OAuth account", "account_id", accountID, "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+save+Microsoft+account", http.StatusSeeOther)
		return
	}

	go func() {
		bgCtx := context.Background()
		_ = s.syncEngine.DiscoverCalendars(bgCtx)
		_ = s.syncEngine.SyncAll(bgCtx)
		s.BroadcastReload()
	}()

	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "oauth_connect",
		ResourceType: "account",
		ResourceID:   accountID,
		Details:      fmt.Sprintf("Successfully connected Microsoft Exchange identity %s for %s (%s)", token.Email, parsedState.PersonID, parsedState.Flow),
		IPAddress:    r.RemoteAddr,
	})

	redirectTab := "accounts"
	msg := "Successfully+connected+Microsoft+Exchange+account"
	if parsedState.Flow == "mail" {
		redirectTab = "mail"
		msg = "Successfully+connected+Microsoft+Outlook"
	}
	http.Redirect(w, r, fmt.Sprintf("/admin?tab=%s&msg=%s", redirectTab, msg), http.StatusSeeOther)
}

func (s *Server) handleAdminAppleConnect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+form+submission", http.StatusSeeOther)
		return
	}
	personID := r.FormValue("person_id")
	authType := r.FormValue("auth_type")
	appleID := strings.TrimSpace(r.FormValue("apple_id"))
	appPassword := strings.TrimSpace(r.FormValue("app_password"))
	webcalURL := strings.TrimSpace(r.FormValue("webcal_url"))

	if personID == "" {
		http.Redirect(w, r, "/admin?tab=accounts&err=Family+member+is+required", http.StatusSeeOther)
		return
	}

	people, _ := s.store.ListPeople(r.Context())
	var person *models.Person
	for _, p := range people {
		if p.ID == personID {
			person = &p
			break
		}
	}
	if person == nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Family+member+not+found", http.StatusSeeOther)
		return
	}

	var credential string
	var verifiedEmail string

	if authType == "webcal" || (webcalURL != "" && appleID == "") {
		if webcalURL == "" {
			http.Redirect(w, r, "/admin?tab=accounts&err=Apple+Calendar+URL+is+required", http.StatusSeeOther)
			return
		}
		credential = webcalURL
		verifiedEmail = person.Email
	} else {
		if appleID == "" || appPassword == "" {
			http.Redirect(w, r, "/admin?tab=accounts&err=Apple+ID+and+App-Specific+Password+are+required", http.StatusSeeOther)
			return
		}
		credential = fmt.Sprintf("%s:%s", appleID, appPassword)
		verifiedEmail = appleID
	}

	// Preflight validation of Apple calendar feed or CalDAV credentials
	appleClient := apple.NewAppleCalendarClient("", nil)
	if _, err := appleClient.ListCalendars(r.Context(), credential); err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "invalid apple credentials") {
			slog.Warn("Apple calendar authentication rejected", "err", err)
			errMsg := "Could not connect to Apple: Incorrect Apple ID or App-Specific Password. Generate one at appleid.apple.com."
			http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(errMsg), http.StatusSeeOther)
			return
		}
		slog.Info("Apple calendar preflight noted non-auth condition (e.g. offline/transient network)", "err", err)
	}

	accountID := fmt.Sprintf("acc-apple-%s", personID)
	token := &oauth.StoredToken{
		AccessToken: credential,
		Email:       verifiedEmail,
		UpdatedAt:   time.Now(),
	}

	if err := s.tokenStorage.Save(accountID, token); err != nil {
		slog.Error("Failed to store Apple credentials", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+securely+store+Apple+credentials", http.StatusSeeOther)
		return
	}

	account := &models.OAuthAccount{
		ID:                  accountID,
		PersonID:            personID,
		Provider:            "apple",
		ProviderAccountID:   verifiedEmail,
		VerifiedEmail:       verifiedEmail,
		Scopes:              "caldav",
		CredentialReference: accountID,
		Status:              "active",
	}

	if err := s.store.UpsertOAuthAccount(r.Context(), account); err != nil {
		slog.Error("Failed to store Apple account", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+save+Apple+account", http.StatusSeeOther)
		return
	}

	go func() {
		bgCtx := context.Background()
		_ = s.syncEngine.DiscoverCalendars(bgCtx)
		_ = s.syncEngine.SyncAll(bgCtx)
		s.BroadcastReload()
	}()

	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID:           fmt.Sprintf("aud-%d", time.Now().UnixNano()),
		Actor:        "admin",
		Action:       "apple_connect",
		ResourceType: "account",
		ResourceID:   accountID,
		Details:      fmt.Sprintf("Successfully connected Apple Calendar for %s (%s)", person.DisplayName, verifiedEmail),
		IPAddress:    r.RemoteAddr,
	})

	http.Redirect(w, r, "/admin?tab=accounts&msg=Successfully+connected+Apple+Calendar", http.StatusSeeOther)
}
