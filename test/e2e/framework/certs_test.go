package framework

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"
)

func TestGenerateWebhookServingCert(t *testing.T) {
	cert, err := GenerateWebhookServingCert(WebhookServiceName, SystemNamespace)
	if err != nil {
		t.Fatal(err)
	}

	caBlock, _ := pem.Decode(cert.CAPEM)
	if caBlock == nil {
		t.Fatal("CA PEM did not decode")
	}
	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.IsCA {
		t.Fatal("CA certificate is not a CA")
	}

	servingBlock, _ := pem.Decode(cert.CertPEM)
	if servingBlock == nil {
		t.Fatal("serving PEM did not decode")
	}
	serving, err := x509.ParseCertificate(servingBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	// The serving certificate must verify against the harness CA for the
	// in-cluster service DNS names, exactly as the API server verifies it.
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	for _, dns := range []string{
		"optipod-webhook-service.optipod-system.svc",
		"optipod-webhook-service.optipod-system.svc.cluster.local",
	} {
		if _, err := serving.Verify(x509.VerifyOptions{
			DNSName: dns,
			Roots:   roots,
		}); err != nil {
			t.Errorf("serving cert did not verify for %s: %v", dns, err)
		}
	}
	if _, err := serving.Verify(x509.VerifyOptions{DNSName: "wrong.example.com", Roots: roots}); err == nil {
		t.Error("serving cert unexpectedly verified for a foreign DNS name")
	}

	if serving.NotAfter.Before(time.Now().Add(23 * time.Hour)) {
		t.Error("serving cert validity shorter than expected")
	}
	keyBlock, _ := pem.Decode(cert.KeyPEM)
	if keyBlock == nil || keyBlock.Type != "EC PRIVATE KEY" {
		t.Fatalf("unexpected key PEM block: %v", keyBlock)
	}
	if len(cert.DNSNames) != 4 {
		t.Errorf("DNSNames = %v", cert.DNSNames)
	}
}
