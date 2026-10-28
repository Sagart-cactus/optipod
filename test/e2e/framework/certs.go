package framework

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// WebhookServingCert bundles a freshly generated test CA and a serving
// certificate for the webhook Service. The private key only ever exists in
// memory and in the in-cluster TLS Secret; it is never written to disk or
// logged. The CA PEM is public material and is safe to embed in the rendered
// MutatingWebhookConfiguration caBundle.
type WebhookServingCert struct {
	CAPEM   []byte
	CertPEM []byte
	KeyPEM  []byte
	// DNSNames are the subject alternative names the serving certificate was
	// generated for, recorded for certificate-readiness verification.
	DNSNames []string
}

// WebhookDNSNames returns the DNS SANs a serving certificate for the given
// webhook Service must carry for API-server TLS verification to succeed.
func WebhookDNSNames(serviceName, namespace string) []string {
	return []string{
		serviceName,
		serviceName + "." + namespace,
		serviceName + "." + namespace + ".svc",
		serviceName + "." + namespace + ".svc.cluster.local",
	}
}

// GenerateWebhookServingCert creates a self-signed test CA and a serving
// certificate for the webhook Service. The certificate is valid for 24 hours
// and only ever used inside the owned ephemeral cluster.
func GenerateWebhookServingCert(serviceName, namespace string) (*WebhookServingCert, error) {
	dnsNames := WebhookDNSNames(serviceName, namespace)

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	caSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate CA serial: %w", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          caSerial,
		Subject:               pkix.Name{CommonName: "optipod-e2e-webhook-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate serving key: %w", err)
	}
	serverSerial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serving serial: %w", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: serverSerial,
		Subject:      pkix.Name{CommonName: serviceName + "." + namespace + ".svc"},
		DNSNames:     dnsNames,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("create serving certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshal serving key: %w", err)
	}

	return &WebhookServingCert{
		CAPEM:    pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		CertPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}),
		KeyPEM:   pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		DNSNames: dnsNames,
	}, nil
}
