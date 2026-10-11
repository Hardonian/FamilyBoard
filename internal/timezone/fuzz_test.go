package timezone_test

import (
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/timezone"
)

func FuzzTimezoneResolve(f *testing.F) {
	f.Add("America/Toronto")
	f.Add("UTC")
	f.Add("Eastern Standard Time")
	f.Add("\"Pacific Standard Time\"")
	f.Add("")
	f.Add("Invalid/Zone/123")
	f.Add("etc/gmt+5")

	f.Fuzz(func(t *testing.T, name string) {
		_, _, _ = timezone.Resolve(name)
	})
}
