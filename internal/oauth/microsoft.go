package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	ScopeMSOfflineAccess = "offline_access"
	ScopeMSUserRead      = "User.Read"
	ScopeMSMailRead      = "Mail.Read"
)

type MicrosoftUserProfile struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	UserPrincipalName string `json:"userPrincipalName"`
	Mail              string `json:"mail"`
}

type MicrosoftClient struct {
	clientID     string
	clientSecret string
	redirectURL  string
	authURL      string
	tokenURL     string
	profileURL   string
	httpClient   *http.Client
}

func NewMicrosoftClient(clientID, clientSecret, redirectURL string) *MicrosoftClient {
	return &MicrosoftClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		authURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		tokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		profileURL:   "https://graph.microsoft.com/v1.0/me",
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (mc *MicrosoftClient) SetEndpoints(authURL, tokenURL, profileURL string, client *http.Client) {
	if authURL != "" {
		mc.authURL = authURL
	}
	if tokenURL != "" {
		mc.tokenURL = tokenURL
	}
	if profileURL != "" {
		mc.profileURL = profileURL
	}
	if client != nil {
		mc.httpClient = client
	}
}

func (mc *MicrosoftClient) AuthURL(state string) string {
	conf := &oauth2.Config{
		ClientID:     mc.clientID,
		ClientSecret: mc.clientSecret,
		RedirectURL:  mc.redirectURL,
		Scopes:       []string{ScopeMSOfflineAccess, ScopeMSUserRead, ScopeMSMailRead},
		Endpoint: oauth2.Endpoint{
			AuthURL:  mc.authURL,
			TokenURL: mc.tokenURL,
		},
	}

	return conf.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (mc *MicrosoftClient) Exchange(ctx context.Context, code string) (*StoredToken, *MicrosoftUserProfile, error) {
	conf := &oauth2.Config{
		ClientID:     mc.clientID,
		ClientSecret: mc.clientSecret,
		RedirectURL:  mc.redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  mc.authURL,
			TokenURL: mc.tokenURL,
		},
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, mc.httpClient)
	token, err := conf.Exchange(ctx, code)
	if err != nil {
		return nil, nil, fmt.Errorf("exchange ms code: %w", err)
	}

	profile, err := mc.GetProfile(ctx, token.AccessToken)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch ms user profile: %w", err)
	}

	email := profile.Mail
	if email == "" {
		email = profile.UserPrincipalName
	}

	stored := &StoredToken{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		Email:        email,
		UpdatedAt:    time.Now(),
	}

	return stored, profile, nil
}

func (mc *MicrosoftClient) GetProfile(ctx context.Context, accessToken string) (*MicrosoftUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mc.profileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute profile request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("profile request returned status %d: %s", resp.StatusCode, string(body))
	}

	var p MicrosoftUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, fmt.Errorf("decode profile response: %w", err)
	}

	return &p, nil
}

func (mc *MicrosoftClient) RefreshToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("cannot refresh without refresh token")
	}

	form := url.Values{
		"client_id":     {mc.clientID},
		"client_secret": {mc.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
		"scope":         {strings.Join([]string{ScopeMSOfflineAccess, ScopeMSUserRead, ScopeMSMailRead}, " ")},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mc.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusBadRequest {
		return nil, ErrAccountRevoked
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("token refresh returned status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	tok := &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: tokenResp.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}

	return tok, nil
}
