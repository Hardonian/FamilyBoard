package restore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStringsContainsDotDot(t *testing.T) {
	t.Parallel()

	absolutePath := filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), "database.db")
	tests := []struct {
		name   string
		path   string
		unsafe bool
	}{
		{name: "expected file", path: "database.db"},
		{name: "dot prefix in filename", path: "..database.db"},
		{name: "parent directory", path: "../database.db", unsafe: true},
		{name: "embedded traversal", path: "safe/../database.db", unsafe: true},
		{name: "absolute path", path: absolutePath, unsafe: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stringsContainsDotDot(tt.path); got != tt.unsafe {
				t.Fatalf("stringsContainsDotDot(%q) = %v, want %v", tt.path, got, tt.unsafe)
			}
		})
	}
}
