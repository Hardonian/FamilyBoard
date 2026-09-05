package oauth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

var (
	ErrAccountMismatch  = errors.New("authenticated account does not match expected family member identity")
	ErrInvalidState     = errors.New("invalid or expired oauth state parameter")
	ErrTokenExpired     = errors.New("oauth token has expired and cannot be refreshed")
	ErrAccountRevoked   = errors.New("access revoked by provider or invalid credentials")
	ErrTokenNotFound    = errors.New("credential token not found in secure storage")
)

// StoredToken holds tokens on disk with expiry and scope metadata.
type StoredToken struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
	Scopes       string    `json:"scopes"`
	Email        string    `json:"email"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ToOAuth2Token converts StoredToken to golang.org/x/oauth2.Token.
func (st *StoredToken) ToOAuth2Token() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  st.AccessToken,
		TokenType:    st.TokenType,
		RefreshToken: st.RefreshToken,
		Expiry:       st.Expiry,
	}
}

// TokenStorage manages secure file-based storage for sensitive OAuth credentials.
type TokenStorage struct {
	baseDir string
}

func NewTokenStorage(baseDir string) (*TokenStorage, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("create token storage dir: %w", err)
	}
	return &TokenStorage{baseDir: baseDir}, nil
}

func (ts *TokenStorage) tokenPath(accountID string) string {
	// Sanitize filename to prevent directory traversal
	safeID := filepath.Base(accountID)
	return filepath.Join(ts.baseDir, fmt.Sprintf("%s.token.json", safeID))
}

// Save stores the token securely with strict 0600 file permissions.
func (ts *TokenStorage) Save(accountID string, token *StoredToken) error {
	token.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	p := ts.tokenPath(accountID)
	tmp := fmt.Sprintf("%s.tmp.%d", p, os.Getpid())
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write token temp file: %w", err)
	}

	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("atomic rename token: %w", err)
	}

	return nil
}

// Load reads and parses the stored token.
func (ts *TokenStorage) Load(accountID string) (*StoredToken, error) {
	p := ts.tokenPath(accountID)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var token StoredToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

// Delete removes the token file securely.
func (ts *TokenStorage) Delete(accountID string) error {
	p := ts.tokenPath(accountID)
	err := os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// StateManager handles cryptographic state parameter generation and verification.
type StateManager struct {
	secret []byte
}

func NewStateManager(secret string) *StateManager {
	if secret == "" {
		// Generate 32-byte ephemeral secret if none provided
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		return &StateManager{secret: b}
	}
	return &StateManager{secret: []byte(secret)}
}

// GenerateState creates a signed state string containing personID, provider, flow, and expiration.
func (sm *StateManager) GenerateState(personID, provider, flow string, validity time.Duration) (string, error) {
	exp := time.Now().Add(validity).Unix()
	nonceBytes := make([]byte, 8)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nonceBytes)

	payload := fmt.Sprintf("%s|%s|%s|%d|%s", personID, provider, flow, exp, nonce)
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	full := fmt.Sprintf("%s|%s", payload, sig)
	return base64.URLEncoding.EncodeToString([]byte(full)), nil
}

type ParsedState struct {
	PersonID string
	Provider string
	Flow     string // e.g. "calendar", "mail"
}

// VerifyState validates signature and expiration of an incoming state parameter.
func (sm *StateManager) VerifyState(stateStr string) (*ParsedState, error) {
	data, err := base64.URLEncoding.DecodeString(stateStr)
	if err != nil {
		return nil, ErrInvalidState
	}

	parts := strings.Split(string(data), "|")
	if len(parts) != 6 {
		return nil, ErrInvalidState
	}

	personID := parts[0]
	provider := parts[1]
	flow := parts[2]
	expStr := parts[3]
	nonce := parts[4]
	sig := parts[5]

	payload := fmt.Sprintf("%s|%s|%s|%s|%s", personID, provider, flow, expStr, nonce)
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(payload))
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, ErrInvalidState
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return nil, ErrInvalidState
	}

	return &ParsedState{
		PersonID: personID,
		Provider: provider,
		Flow:     flow,
	}, nil
}
