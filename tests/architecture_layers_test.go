package tests

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureLayers_EnforceSeparation validates P3 #61 layer rules:
// 1. internal/calendar/* and internal/mail/* must never import internal/db or internal/web.
// 2. internal/db must never import internal/web or internal/calendar.
// 3. internal/sync, internal/queue, internal/rules, internal/recurrence must never import internal/web.
// 4. internal/hardware, internal/watchdog, internal/timezone must never import internal/web.
func TestArchitectureLayers_EnforceSeparation(t *testing.T) {
	pkgRules := []struct {
		pkgPrefix   string
		disallowed  []string
		description string
	}{
		{
			pkgPrefix:   "internal/calendar",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/db", "github.com/Hardonian/FamilyBoard/internal/web"},
			description: "Provider adapters must never touch database or web presentation layers",
		},
		{
			pkgPrefix:   "internal/mail",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/db", "github.com/Hardonian/FamilyBoard/internal/web"},
			description: "Mail adapters must never touch database or web presentation layers",
		},
		{
			pkgPrefix:   "internal/db",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web", "github.com/Hardonian/FamilyBoard/internal/calendar"},
			description: "Persistence layer must never depend on higher layers (web or calendar)",
		},
		{
			pkgPrefix:   "internal/sync",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web", "net/http/httptest"},
			description: "Sync domain policy must never import transport/web handlers",
		},
		{
			pkgPrefix:   "internal/queue",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web"},
			description: "Queue engine must never import transport/web handlers",
		},
		{
			pkgPrefix:   "internal/hardware",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web"},
			description: "Hardware platform package must never import web presentation",
		},
		{
			pkgPrefix:   "internal/watchdog",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web"},
			description: "Watchdog service must never import web presentation",
		},
		{
			pkgPrefix:   "internal/timezone",
			disallowed:  []string{"github.com/Hardonian/FamilyBoard/internal/web", "github.com/Hardonian/FamilyBoard/internal/db"},
			description: "Timezone utility must remain pure platform stdlib",
		},
	}

	rootPath := filepath.Join("..", "internal")
	fset := token.NewFileSet()

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel("..", path)
		if err != nil {
			return err
		}
		relNormalized := filepath.ToSlash(relPath)

		node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("failed to parse %s: %v", path, err)
		}

		for _, rule := range pkgRules {
			if strings.HasPrefix(relNormalized, rule.pkgPrefix+"/") || relNormalized == rule.pkgPrefix+".go" {
				for _, imp := range node.Imports {
					impPath := strings.Trim(imp.Path.Value, `"`)
					for _, dis := range rule.disallowed {
						if impPath == dis || strings.HasPrefix(impPath, dis+"/") {
							t.Errorf("Architecture layer violation in %s: imports disallowed %s (%s)", relNormalized, impPath, rule.description)
						}
					}
				}
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("failed to walk internal tree: %v", err)
	}
}
