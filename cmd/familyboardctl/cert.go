package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runGenerateCert(args []string) {
	fs := flag.NewFlagSet("generate-cert", flag.ExitOnError)
	hostsFlag := fs.String("hosts", "localhost,familyboard.local", "Comma-separated list of hostnames and IP addresses for the certificate")
	outDirFlag := fs.String("out", "credentials", "Directory to write the certificate and private key")
	fs.Parse(args)

	if err := os.MkdirAll(*outDirFlag, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	certPath := filepath.Join(*outDirFlag, "familyboard.crt")
	keyPath := filepath.Join(*outDirFlag, "familyboard.key")

	if err := generateSelfSignedCert(certPath, keyPath, *hostsFlag); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating certificate: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("=== Generated Local LAN TLS Certificate ===")
	fmt.Printf("Certificate: %s\n", certPath)
	fmt.Printf("Private Key: %s (mode 0600)\n\n", keyPath)
	fmt.Println("To enable direct HTTPS on your appliance, add to config.toml:")
	fmt.Printf("  [server]\n")
	fmt.Printf("  tls_cert_file = %q\n", certPath)
	fmt.Printf("  tls_key_file  = %q\n\n", keyPath)
	fmt.Println("To trust this certificate on mobile devices:")
	fmt.Println("  - iOS: AirDrop or email familyboard.crt, install Profile in Settings, then enable Full Trust under Certificate Trust Settings.")
	fmt.Println("  - Android: Open Settings -> Security -> Install from storage -> CA certificate, and select familyboard.crt.")
}

func generateSelfSignedCert(certFile, keyFile, hosts string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ECDSA private key: %w", err)
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}

	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.Add(3 * 365 * 24 * time.Hour) // 3 years validity

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"FamilyBoard Household Appliance"},
			CommonName:   "familyboard.local",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Always include loopback addresses
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
	template.DNSNames = append(template.DNSNames, "localhost", "familyboard.local")

	// Parse provided hosts
	for _, h := range strings.Split(hosts, ",") {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	// Discover local non-loopback network interfaces to add local LAN IP
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
					template.IPAddresses = append(template.IPAddresses, ip)
				}
			}
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("create x509 certificate: %w", err)
	}

	// Write certificate
	certOut, err := os.OpenFile(certFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open cert file for write: %w", err)
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return fmt.Errorf("encode cert PEM: %w", err)
	}

	// Write private key
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshal ECDSA private key: %w", err)
	}
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("open key file for write: %w", err)
	}
	defer keyOut.Close()
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b}); err != nil {
		return fmt.Errorf("encode key PEM: %w", err)
	}

	return nil
}
