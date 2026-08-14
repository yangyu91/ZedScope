// Package ca implements a tiny on-device certificate authority used to
// mint per-host leaf certificates for MITM interception.
//
// Security note: the generated root CA private key lives only in process
// memory and is never written to disk by this package. The app is expected
// to export CertPEM() and let the USER install it as a user-trusted CA.
// Interception only works for traffic the device owner chooses to route
// through the local proxy.
package ca

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"sync"
	"time"
)

// CA is a self-signed root used to sign per-host leaf certificates.
type CA struct {
	cert    *x509.Certificate
	key     *rsa.PrivateKey
	certPEM string
	mu      sync.Mutex
	cache   map[string][]byte // host -> PEM(cert)+PEM(key)
}

// NewCA generates a fresh root CA.
func NewCA() *CA {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "yami-UA CA", Organization: []string{"yami-UA"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &CA{
		cert:    cert,
		key:     key,
		certPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		cache:   map[string][]byte{},
	}
}

// CertPEM returns the PEM-encoded root certificate for the user to install.
func (c *CA) CertPEM() string { return c.certPEM }

// LeafFor returns a TLS certificate valid for host, signed by this CA.
func (c *CA) LeafFor(host string) (*tls.Certificate, error) {
	c.mu.Lock()
	if b, ok := c.cache[host]; ok {
		c.mu.Unlock()
		kp, err := tls.X509KeyPair(b, b)
		if err != nil {
			return nil, err
		}
		return &kp, nil
	}
	c.mu.Unlock()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	sn, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: sn,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	combined := append(append([]byte{}, pemCert...), pemKey...)
	c.mu.Lock()
	c.cache[host] = combined
	c.mu.Unlock()

	return &tls.Certificate{Certificate: [][]byte{der, c.cert.Raw}, PrivateKey: key, Leaf: leaf}, nil
}
