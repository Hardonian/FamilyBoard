package mime_test

import (
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/mail/mime"
)

func FuzzMIMEParse(f *testing.F) {
	parser := mime.NewParser(1024*1024, 512*1024)

	f.Add([]byte("From: alice@example.com\r\nSubject: Hello\r\n\r\nBody text"))
	f.Add([]byte(""))
	f.Add([]byte("Content-Type: multipart/mixed; boundary=xyz\r\n\r\n--xyz\r\nContent-Type: text/plain\r\n\r\nHi\r\n--xyz--"))
	f.Add([]byte("Broken headers\nWithout colon\n\nBody"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = parser.Parse(data)
	})
}
