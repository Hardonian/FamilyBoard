package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	ScopeGoogleEmail    = "https://www.googleapis.com/auth/userinfo.email"
	ScopeGoogleOpenID   = "openid"
	ScopeGoogleCalendar = "https://www.googleapis.com/auth/calendar"
	ScopeGoogleGmail    = "https://www.googleapis.com/auth/gmail.readonly"
)

type GoogleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

type GoogleClient struct {
	clientID     string
	clientSecret string
	redirectURL  string
	authURL      string
	tokenURL     string
	userInfoURL  string
	httpClient   *http.Client
}

func NewGoogleClient(clientID, clientSecret, redirectURL string) *GoogleClient {
	return &GoogleClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		redirectURL:  redirectURL,
		authURL:      google.Endpoint.AuthURL,
		tokenURL:     google.Endpoint.TokenURL,
		userInfoURL:  "https://www.googleapis.com/oauth2/v3/userinfo",
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetEndpoints allows overriding endpoints for mock integration tests.
func (gc *GoogleClient) SetEndpoints(authURL, tokenURL, userInfoURL string, client *http.Client) {
	if authURL != "" {
		gc.authURL = authURL
	}
	if tokenURL != "" {
		gc.tokenURL = tokenURL
	}
	if userInfoURL != "" {
		gc.userInfoURL = userInfoURL
	}
	if client != nil {
		gc.httpClient = client
	}
}

// AuthURL generates the Google OAuth URL with distinct scopes depending on flow.
// Connecting Google Calendar must NOT automatically request Gmail access.
func (gc *GoogleClient) AuthURL(state, flow, loginHint string) string {
	scopes := []string{ScopeGoogleOpenID, ScopeGoogleEmail}
	if flow == "mail" {
		scopes = append(scopes, ScopeGoogleGmail)
	} else {
		// Calendar flow (default)
		scopes = append(scopes, ScopeGoogleCalendar)
	}

	endpoint := oauth2.Endpoint{
		AuthURL:  gc.authURL,
		TokenURL: gc.tokenURL,
	}

	conf := &oauth2.Config{
		ClientID:     gc.clientID,
		ClientSecret: gc.clientSecret,
		RedirectURL:  gc.redirectURL,
		Scopes:       scopes,
		Endpoint:     endpoint,
	}

	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce, // prompt=consent to ensure refresh_token returned
	}
	if loginHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint))
	}

	return conf.AuthCodeURL(state, opts...)
}

// Exchange exchanges the auth code for tokens and retrieves the verified user profile.
func (gc *GoogleClient) Exchange(ctx context.Context, code string) (*StoredToken, *GoogleUserInfo, error) {
	conf := &oauth2.Config{
		ClientID:     gc.clientID,
		ClientSecret: gc.clientSecret,
		RedirectURL:  gc.redirectURL,
		Endpoint: oauth2.Endpoint{
			AuthURL:  gc.authURL,
			TokenURL: gc.tokenURL,
		},
	}

	ctx = context.WithValue(ctx, oauth2.HTTPClient, gc.httpClient)
	token, err := conf.Exchange(ctx, code)
	if err != nil {
		return nil, nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	// Fetch verified user profile
	userInfo, err := gc.GetUserInfo(ctx, token.AccessToken)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch verified user info: %w", err)
	}

	stored := &StoredToken{
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
		Email:        userInfo.Email,
		UpdatedAt:    time.Now(),
	}

	return stored, userInfo, nil
}

// GetUserInfo fetches the verified user profile from Google.
func (gc *GoogleClient) GetUserInfo(ctx context.Context, accessToken string) (*GoogleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gc.userInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := gc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute userinfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("userinfo request returned status %d: %s", resp.StatusCode, string(body))
	}

	var info GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo response: %w", err)
	}

	if !info.EmailVerified {
		return nil, fmt.Errorf("google account email %s is not verified", info.Email)
	}

	return &info, nil
}

// RefreshToken refreshes an expired access token using the stored refresh token.
func (gc *GoogleClient) RefreshToken(ctx context.Context, refreshToken string) (*oauth2.Token, error) {
	if refreshToken == "" {
		return nil, errors.New("cannot refresh without refresh token")
	}

	form := url.Values{
		"client_id":     {gc.clientID},
		"client_secret": {gc.clientSecret},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gc.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := gc.httpClient.Do(req)
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
		tok.RefreshToken = refreshToken // Retain existing if provider didn't rotate
	}

	return tok, nil
}

// VerifyAccountIsolation ensures that the authenticated Google email matches the intended person.
func VerifyAccountIsolation(expectedEmail, authenticatedEmail string) error {
	expected := strings.TrimSpace(strings.ToLower(expectedEmail))
	actual := strings.TrimSpace(strings.ToLower(authenticatedEmail))

	if expected != actual {
		return fmt.Errorf("%w: authenticated as %q, but expected %q. Cross-assignment prevented.",
			ErrAccountMismatch, actual, expected)
	}
	return nil
}
