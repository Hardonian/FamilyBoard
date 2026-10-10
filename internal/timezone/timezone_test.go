package timezone

import "testing"

func TestResolveIANAAndWindowsNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "America/Toronto", want: "America/Toronto"},
		{input: "Eastern Standard Time", want: "America/New_York"},
		{input: `"Pacific Standard Time"`, want: "America/Los_Angeles"},
		{input: "UTC", want: "UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			_, got, err := Resolve(tt.input)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("Resolve(%q) canonical name = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestResolveRejectsUnknownName(t *testing.T) {
	t.Parallel()
	if _, _, err := Resolve("Mars/Olympus_Mons"); err == nil {
		t.Fatal("expected unknown timezone to fail")
	}
}
