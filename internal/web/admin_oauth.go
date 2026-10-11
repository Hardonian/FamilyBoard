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
		people, _ := s.store.ListPeople(r.Context())
		loginHint := ""
		for _, p := range people {
			if p.ID == personID {
				loginHint = p.Email
				break
			}
		}
		authURL := s.googleOAuth.AuthURL(state, flow, loginHint)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
		return
	}

	if provider == "microsoft" && s.msOAuth != nil {
		authURL := s.msOAuth.AuthURL(state, flow)
		http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
		return
	}

	http.Error(w, "OAuth provider not configured", http.StatusBadRequest)
}

func (s *Server) handleOAuthCallbackGoogle(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		slog.Warn("Google OAuth returned error", "error", errParam)
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape("Google OAuth error: "+errParam), http.StatusSeeOther)
		return
	}

	parsedState, err := s.stateManager.ValidateState(state)
	if err != nil {
		slog.Warn("Invalid OAuth state parameter", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+session.+Please+try+again.", http.StatusSeeOther)
		return
	}

	if s.googleOAuth == nil {
		http.Error(w, "Google OAuth not configured", http.StatusInternalServerError)
		return
	}

	token, userInfo, err := s.googleOAuth.ExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("Failed to exchange Google OAuth code", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape("Failed to complete Google authentication: "+err.Error()), http.StatusSeeOther)
		return
	}

	if parsedState.PersonID != "" {
		people, _ := s.store.ListPeople(r.Context())
		var targetPerson *models.Person
		for _, p := range people {
			if p.ID == parsedState.PersonID {
				targetPerson = &p
				break
			}
		}

		expectedEmail := ""
		if targetPerson != nil {
			expectedEmail = targetPerson.Email
		}

		if strings.EqualFold(userInfo.Email, expectedEmail) {
			slog.Info("OAuth account verified against person email", "email", userInfo.Email, "person_id", parsedState.PersonID)
		} else if strings.HasSuffix(expectedEmail, "@example.com") {
			slog.Info("Updating seeded placeholder account email to real Google identity", "old", expectedEmail, "new", userInfo.Email, "person_id", parsedState.PersonID)
			if targetPerson != nil {
				targetPerson.Email = userInfo.Email
				if targetPerson.DisplayName == "Alex" || targetPerson.DisplayName == "Jordan" {
					if userInfo.Name != "" {
						targetPerson.DisplayName = userInfo.Name
					}
				}
				if err := s.store.UpsertPerson(r.Context(), targetPerson); err != nil {
					slog.Error("Failed to update placeholder person identity", "err", err)
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
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errParam := r.URL.Query().Get("error")

	if errParam != "" {
		slog.Warn("Microsoft OAuth returned error", "error", errParam)
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape("Microsoft OAuth error: "+errParam), http.StatusSeeOther)
		return
	}

	parsedState, err := s.stateManager.ValidateState(state)
	if err != nil {
		slog.Warn("Invalid OAuth state parameter", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Invalid+or+expired+OAuth+session.+Please+try+again.", http.StatusSeeOther)
		return
	}

	if s.msOAuth == nil {
		http.Error(w, "Microsoft OAuth not configured", http.StatusInternalServerError)
		return
	}

	token, err := s.msOAuth.ExchangeCode(r.Context(), code)
	if err != nil {
		slog.Error("Failed to exchange Microsoft OAuth code", "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape("Failed to complete Microsoft authentication: "+err.Error()), http.StatusSeeOther)
		return
	}

	if parsedState.PersonID != "" {
		people, _ := s.store.ListPeople(r.Context())
		var targetPerson *models.Person
		for _, p := range people {
			if p.ID == parsedState.PersonID {
				targetPerson = &p
				break
			}
		}

		expectedEmail := ""
		if targetPerson != nil {
			expectedEmail = targetPerson.Email
		}

		if strings.EqualFold(token.Email, expectedEmail) {
			slog.Info("OAuth account verified against person email", "email", token.Email, "person_id", parsedState.PersonID)
		} else if strings.HasSuffix(expectedEmail, "@example.com") {
			slog.Info("Updating seeded placeholder account email to real Microsoft identity", "old", expectedEmail, "new", token.Email, "person_id", parsedState.PersonID)
			if targetPerson != nil {
				targetPerson.Email = token.Email
				if err := s.store.UpsertPerson(r.Context(), targetPerson); err != nil {
					slog.Error("Failed to update placeholder person identity", "err", err)
				}
			}
		} else {
			slog.Error("OAuth cross-assignment attempted and prevented", "expected", expectedEmail, "authenticated", token.Email)
			message := fmt.Sprintf("Account mismatch: authenticated as %s but expected %s. Cross-assignment prevented.", token.Email, expectedEmail)
			http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(message), http.StatusSeeOther)
			return
		}
	}

	accountID := fmt.Sprintf("acc-ms-%s", token.Email)

	if err := s.tokenStorage.Save(accountID, token); err != nil {
		slog.Error("Failed to store Microsoft OAuth token", "account_id", accountID, "err", err)
		http.Redirect(w, r, "/admin?tab=accounts&err=Could+not+securely+store+Microsoft+credentials", http.StatusSeeOther)
		return
	}

	oauthAccount := &models.OAuthAccount{
		ID:                  accountID,
		PersonID:            parsedState.PersonID,
		Provider:            "microsoft",
		ProviderAccountID:   token.Email,
		VerifiedEmail:       token.Email,
		Scopes:              "Calendars.ReadWrite",
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
