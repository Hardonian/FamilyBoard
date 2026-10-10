package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Server     ServerConfig    `toml:"server"`
	Database   DatabaseConfig  `toml:"database"`
	Kiosk      KioskConfig     `toml:"kiosk"`
	Security   SecurityConfig  `toml:"security"`
	Auth       AuthConfig      `toml:"auth"`
	Google     GoogleConfig    `toml:"google"`
	Microsoft  MicrosoftConfig `toml:"microsoft"`
	Sync       SyncConfig      `toml:"sync"`
	Mail       MailConfig      `toml:"mail"`
	AI         AIConfig        `toml:"ai"`
	sourcePath string
}

// EnsureSessionSecret generates a cryptographically strong, process-stable
// secret when a new installation has not configured one yet.
func (c *Config) EnsureSessionSecret() error {
	if c.Security.SessionSecret != "" {
		return nil
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("generate session secret: %w", err)
	}
	c.Security.SessionSecret = base64.RawURLEncoding.EncodeToString(random)
	return nil
}

type ServerConfig struct {
	Host       string `toml:"host"`
	Port       int    `toml:"port"`
	BaseURL    string `toml:"base_url"`
	Timezone   string `toml:"timezone"`
	DeviceName string `toml:"device_name"`
}

type DatabaseConfig struct {
	Path          string `toml:"path"`
	BusyTimeoutMs int    `toml:"busy_timeout_ms"`
	MaxOpenConns  int    `toml:"max_open_conns"`
	MaxIdleConns  int    `toml:"max_idle_conns"`
}

type KioskConfig struct {
	Theme               string `toml:"theme"`
	DefaultView         string `toml:"default_view"`
	DayStartsAt         string `toml:"day_starts_at"`
	NightStartsAt       string `toml:"night_starts_at"`
	ScreenBlankEnabled  bool   `toml:"screen_blank_enabled"`
	ScreenBlankSchedule string `toml:"screen_blank_schedule"`
}

type SecurityConfig struct {
	SessionSecret   string `toml:"session_secret"`
	HotspotPassword string `toml:"hotspot_password"`
	CookieSecure    bool   `toml:"cookie_secure"`
	CredentialsDir  string `toml:"credentials_dir"`
}

// EnsureHotspotPassword generates a stable per-installation WPA2 passphrase.
func (c *Config) EnsureHotspotPassword() error {
	if c.Security.HotspotPassword != "" {
		return nil
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return fmt.Errorf("generate hotspot password: %w", err)
	}
	c.Security.HotspotPassword = base64.RawURLEncoding.EncodeToString(random)
	return nil
}

type AuthConfig struct {
	AdminUsername     string `toml:"admin_username"`
	AdminPasswordHash string `toml:"admin_password_hash"`
}

type GoogleConfig struct {
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	RedirectURL  string `toml:"redirect_url"`
}

type MicrosoftConfig struct {
	Enabled      bool   `toml:"enabled"`
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`
	RedirectURL  string `toml:"redirect_url"`
	TenantID     string `toml:"tenant_id"`
}

type SyncConfig struct {
	CalendarIntervalSeconds      int `toml:"calendar_interval_seconds"`
	MailIntervalSeconds          int `toml:"mail_interval_seconds"`
	MutationRetryIntervalSeconds int `toml:"mutation_retry_interval_seconds"`
	MaxMutationAttempts          int `toml:"max_mutation_attempts"`
}

type MailConfig struct {
	MaxMessageSizeBytes            int64   `toml:"max_message_size_bytes"`
	MaxBodyParseBytes              int64   `toml:"max_body_parse_bytes"`
	AutoApproveConfidenceThreshold float64 `toml:"auto_approve_confidence_threshold"`
	GmailLabel                     string  `toml:"gmail_label"`
}

type AIConfig struct {
	Enabled        bool   `toml:"enabled"`
	Endpoint       string `toml:"endpoint"`
	Model          string `toml:"model"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:       "127.0.0.1",
			Port:       8080,
			BaseURL:    "http://127.0.0.1:8080",
			Timezone:   "America/Toronto",
			DeviceName: "FamilyBoard Appliance",
		},
		Database: DatabaseConfig{
			Path:          "familyboard.db",
			BusyTimeoutMs: 5000,
			MaxOpenConns:  4,
			MaxIdleConns:  2,
		},
		Kiosk: KioskConfig{
			Theme:               "auto",
			DefaultView:         "month",
			DayStartsAt:         "07:00",
			NightStartsAt:       "20:30",
			ScreenBlankEnabled:  false,
			ScreenBlankSchedule: "23:00-06:00",
		},
		Security: SecurityConfig{
			SessionSecret:  "",
			CookieSecure:   false,
			CredentialsDir: "credentials",
		},
		Auth: AuthConfig{
			AdminUsername:     "admin",
			AdminPasswordHash: "",
		},
		Google: GoogleConfig{
			ClientID:     "",
			ClientSecret: "",
			RedirectURL:  "http://127.0.0.1:8080/oauth/callback/google",
		},
		Microsoft: MicrosoftConfig{
			Enabled:      false,
			ClientID:     "",
			ClientSecret: "",
			RedirectURL:  "http://127.0.0.1:8080/oauth/callback/microsoft",
		},
		Sync: SyncConfig{
			CalendarIntervalSeconds:      180,
			MailIntervalSeconds:          300,
			MutationRetryIntervalSeconds: 30,
			MaxMutationAttempts:          5,
		},
		Mail: MailConfig{
			MaxMessageSizeBytes:            10 * 1024 * 1024, // 10 MB
			MaxBodyParseBytes:              5 * 1024 * 1024,  // 5 MB
			AutoApproveConfidenceThreshold: 0.90,
			GmailLabel:                     "FamilyBoard",
		},
		AI: AIConfig{
			Enabled:        false,
			Endpoint:       "http://127.0.0.1:11434/v1",
			Model:          "llama3:latest",
			TimeoutSeconds: 3,
		},
	}
}

// Load loads configuration from the specified path, or checks standard locations.
func Load(explicitPath string) (*Config, string, error) {
	cfg := DefaultConfig()

	candidatePaths := []string{}
	if explicitPath != "" {
		candidatePaths = append(candidatePaths, explicitPath)
	} else {
		// Priority:
		// 1. /etc/familyboard/config.toml (appliance production)
		// 2. ./config.toml (local dev)
		// 3. ./config.example.toml (fallback)
		candidatePaths = append(candidatePaths,
			"/etc/familyboard/config.toml",
			"config.toml",
			"config.example.toml",
		)
	}

	var foundPath string
	for _, p := range candidatePaths {
		if _, err := os.Stat(p); err == nil {
			foundPath = p
			break
		}
	}

	if foundPath == "" {
		if explicitPath != "" {
			cfg.sourcePath = explicitPath
		}
		return cfg, "", nil // Return defaults if no config file found
	}

	data, err := os.ReadFile(foundPath)
	if err != nil {
		return nil, foundPath, fmt.Errorf("read config file: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, foundPath, fmt.Errorf("unmarshal config TOML: %w", err)
	}
	cfg.sourcePath = foundPath

	return cfg, foundPath, nil
}

// SourcePath returns the file this configuration was loaded from, or fallback
// for a new installation.
func (c *Config) SourcePath(fallback string) string {
	if c.sourcePath != "" {
		return c.sourcePath
	}
	return fallback
}

// Redacted returns a clone of the configuration with sensitive tokens and passwords scrubbed.
func (c *Config) Redacted() Config {
	clone := *c
	if clone.Security.SessionSecret != "" {
		clone.Security.SessionSecret = "[REDACTED]"
	}
	if clone.Security.HotspotPassword != "" {
		clone.Security.HotspotPassword = "[REDACTED]"
	}
	if clone.Auth.AdminPasswordHash != "" {
		clone.Auth.AdminPasswordHash = "[REDACTED]"
	}
	if clone.Google.ClientSecret != "" {
		clone.Google.ClientSecret = "[REDACTED]"
	}
	if clone.Microsoft.ClientSecret != "" {
		clone.Microsoft.ClientSecret = "[REDACTED]"
	}
	return clone
}

// Save writes the configuration to a file atomically.
func (c *Config) Save(targetPath string) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config TOML: %w", err)
	}

	tmpFile := fmt.Sprintf("%s.tmp.%d", targetPath, os.Getpid())
	if err := os.WriteFile(tmpFile, data, 0600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	if err := os.Rename(tmpFile, targetPath); err != nil {
		// Windows does not replace an existing destination with os.Rename. Move
		// the current file aside, install the complete new file, and roll back if
		// that second rename fails.
		if _, statErr := os.Stat(targetPath); statErr != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("atomic rename config: %w", err)
		}
		backupPath := tmpFile + ".previous"
		if backupErr := os.Rename(targetPath, backupPath); backupErr != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("prepare config replacement: %w", backupErr)
		}
		if replaceErr := os.Rename(tmpFile, targetPath); replaceErr != nil {
			_ = os.Rename(backupPath, targetPath)
			_ = os.Remove(tmpFile)
			return fmt.Errorf("replace config: %w", replaceErr)
		}
		_ = os.Remove(backupPath)
	}
	c.sourcePath = targetPath

	return nil
}
