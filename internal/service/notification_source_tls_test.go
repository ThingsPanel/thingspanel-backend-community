package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceBridgeLoadsOperatorCAWithoutDisablingTLSVerification(t *testing.T) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate fixture CA")
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(101), Subject: pkix.Name{CommonName: "notification fixture root"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("create fixture CA")
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal("parse fixture CA")
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate fixture TLS key")
	}
	serverTemplate := &x509.Certificate{SerialNumber: big.NewInt(102), Subject: pkix.Name{CommonName: "127.0.0.1"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, BasicConstraintsValid: true}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, ca, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("create fixture server certificate")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sourceGroupSnapshotEnvelope{Code: 200, Message: "ok", RequestID: "fixture", Data: emailSnapshotFixture()})
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{serverDER, caDER}, PrivateKey: serverKey}}}
	server.StartTLS()
	defer server.Close()

	caPath := filepath.Join(t.TempDir(), "source-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal("write fixture CA")
	}
	roots, err := LoadSourceBridgeRoots(caPath)
	if err != nil {
		t.Fatalf("load operator CA: %v", err)
	}
	checker, err := NewEmailSourceCompatibilityCheckerWithRoots(server.URL, "projection-fixture-token-at-least-32", roots)
	if err != nil {
		t.Fatalf("construct checker with operator CA: %v", err)
	}
	defer checker.Close()
	request := SourceGroupProjectionRequest{TenantID: "tenant-a", NotificationGroupID: "native-a", GroupRevision: 7}
	if _, err := checker.fetchSnapshot(context.Background(), request); err != nil {
		t.Fatalf("trusted private TLS snapshot failed: %v", err)
	}

	withoutRoots, err := NewEmailSourceCompatibilityChecker(server.URL, "projection-fixture-token-at-least-32")
	if err != nil {
		t.Fatal("construct system-root checker")
	}
	defer withoutRoots.Close()
	if _, err := withoutRoots.fetchSnapshot(context.Background(), request); err == nil {
		t.Fatal("untrusted server certificate was accepted without configured CA")
	}
}

func TestSourceBridgeCAFileRejectsMalformedAndNonCAFiles(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string][]byte{"malformed": []byte("not a certificate"), "empty": nil} {
		path := filepath.Join(dir, name+".pem")
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal("write invalid fixture")
		}
		if _, err := LoadSourceBridgeRoots(path); err == nil {
			t.Fatalf("invalid CA file %q accepted", name)
		}
	}
	if _, err := LoadSourceBridgeRoots(filepath.Join(dir, "missing.pem")); err == nil {
		t.Fatal("missing CA file accepted")
	}
	if _, err := LoadSourceBridgeRoots("relative-ca.pem"); err == nil {
		t.Fatal("relative CA path accepted")
	}
}
