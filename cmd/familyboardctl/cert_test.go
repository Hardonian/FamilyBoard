package main

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "test.crt")
	keyFile := filepath.Join(dir, "test.key")

	err := generateSelfSignedCert(certFile, keyFile, "my-kiosk.local,192.168.1.100")
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	certBytes, err := os.ReadFile(certFile)
	if err != nil {
		t.Fatalf("failed to read cert file: %v", err)
	}
	block, _ := pem.Decode(certBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("invalid PEM certificate block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if cert.Subject.CommonName != "familyboard.local" {
		t.Errorf("expected CommonName familyboard.local, got: %s", cert.Subject.CommonName)
	}

	dnsFound := false
	for _, dns := range cert.DNSNames {
		if dns == "my-kiosk.local" {
			dnsFound = true
			break
		}
	}
	if !dnsFound {
		t.Errorf("expected my-kiosk.local in DNS names, got: %v", cert.DNSNames)
	}

	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("failed to read key file info: %v", err)
	}
	if keyInfo.Size() == 0 {
		t.Errorf("key file is empty")
	}
}
