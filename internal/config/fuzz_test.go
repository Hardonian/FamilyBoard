package config_test

import (
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/config"
)

func FuzzConfigParse(f *testing.F) {
	f.Add([]byte("[server]\nhost = \"127.0.0.1\"\nport = 8080\nbase_url = \"http://127.0.0.1:8080\"\n"))
	f.Add([]byte(""))
	f.Add([]byte("random invalid text here %%%% \x00\xff"))
	f.Add([]byte("[security]\nlegacy_token_dir = \"/tmp\"\n[unknown]\nx = 1\n"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = config.CheckDeprecationsAndUnknown(data)
		_, _, _ = config.MigrateConfig(data)
	})
}
