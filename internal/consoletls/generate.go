package consoletls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

// Generate creates a new private CA and server certificate, never overwriting
// an existing directory. Production operators can supply their own PKI instead.
func Generate(directory string, hosts []string) error {
	if len(hosts) == 0 || len(hosts) > 20 {
		return fmt.Errorf("one to twenty DNS names or IP addresses are required")
	}
	for _, host := range hosts {
		if host == "" || strings.ContainsAny(host, " /\\\t\r\n") || len(host) > 253 {
			return fmt.Errorf("invalid server certificate name")
		}
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return fmt.Errorf("PKI output must be a new directory")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	ca := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "OptiPod agent CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(2, 0, 0), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, key.Public(), key)
	if err != nil {
		return err
	}
	ca, err = x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	if err = writeKeyPair(directory, "ca", der, key); err != nil {
		return err
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err = rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	server := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: hosts[0]}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			server.IPAddresses = append(server.IPAddresses, ip)
		} else {
			server.DNSNames = append(server.DNSNames, host)
		}
	}
	der, err = x509.CreateCertificate(rand.Reader, server, ca, serverKey.Public(), key)
	if err != nil {
		return err
	}
	return writeKeyPair(directory, "server", der, serverKey)
}

func writeKeyPair(directory, name string, certificate []byte, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	for suffix, data := range map[string][]byte{".crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}), ".key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})} {
		file, err := os.OpenFile(filepath.Join(directory, name+suffix), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
