package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	Host        string `toml:"host"`
	Port        int    `toml:"port"`
	BaseURL     string `toml:"base_url"`
	Timezone    string `toml:"timezone"`
	DeviceName  string `toml:"device_name"`
	TLSCertFile string `toml:"tls_cert_file"`
	TLSKeyFile  string `toml:"tls_key_file"`
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
			TenantID:     "common",
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
	} else if envPath := strings.TrimSpace(os.Getenv("FAMILYBOARD_CONFIG")); envPath != "" {
		candidatePaths = append(candidatePaths, envPath)
		explicitPath = envPath
	} else {
		// Priority:
		// 1. /etc/familyboard/config.toml (appliance production)
		// 2. ./config.toml (local/portable install)
		// 3. ./config.example.toml (development fallback)
		candidatePaths = append(candidatePaths,
			"/etc/familyboard/config.toml",
			"config.toml",
			"config.example.toml",
		)
	}

	var foundPath string
	for _, p := range candidatePaths {
		info, err := os.Stat(p)
		if err == nil {
			if info.IsDir() {
				return nil, p, fmt.Errorf("config path is a directory: %s", p)
			}
			foundPath = p
			break
		}
		if explicitPath != "" {
			if os.IsNotExist(err) {
				return nil, p, fmt.Errorf("explicit config file does not exist: %s", p)
			}
			return nil, p, fmt.Errorf("inspect config file %s: %w", p, err)
		}
		if !os.IsNotExist(err) {
			return nil, p, fmt.Errorf("inspect config file %s: %w", p, err)
		}
	}

	if foundPath == "" {
		if err := cfg.Validate(); err != nil {
			return nil, "", err
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
	if err := cfg.Validate(); err != nil {
		return nil, foundPath, fmt.Errorf("validate config: %w", err)
	}

	return cfg, foundPath, nil
}

// Validate checks cross-field and operational constraints before the daemon
// opens files, starts listeners, or launches background workers.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is nil")
	}

	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}
	validURL := func(name, raw string, required bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			if required {
				add("%s is required", name)
			}
			return
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			add("%s must be an absolute http(s) URL without credentials", name)
		}
	}
	validClock := func(name, value string) {
		if _, err := time.Parse("15:04", value); err != nil {
			add("%s must use 24-hour HH:MM format", name)
		}
	}

	host := strings.TrimSpace(c.Server.Host)
	if host == "" || strings.ContainsAny(host, " /\\\t\r\n") {
		add("server.host must be a host name or IP address")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		add("server.port must be between 1 and 65535")
	}
	validURL("server.base_url", c.Server.BaseURL, true)
	tlsCert := strings.TrimSpace(c.Server.TLSCertFile)
	tlsKey := strings.TrimSpace(c.Server.TLSKeyFile)
	if (tlsCert == "") != (tlsKey == "") {
		add("server.tls_cert_file and server.tls_key_file must be configured together")
	}
	if tlsCert != "" {
		base, err := url.Parse(c.Server.BaseURL)
		if err != nil || base.Scheme != "https" {
			add("server.base_url must use https when TLS certificate files are configured")
		}
		if !c.Security.CookieSecure {
			add("security.cookie_secure must be true when TLS certificate files are configured")
		}
	}
	if _, err := time.LoadLocation(c.Server.Timezone); err != nil {
		add("server.timezone %q is invalid", c.Server.Timezone)
	}
	if strings.TrimSpace(c.Server.DeviceName) == "" || len(c.Server.DeviceName) > 100 {
		add("server.device_name must be between 1 and 100 characters")
	}

	if strings.TrimSpace(c.Database.Path) == "" || strings.ContainsRune(c.Database.Path, '\x00') {
		add("database.path is required")
	}
	if c.Database.BusyTimeoutMs < 1 || c.Database.BusyTimeoutMs > 600000 {
		add("database.busy_timeout_ms must be between 1 and 600000")
	}
	if c.Database.MaxOpenConns < 1 || c.Database.MaxOpenConns > 64 {
		add("database.max_open_conns must be between 1 and 64")
	}
	if c.Database.MaxIdleConns < 0 || c.Database.MaxIdleConns > c.Database.MaxOpenConns {
		add("database.max_idle_conns must be between 0 and max_open_conns")
	}

	switch c.Kiosk.Theme {
	case "light", "dark", "auto":
	default:
		add("kiosk.theme must be light, dark, or auto")
	}
	switch c.Kiosk.DefaultView {
	case "month", "hybrid", "week", "schedule", "today":
	default:
		add("kiosk.default_view must be month, hybrid, week, schedule, or today")
	}
	validClock("kiosk.day_starts_at", c.Kiosk.DayStartsAt)
	validClock("kiosk.night_starts_at", c.Kiosk.NightStartsAt)
	if parts := strings.Split(c.Kiosk.ScreenBlankSchedule, "-"); len(parts) != 2 {
		add("kiosk.screen_blank_schedule must use HH:MM-HH:MM format")
	} else {
		validClock("kiosk.screen_blank_schedule start", parts[0])
		validClock("kiosk.screen_blank_schedule end", parts[1])
	}

	if strings.TrimSpace(c.Security.CredentialsDir) == "" || strings.ContainsRune(c.Security.CredentialsDir, '\x00') {
		add("security.credentials_dir is required")
	}
	if password := c.Security.HotspotPassword; password != "" && !validHotspotPassword(password) {
		add("security.hotspot_password must be 8-63 characters or a 64-digit hexadecimal key")
	}
	if strings.TrimSpace(c.Auth.AdminUsername) == "" || len(c.Auth.AdminUsername) > 100 {
		add("auth.admin_username must be between 1 and 100 characters")
	}

	googleConfigured := strings.TrimSpace(c.Google.ClientID) != "" || strings.TrimSpace(c.Google.ClientSecret) != ""
	if googleConfigured && (strings.TrimSpace(c.Google.ClientID) == "" || strings.TrimSpace(c.Google.ClientSecret) == "") {
		add("google.client_id and google.client_secret must be configured together")
	}
	validURL("google.redirect_url", c.Google.RedirectURL, googleConfigured)
	if c.Microsoft.Enabled {
		if strings.TrimSpace(c.Microsoft.ClientID) == "" || strings.TrimSpace(c.Microsoft.ClientSecret) == "" {
			add("microsoft.client_id and microsoft.client_secret are required when Microsoft is enabled")
		}
		validURL("microsoft.redirect_url", c.Microsoft.RedirectURL, true)
	}

	if c.Sync.CalendarIntervalSeconds < 5 || c.Sync.CalendarIntervalSeconds > 86400 {
		add("sync.calendar_interval_seconds must be between 5 and 86400")
	}
	if c.Sync.MailIntervalSeconds < 5 || c.Sync.MailIntervalSeconds > 86400 {
		add("sync.mail_interval_seconds must be between 5 and 86400")
	}
	if c.Sync.MutationRetryIntervalSeconds < 1 || c.Sync.MutationRetryIntervalSeconds > 3600 {
		add("sync.mutation_retry_interval_seconds must be between 1 and 3600")
	}
	if c.Sync.MaxMutationAttempts < 1 || c.Sync.MaxMutationAttempts > 100 {
		add("sync.max_mutation_attempts must be between 1 and 100")
	}

	if c.Mail.MaxMessageSizeBytes < 1024 || c.Mail.MaxMessageSizeBytes > 100<<20 {
		add("mail.max_message_size_bytes must be between 1 KiB and 100 MiB")
	}
	if c.Mail.MaxBodyParseBytes < 1024 || c.Mail.MaxBodyParseBytes > c.Mail.MaxMessageSizeBytes {
		add("mail.max_body_parse_bytes must be between 1 KiB and max_message_size_bytes")
	}
	if c.Mail.AutoApproveConfidenceThreshold < 0 || c.Mail.AutoApproveConfidenceThreshold > 1 {
		add("mail.auto_approve_confidence_threshold must be between 0 and 1")
	}
	if c.AI.Enabled {
		validURL("ai.endpoint", c.AI.Endpoint, true)
		if strings.TrimSpace(c.AI.Model) == "" {
			add("ai.model is required when AI is enabled")
		}
		if c.AI.TimeoutSeconds < 1 || c.AI.TimeoutSeconds > 300 {
			add("ai.timeout_seconds must be between 1 and 300")
		}
	}

	return errors.Join(errs...)
}

func validHotspotPassword(password string) bool {
	if len(password) >= 8 && len(password) <= 63 {
		return true
	}
	if len(password) != 64 {
		return false
	}
	for _, char := range password {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
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
	if err := c.Validate(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if strings.TrimSpace(targetPath) == "" {
		return errors.New("config target path is required")
	}
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
