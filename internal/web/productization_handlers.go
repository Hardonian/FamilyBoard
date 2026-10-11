package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/models"
)

func (s *Server) recordAudit(r *http.Request, action, resourceType, resourceID, details string) {
	_ = s.store.RecordAudit(r.Context(), &models.AuditEntry{
		ID: models.NewID("aud"), Actor: "admin", Action: action,
		ResourceType: resourceType, ResourceID: resourceID,
		Details: details, IPAddress: remoteIP(r),
	})
}

func (s *Server) handleAdminDeviceRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.RevokePairedDevice(r.Context(), id); err != nil {
		http.Redirect(w, r, "/admin?tab=privacy&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "device_revoke", "paired_device", id, "Revoked a paired household device")
	http.Redirect(w, r, "/admin?tab=privacy&msg=Device+access+revoked", http.StatusSeeOther)
}

func (s *Server) handleAdminDevicesRevokeAll(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.RevokeAllPairedDevices(r.Context())
	if err != nil {
		http.Redirect(w, r, "/admin?tab=privacy&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "device_revoke_all", "paired_device", "*", fmt.Sprintf("Revoked %d paired devices", count))
	http.Redirect(w, r, fmt.Sprintf("/admin?tab=privacy&msg=Revoked+%d+paired+devices", count), http.StatusSeeOther)
}

func (s *Server) handleAdminPersonCreate(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("display_name"))
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	if name == "" || !strings.Contains(email, "@") || len(name) > 100 || len(email) > 254 {
		http.Redirect(w, r, "/admin?tab=accounts&err=Enter+a+valid+name+and+email", http.StatusSeeOther)
		return
	}
	if existing, _ := s.store.GetPersonByEmail(r.Context(), email); existing != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=That+email+already+belongs+to+a+family+member", http.StatusSeeOther)
		return
	}
	person := &models.Person{ID: models.NewID("person"), DisplayName: name, Email: email, Enabled: true}
	if err := s.store.UpsertPerson(r.Context(), person); err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "person_create", "person", person.ID, "Added family member "+name)
	http.Redirect(w, r, "/admin?tab=accounts&msg=Family+member+added", http.StatusSeeOther)
}

func (s *Server) handleAdminPersonDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	people, err := s.store.ListPeople(r.Context())
	if err != nil || len(people) <= 1 {
		http.Redirect(w, r, "/admin?tab=accounts&err=At+least+one+family+member+is+required", http.StatusSeeOther)
		return
	}
	person, _ := s.store.GetPerson(r.Context(), id)
	if person == nil {
		http.Redirect(w, r, "/admin?tab=accounts&err=Family+member+not+found", http.StatusSeeOther)
		return
	}
	accounts, _ := s.store.ListOAuthAccounts(r.Context())
	for _, account := range accounts {
		if account.PersonID == id {
			_ = s.tokenStorage.Delete(account.ID)
		}
	}
	if err := s.store.DeletePerson(r.Context(), id); err != nil {
		http.Redirect(w, r, "/admin?tab=accounts&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "person_delete", "person", id, "Deleted family member and connected account data: "+person.DisplayName)
	http.Redirect(w, r, "/admin?tab=accounts&msg=Family+member+and+connected+data+deleted", http.StatusSeeOther)
}

func (s *Server) handleAdminPrivacyExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	people, _ := s.store.ListPeople(ctx)
	accounts, _ := s.store.ListOAuthAccounts(ctx)
	calendars, _ := s.store.ListCalendars(ctx)
	events, _ := s.store.ListAllEventsForExport(ctx)
	audit, _ := s.store.ListAuditLogs(ctx, 10000, 0)
	for i := range accounts {
		accounts[i].CredentialReference = "[REDACTED]"
	}
	export := map[string]any{
		"format": "familyboard-privacy-export-v1", "exported_at": time.Now().UTC(),
		"people": people, "connected_accounts": accounts, "calendars": calendars,
		"events": events, "audit_log": audit,
		"excluded": []string{"oauth access and refresh tokens", "admin password hash", "installation secrets"},
	}
	s.recordAudit(r, "privacy_export", "household", "local", "Exported household data without credentials")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=familyboard-privacy-export-%s.json", time.Now().Format("20060102-150405")))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(export)
}

func (s *Server) handleAPISystemStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	calendars, calErr := s.store.ListCalendars(ctx)
	pending, queueErr := s.store.CountPendingMutations(ctx)
	dead, _ := s.store.CountDeadLetterMutations(ctx)
	oldestSync := (*time.Time)(nil)
	for i := range calendars {
		if calendars[i].LastSyncedAt != nil && (oldestSync == nil || calendars[i].LastSyncedAt.Before(*oldestSync)) {
			value := *calendars[i].LastSyncedAt
			oldestSync = &value
		}
	}
	state := "healthy"
	message := "Up to date"
	if calErr != nil || queueErr != nil || dead > 0 {
		state, message = "error", "Synchronization needs attention"
	} else if pending > 0 {
		state, message = "syncing", fmt.Sprintf("%d change(s) waiting to sync", pending)
	} else if len(calendars) == 0 {
		state, message = "setup", "Connect a calendar in Admin"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"state": state, "message": message, "pending_mutations": pending,
		"failed_mutations": dead, "calendar_count": len(calendars), "oldest_last_sync": oldestSync,
		"calendars": calendars,
	})
}

func (s *Server) handleAdminPrivacyRetention(w http.ResponseWriter, r *http.Request) {
	eventDays, eventErr := strconv.Atoi(r.FormValue("deleted_event_days"))
	auditDays, auditErr := strconv.Atoi(r.FormValue("audit_days"))
	if eventErr != nil || auditErr != nil || eventDays < 7 || eventDays > 3650 || auditDays < 30 || auditDays > 3650 {
		http.Redirect(w, r, "/admin?tab=privacy&err=Retention+must+be+7-3650+days+for+deleted+events+and+30-3650+days+for+audit+history", http.StatusSeeOther)
		return
	}
	if err := s.store.SetSetting(r.Context(), "deleted_event_retention_days", strconv.Itoa(eventDays)); err != nil {
		http.Redirect(w, r, "/admin?tab=privacy&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if err := s.store.SetSetting(r.Context(), "audit_retention_days", strconv.Itoa(auditDays)); err != nil {
		http.Redirect(w, r, "/admin?tab=privacy&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	removedEvents, removedAudit, err := s.store.ApplyPrivacyRetention(r.Context(), eventDays, auditDays)
	if err != nil {
		http.Redirect(w, r, "/admin?tab=privacy&err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	s.recordAudit(r, "privacy_retention", "household", "local", fmt.Sprintf("Retention updated; removed %d deleted events and %d old audit records", removedEvents, removedAudit))
	http.Redirect(w, r, "/admin?tab=privacy&msg=Retention+policy+saved+and+applied", http.StatusSeeOther)
}
