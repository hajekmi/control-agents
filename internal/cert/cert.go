package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const validity = 825 * 24 * time.Hour

func EnsureSelfSignedECC(certFile, keyFile, bindAddr string) error {
	if _, err := os.Stat(certFile); err == nil {
		if _, keyErr := os.Stat(keyFile); keyErr == nil {
			if _, loadErr := tlsKeyPair(certFile, keyFile); loadErr != nil {
				return fmt.Errorf("load TLS certificate: %w", loadErr)
			}
			return nil
		}
	}

	hosts := DefaultHosts(bindAddr)
	return GenerateSelfSignedECC(certFile, keyFile, hosts, time.Now())
}

func GenerateSelfSignedECC(certFile, keyFile string, hosts Hosts, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return fmt.Errorf("create cert directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ECDSA key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate certificate serial: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "Control Agents",
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              hosts.DNSNames,
		IPAddresses:           hosts.IPAddresses,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("marshal ECDSA key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o644); err != nil {
		return fmt.Errorf("write certificate: %w", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	return nil
}

type Hosts struct {
	DNSNames    []string
	IPAddresses []net.IP
}

func DefaultHosts(bindAddr string) Hosts {
	hosts := Hosts{}
	hosts.addDNS("localhost")
	if hostname, err := os.Hostname(); err == nil {
		hosts.addDNS(hostname)
	}
	hosts.addIP(net.ParseIP("127.0.0.1"))
	hosts.addIP(net.ParseIP("::1"))

	if ip := net.ParseIP(strings.TrimSpace(bindAddr)); ip != nil && !ip.IsUnspecified() {
		hosts.addIP(ip)
	}
	for _, ip := range interfaceIPs() {
		hosts.addIP(ip)
	}
	return hosts
}

func (h *Hosts) addDNS(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	for _, existing := range h.DNSNames {
		if existing == name {
			return
		}
	}
	h.DNSNames = append(h.DNSNames, name)
}

func (h *Hosts) addIP(ip net.IP) {
	if ip == nil || ip.IsUnspecified() {
		return
	}
	for _, existing := range h.IPAddresses {
		if existing.Equal(ip) {
			return
		}
	}
	h.IPAddresses = append(h.IPAddresses, ip)
}

func interfaceIPs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		var ip net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		ips = append(ips, ip)
	}
	return ips
}

func tlsKeyPair(certFile, keyFile string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}
