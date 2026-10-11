package i18n

import "testing"

func TestTUsesLocaleAndFallback(t *testing.T) {
	if got := T("fr-CA", "view.week"); got != "Semaine" {
		t.Fatalf("French Canadian lookup = %q", got)
	}
	if got := T("de-DE", "view.week"); got != "Week" {
		t.Fatalf("English fallback = %q", got)
	}
	if got := T("en-CA", "missing.key"); got != "missing.key" {
		t.Fatalf("missing key visibility = %q", got)
	}
}
