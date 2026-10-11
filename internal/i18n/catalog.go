package i18n

import "strings"

var catalogs = map[string]map[string]string{
	"en": {
		"nav.accounts": "Accounts", "nav.calendars": "Calendars", "nav.mail": "Mail Sources",
		"nav.approvals": "Approval Queue", "nav.conflicts": "Conflicts", "nav.queue": "Mutation Queue",
		"nav.senders": "Trusted Senders", "nav.audit": "Audit Log", "nav.privacy": "Privacy & Devices",
		"nav.hardware": "Hardware & Network", "nav.system": "System & Diagnostics",
		"view.month": "Month", "view.week": "Week", "view.schedule": "Schedule", "view.day": "Day",
	},
	"fr": {
		"nav.accounts": "Comptes", "nav.calendars": "Calendriers", "nav.mail": "Sources de courriel",
		"nav.approvals": "File d’approbation", "nav.conflicts": "Conflits", "nav.queue": "File de synchronisation",
		"nav.senders": "Expéditeurs fiables", "nav.audit": "Journal d’audit", "nav.privacy": "Confidentialité et appareils",
		"nav.hardware": "Matériel et réseau", "nav.system": "Système et diagnostic",
		"view.month": "Mois", "view.week": "Semaine", "view.schedule": "Horaire", "view.day": "Jour",
	},
}

// T resolves a UI key using a BCP 47 locale with language fallback and an
// English fallback. Unknown keys remain visible to make missing catalog entries
// obvious during development rather than silently rendering blank controls.
func T(locale, key string) string {
	language := strings.ToLower(strings.Split(strings.TrimSpace(locale), "-")[0])
	if catalog := catalogs[language]; catalog != nil && catalog[key] != "" {
		return catalog[key]
	}
	if value := catalogs["en"][key]; value != "" {
		return value
	}
	return key
}
