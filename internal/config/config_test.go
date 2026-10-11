package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSessionSecretIsStrongAndStable(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.EnsureSessionSecret(); err != nil {
		t.Fatalf("EnsureSessionSecret: %v", err)
	}
	if err := cfg.EnsureHotspotPassword(); err != nil {
		t.Fatalf("EnsureHotspotPassword: %v", err)
	}
	if len(cfg.Security.HotspotPassword) < 16 {
		t.Fatalf("expected a strong generated hotspot password, got %d characters", len(cfg.Security.HotspotPassword))
	}
	first := cfg.Security.SessionSecret
	if len(first) < 43 {
		t.Fatalf("expected at least 256 bits of encoded entropy, got %d characters", len(first))
	}
	if err := cfg.EnsureSessionSecret(); err != nil {
		t.Fatalf("second EnsureSessionSecret: %v", err)
	}
	if cfg.Security.SessionSecret != first {
		t.Fatal("existing session secret must not be rotated implicitly")
	}
}

func TestSaveCanSafelyReplaceAndRememberSource(t *testing.T) {
	target := filepath.Join(t.TempDir(), "config.toml")
	cfg := DefaultConfig()
	if err := cfg.EnsureSessionSecret(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureHotspotPassword(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(target); err != nil {
		t.Fatalf("initial Save: %v", err)
	}
	cfg.Server.DeviceName = "Updated Kitchen Board"
	if err := cfg.Save(target); err != nil {
		t.Fatalf("replacement Save: %v", err)
	}

	loaded, loadedPath, err := Load(target)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadedPath != target || loaded.SourcePath("fallback") != target {
		t.Fatalf("expected source path %q, got %q / %q", target, loadedPath, loaded.SourcePath("fallback"))
	}
	if loaded.Server.DeviceName != "Updated Kitchen Board" {
		t.Fatalf("replacement content was not persisted: %q", loaded.Server.DeviceName)
	}
	if loaded.Security.SessionSecret != cfg.Security.SessionSecret {
		t.Fatal("session secret changed during save/load")
	}
	if loaded.Security.HotspotPassword != cfg.Security.HotspotPassword {
		t.Fatal("hotspot password changed during save/load")
	}
}

func TestLoadUsesEnvironmentConfigForPortableInstall(t *testing.T) {
	target := filepath.Join(t.TempDir(), "portable.toml")
	cfg := DefaultConfig()
	cfg.Server.DeviceName = "Portable FamilyBoard"
	if err := cfg.Save(target); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("FAMILYBOARD_CONFIG", target)

	loaded, loadedPath, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loadedPath != target || loaded.Server.DeviceName != "Portable FamilyBoard" {
		t.Fatalf("environment config was not loaded: path=%q device=%q", loadedPath, loaded.Server.DeviceName)
	}
}

func TestLoadRejectsMissingEnvironmentConfig(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	t.Setenv("FAMILYBOARD_CONFIG", missing)

	if _, _, err := Load(""); err == nil {
		t.Fatal("expected a missing FAMILYBOARD_CONFIG path to fail")
	}
}

func TestValidateDirectTLSRequiresSecureOriginAndCookies(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Server.TLSCertFile = "server.crt"
	cfg.Server.TLSKeyFile = "server.key"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected direct TLS with HTTP base URL and insecure cookie to fail")
	}
	cfg.Server.BaseURL = "https://familyboard.local:8443"
	cfg.Security.CookieSecure = true
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid direct TLS configuration: %v", err)
	}
}

func TestConfig_DeprecationsAndUnknownKeys(t *testing.T) {
	raw := []byte(`
[server]
host = "127.0.0.1"

[security]
legacy_token_dir = "/tmp/old"

[future_unknown_section]
feature_xyz = true
`)
	deprecations, unknown, err := CheckDeprecationsAndUnknown(raw)
	if err != nil {
		t.Fatalf("CheckDeprecationsAndUnknown failed: %v", err)
	}
	if len(deprecations) != 1 {
		t.Errorf("expected 1 deprecation warning, got %d", len(deprecations))
	}
	if len(unknown) != 1 || unknown[0] != "future_unknown_section" {
		t.Errorf("expected unknown section 'future_unknown_section', got %v", unknown)
	}
}

func TestConfig_MigrationFromUnversioned(t *testing.T) {
	raw := []byte(`
[server]
host = "127.0.0.1"
port = 8080
`)
	migrated, changed, err := MigrateConfig(raw)
	if err != nil {
		t.Fatalf("MigrateConfig failed: %v", err)
	}
	if !changed {
		t.Errorf("expected unversioned config to be upgraded")
	}
	if !strings.Contains(string(migrated), "config_version = 1") {
		t.Errorf("expected migrated config to contain config_version = 1, got:\n%s", string(migrated))
	}

	// Migrating already current config should make no changes
	_, changed2, _ := MigrateConfig(migrated)
	if changed2 {
		t.Errorf("expected already current config not to be modified")
	}
}
