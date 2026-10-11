package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/buildinfo"
	"github.com/Hardonian/FamilyBoard/internal/diagnostics"
	"github.com/Hardonian/FamilyBoard/internal/metrics"
	"github.com/Hardonian/FamilyBoard/internal/timezone"
)

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
	metrics.Default().RecordBackup(time.Now())
	s.recordAudit(r, "backup_download", "database", "local", "Downloaded database backup; OAuth token files excluded")

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
	var bundle bytes.Buffer
	zw := zip.NewWriter(&bundle)

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
	if err := addJSON("manifest.json", map[string]any{
		"version":      buildinfo.Version,
		"commit":       buildinfo.Commit,
		"build_date":   buildinfo.Date,
		"platform":     runtime.GOOS,
		"architecture": runtime.GOARCH,
		"generated_at": time.Now().UTC(),
	}); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	// 2. Diagnostics
	report, err := diagnostics.Generate(r.Context(), s.store, s.cfg.Database.Path)
	if err != nil || addJSON("diagnostics.json", report) != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	// 3. Operational Metrics
	if err := addJSON("metrics.json", metrics.Default().Snapshot()); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	// 4. Clock and NTP Health
	if err := addJSON("clock_health.json", timezone.CheckClockHealth(s.cfg.Server.Timezone)); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	// 5. Redacted Configuration Shape (never includes tokens, keys, or passwords)
	if err := addJSON("config_shape.json", map[string]any{
		"server": map[string]any{
			"timezone":    s.cfg.Server.Timezone,
			"port":        s.cfg.Server.Port,
			"tls_enabled": s.cfg.Server.TLSCertFile != "" && s.cfg.Server.TLSKeyFile != "",
		},
		"auth": map[string]any{
			"admin_username_set": s.cfg.Auth.AdminUsername != "",
			"password_hash_set":  s.cfg.Auth.AdminPasswordHash != "",
		},
		"google": map[string]any{
			"configured": s.cfg.Google.ClientID != "",
		},
		"microsoft": map[string]any{
			"enabled":       s.cfg.Microsoft.Enabled,
			"configured":    s.cfg.Microsoft.ClientID != "",
			"custom_tenant": s.cfg.Microsoft.TenantID != "" && s.cfg.Microsoft.TenantID != "common" && s.cfg.Microsoft.TenantID != "organizations",
		},
		"security": map[string]any{
			"cookie_secure":      s.cfg.Security.CookieSecure,
			"session_secret_set": s.cfg.Security.SessionSecret != "",
		},
	}); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	// 6. Recent Audit Entries (privacy-scrubbed)
	audit, err := s.store.ListAuditLogs(r.Context(), 100, 0)
	if err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}
	type supportAuditEntry struct {
		Timestamp    time.Time `json:"timestamp"`
		Actor        string    `json:"actor"`
		Action       string    `json:"action"`
		ResourceType string    `json:"resource_type"`
	}
	redactedAudit := make([]supportAuditEntry, 0, len(audit))
	for _, entry := range audit {
		redactedAudit = append(redactedAudit, supportAuditEntry{
			Timestamp: entry.Timestamp, Actor: entry.Actor, Action: entry.Action, ResourceType: entry.ResourceType,
		})
	}
	if err := addJSON("recent_audit.json", redactedAudit); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}
	if err := zw.Close(); err != nil {
		http.Error(w, "generate support bundle", http.StatusInternalServerError)
		return
	}

	s.recordAudit(r, "support_bundle_download", "system", "local", "Downloaded comprehensive redacted support bundle")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"familyboard-support-bundle-%s.zip\"", time.Now().Format("20060102-150405")))
	w.Header().Set("Content-Length", strconv.Itoa(bundle.Len()))
	_, _ = w.Write(bundle.Bytes())
}
