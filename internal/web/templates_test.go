package web

import (
	"html/template"
	"testing"

	webassets "github.com/Hardonian/FamilyBoard/web"
)

func TestEmbeddedTemplatesParse(t *testing.T) {
	t.Parallel()
	funcs := template.FuncMap{
		"mul":             func(a, b float64) float64 { return a * b },
		"div":             func(a, b float64) float64 { return a / b },
		"float":           func(value int64) float64 { return float64(value) },
		"asset":           func(path string) string { return path },
		"t":               func(key string) string { return key },
		"conflictSummary": func(payload string) string { return payload },
	}
	for _, page := range []string{"kiosk.html", "login.html", "setup.html", "admin.html"} {
		t.Run(page, func(t *testing.T) {
			if _, err := template.New("").Funcs(funcs).ParseFS(webassets.Content, "templates/layout.html", "templates/"+page); err != nil {
				t.Fatalf("parse %s: %v", page, err)
			}
		})
	}
}
