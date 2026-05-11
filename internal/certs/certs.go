package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"herdlite/internal/paths"
)

const (
	caCertName = "herdlite-local-ca.crt"
	caKeyName  = "herdlite-local-ca.key"
)

type Manager struct {
	Paths paths.Paths
}

type CAInfo struct {
	CertPath    string
	KeyPath     string
	Fingerprint string
	Created     bool
}

type SiteCert struct {
	Domain   string
	CertPath string
	KeyPath  string
	Created  bool
}

func (m Manager) CAPaths() (certPath string, keyPath string) {
	return filepath.Join(m.Paths.CADir, caCertName), filepath.Join(m.Paths.CADir, caKeyName)
}

func (m Manager) ExistingCA() (CAInfo, error) {
	certPath, keyPath := m.CAPaths()
	if !exists(certPath) || !exists(keyPath) {
		return CAInfo{}, fmt.Errorf("local CA does not exist; run `herdlite cert init` first")
	}

	fingerprint, err := certFingerprint(certPath)
	if err != nil {
		return CAInfo{}, err
	}

	return CAInfo{CertPath: certPath, KeyPath: keyPath, Fingerprint: fingerprint}, nil
}

func (m Manager) EnsureCA() (CAInfo, error) {
	certPath, keyPath := m.CAPaths()
	if exists(certPath) && exists(keyPath) {
		return m.ExistingCA()
	}

	if err := os.MkdirAll(m.Paths.CADir, 0o700); err != nil {
		return CAInfo{}, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return CAInfo{}, err
	}

	template := x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Herdlite Local Development CA", Organization: []string{"Herdlite"}},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return CAInfo{}, err
	}

	if err := writeCert(certPath, der); err != nil {
		return CAInfo{}, err
	}
	if err := writePrivateKey(keyPath, key); err != nil {
		return CAInfo{}, err
	}

	fingerprint, err := certFingerprint(certPath)
	if err != nil {
		return CAInfo{}, err
	}

	return CAInfo{CertPath: certPath, KeyPath: keyPath, Fingerprint: fingerprint, Created: true}, nil
}

func (m Manager) EnsureSite(domain string) (SiteCert, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || strings.ContainsAny(domain, `/\`) {
		return SiteCert{}, fmt.Errorf("invalid domain %q", domain)
	}

	caInfo, err := m.EnsureCA()
	if err != nil {
		return SiteCert{}, err
	}

	certPath := filepath.Join(m.Paths.SiteCertDir, domain+".crt")
	keyPath := filepath.Join(m.Paths.SiteCertDir, domain+".key")
	if exists(certPath) && exists(keyPath) {
		return SiteCert{Domain: domain, CertPath: certPath, KeyPath: keyPath}, nil
	}

	caCert, err := loadCert(caInfo.CertPath)
	if err != nil {
		return SiteCert{}, err
	}
	caKey, err := loadPrivateKey(caInfo.KeyPath)
	if err != nil {
		return SiteCert{}, err
	}

	if err := os.MkdirAll(m.Paths.SiteCertDir, 0o755); err != nil {
		return SiteCert{}, err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return SiteCert{}, err
	}

	template := x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: domain, Organization: []string{"Herdlite"}},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{domain},
	}

	if ip := net.ParseIP(domain); ip != nil {
		template.IPAddresses = []net.IP{ip}
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return SiteCert{}, err
	}

	if err := writeCert(certPath, der); err != nil {
		return SiteCert{}, err
	}
	if err := writePrivateKey(keyPath, key); err != nil {
		return SiteCert{}, err
	}

	return SiteCert{Domain: domain, CertPath: certPath, KeyPath: keyPath, Created: true}, nil
}

func writeCert(path string, der []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func writePrivateKey(path string, key *rsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return os.WriteFile(path, data, 0o600)
}

func loadCert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode certificate %s", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

func loadPrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode private key %s", path)
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func certFingerprint(path string) (string, error) {
	cert, err := loadCert(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

func serial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	value, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return value
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
