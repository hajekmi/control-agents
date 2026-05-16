package cert

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateSelfSignedECCCreatesUsableCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")

	err := GenerateSelfSignedECC(certFile, keyFile, Hosts{
		DNSNames: []string{"localhost"},
	}, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	key, ok := pair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("private key type = %T, want ECDSA", pair.PrivateKey)
	}
	if key.Curve.Params().Name != "P-256" {
		t.Fatalf("curve = %s, want P-256", key.Curve.Params().Name)
	}

	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := certificate.VerifyHostname("localhost"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("key permissions = %o, want 600", got)
	}
}

func TestEnsureSelfSignedECCReusesExistingCertificate(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "server.crt")
	keyFile := filepath.Join(dir, "server.key")

	if err := EnsureSelfSignedECC(certFile, keyFile, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureSelfSignedECC(certFile, keyFile, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(certFile)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("certificate was unexpectedly regenerated")
	}
}

func TestDefaultHostsIncludesBindAddress(t *testing.T) {
	hosts := DefaultHosts("192.0.2.10")
	for _, ip := range hosts.IPAddresses {
		if ip.String() == "192.0.2.10" {
			return
		}
	}
	t.Fatalf("bind address missing from hosts: %#v", hosts.IPAddresses)
}
