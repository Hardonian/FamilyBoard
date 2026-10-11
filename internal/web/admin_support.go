package web

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/diagnostics"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
	"github.com/Hardonian/FamilyBoard/internal/timezone"
)

func (s *Server) handleAdminBackupDownload(w http.ResponseWriter, r *http.Request) {
	s.recordAudit(r, "backup_download", "database", "local", "Downloaded database backup; OAuth token files excluded")
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

func (s *Server) handleAdminDiagnosticsDownload(w http.ResponseWriter, r *http.Request) {
	s.recordAudit(r, "diagnostics_download", "system", "local", "Downloaded redacted diagnostics report")
	report, err := diagnostics.Generate(r.Context(), s.store, s.cfg.Database.Path)
	if err != nil {
		http.Error(w, "generate diagnostics: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"familyboard-diagnostics-%s.json\"", time.Now().Format("20060102-150405")))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
}

func (s *Server) handleAdminSupportBundle(w http.ResponseWriter, r *http.Request) {
	s.recordAudit(r, "support_bundle_download", "system", "local", "Downloaded comprehensive support bundle")

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"familyboard-support-bundle-%s.zip\"", time.Now().Format("20060102-150405")))

	zw := zip.NewWriter(w)
	defer zw.Close()

	addJSON := func(name string, v any) error {
		fw, err := zw.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(fw)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}

	// 1. Manifest
	_ = addJSON("manifest.json", map[string]any{
		"version":      buildinfo.Version,
		"commit":       buildinfo.Commit,
		"build_date":   buildinfo.Date,
		"platform":     runtime.GOOS,
		"architecture": runtime.GOARCH,
		"generated_at": time.Now().UTC(),
	})

	// 2. Diagnostics
	if report, err := diagnostics.Generate(r.Context(), s.store, s.cfg.Database.Path); err == nil {
		_ = addJSON("diagnostics.json", report)
	}

	// 3. Operational Metrics
	_ = addJSON("metrics.json", metrics.Default().Snapshot())

	// 4. Clock and NTP Health
	_ = addJSON("clock_health.json", timezone.CheckClockHealth(s.cfg.Server.Timezone))

	// 5. Redacted Configuration Shape (never includes tokens, keys, or passwords)
	_ = addJSON("config_shape.json", map[string]any{
		"server": map[string]any{
			"device_name": s.cfg.Server.DeviceName,
			"timezone":    s.cfg.Server.Timezone,
			"port":        s.cfg.Server.Port,
			"host":        s.cfg.Server.Host,
		},
		"auth": map[string]any{
			"admin_username":    s.cfg.Auth.AdminUsername,
			"password_hash_set": s.cfg.Auth.AdminPasswordHash != "",
		},
		"google": map[string]any{
			"configured": s.cfg.Google.ClientID != "",
		},
		"microsoft": map[string]any{
			"enabled":    s.cfg.Microsoft.Enabled,
			"configured": s.cfg.Microsoft.ClientID != "",
			"tenant_id":  s.cfg.Microsoft.TenantID,
		},
		"security": map[string]any{
			"cookie_secure":      s.cfg.Security.CookieSecure,
			"session_secret_set": s.cfg.Security.SessionSecret != "",
		},
	})

	// 6. Recent Audit Entries (privacy-scrubbed)
	if audit, err := s.store.ListAuditLogs(r.Context(), 100, 0); err == nil {
		_ = addJSON("recent_audit.json", audit)
	}
}
