package data

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestVerifiedTLSAndScopedCA(t *testing.T) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "GRAB test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "ca.pem")
	if e = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, path string
		want       string
	}{
		{"system_roots", "", ""}, {"custom_ca", path, ""}, {"missing_ca", filepath.Join(dir, "missing.pem"), Denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e := remoteTLS(definition{Host: "db.example.com", TLS: "verify", TLSCA: tc.path})
			if errorCode(e) != tc.want {
				t.Fatal(e)
			}
			if e == nil && (c.MinVersion != tls.VersionTLS12 || c.InsecureSkipVerify || c.ServerName != "db.example.com") {
				t.Fatal("TLS verification weakened")
			}
			if e == nil && tc.path != "" && c.RootCAs == nil {
				t.Fatal("custom CA was ignored")
			}
		})
	}
	if e = os.WriteFile(path, []byte("invalid secret must not appear in errors"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = remoteTLS(definition{TLSCA: path}); errorCode(e) != Invalid {
		t.Fatal("invalid CA accepted", e)
	}
}

func TestPostgresRejectsInheritedServiceFiles(t *testing.T) {
	t.Setenv("PGSERVICE", "untrusted")
	t.Setenv("PGSERVICEFILE", filepath.Join(t.TempDir(), "must_not_read"))
	_, err := remoteConnector(definition{Driver: "postgres", Host: "127.0.0.1", Port: 5432, TLS: "disabled", Database: "grab_test", User: "grab"}, Read, "test")
	if errorCode(err) != Denied {
		t.Fatal("inherited service was parsed", err)
	}
}
