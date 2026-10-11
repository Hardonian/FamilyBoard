package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/diagnostics"
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
