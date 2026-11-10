package smtp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/notification-adapters/sdk"
)

func TestSMTPImplicitTLSAcceptedAndPayloadPreserved(t *testing.T) {
	fixture := startSMTP(t, true, "accept")
	provider := newFixtureProvider("localhost:"+fixture.port, fixture.roots)
	res, err := provider.Send(context.Background(), smtpRequest("localhost", fixture.port, true))
	if err != nil || res.Outcome != "accepted" || res.ReceiptExpected || fixture.connections.Load() != 1 || fixture.dataCount.Load() != 1 {
		t.Fatalf("result=%+v err=%v connections=%d data=%d", res, err, fixture.connections.Load(), fixture.dataCount.Load())
	}
	msg := fixture.message()
	parsed, err := mail.ReadMessage(strings.NewReader(msg))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Header.Get("From") != "<noreply@example.com>" || parsed.Header.Get("To") != "<target@example.com>" || parsed.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
		t.Fatalf("headers=%v", parsed.Header)
	}
	body, err := io.ReadAll(parsed.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil || string(decoded) != "line1\r\n.line2 & text" {
		t.Fatalf("body=%q err=%v", decoded, err)
	}
}

func TestSMTPRequiresStartTLSWhenImplicitSSLFalse(t *testing.T) {
	fixture := startSMTP(t, false, "accept")
	provider := newFixtureProvider("localhost:"+fixture.port, fixture.roots)
	res, err := provider.Send(context.Background(), smtpRequest("localhost", fixture.port, false))
	if err != nil || res.Outcome != "accepted" || fixture.connections.Load() != 1 || fixture.dataCount.Load() != 1 {
		t.Fatalf("result=%+v err=%v", res, err)
	}
}

func TestSMTPExplicitRecipientRejectionHappensBeforeDATA(t *testing.T) {
	fixture := startSMTP(t, true, "reject-recipient")
	provider := newFixtureProvider("localhost:"+fixture.port, fixture.roots)
	res, err := provider.Send(context.Background(), smtpRequest("localhost", fixture.port, true))
	if err != nil || res.Outcome != "rejected" || res.Error == nil || res.Error.Retryable || fixture.dataCount.Load() != 0 || fixture.connections.Load() != 1 {
		t.Fatalf("result=%+v err=%v data=%d", res, err, fixture.dataCount.Load())
	}
}

func TestSMTPConnectionLossAfterDATAIsUnknownAndNeverRetried(t *testing.T) {
	fixture := startSMTP(t, true, "drop-after-data")
	provider := newFixtureProvider("localhost:"+fixture.port, fixture.roots)
	res, err := provider.Send(context.Background(), smtpRequest("localhost", fixture.port, true))
	if err != nil || res.Outcome != "unknown" || res.Error == nil || res.Error.Retryable || fixture.connections.Load() != 1 || fixture.dataCount.Load() != 1 {
		t.Fatalf("result=%+v err=%v connections=%d data=%d", res, err, fixture.connections.Load(), fixture.dataCount.Load())
	}
}

func TestSMTPInvalidAddressAndUnapprovedHostDoNotDial(t *testing.T) {
	fixture := startSMTP(t, true, "accept")
	provider := newFixtureProvider("localhost:"+fixture.port, fixture.roots)
	req := smtpRequest("localhost", fixture.port, true)
	req.Recipient.Address = "victim@example.com\r\nBcc: evil@example.com"
	res, _ := provider.Send(context.Background(), req)
	if res.Outcome != "rejected" || fixture.connections.Load() != 0 {
		t.Fatalf("result=%+v connections=%d", res, fixture.connections.Load())
	}
	req = smtpRequest("127.0.0.1", fixture.port, true)
	res, _ = provider.Send(context.Background(), req)
	if res.Outcome != "rejected" || fixture.connections.Load() != 0 {
		t.Fatalf("unapproved host reached network: %+v", res)
	}
}

type smtpFixture struct {
	port        string
	roots       *x509.CertPool
	connections atomic.Int32
	dataCount   atomic.Int32
	messageMu   chan string
}

func (f *smtpFixture) message() string {
	select {
	case value := <-f.messageMu:
		return value
	default:
		return ""
	}
}

func startSMTP(t *testing.T, implicit bool, mode string) *smtpFixture {
	t.Helper()
	cert, roots := fixtureCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	f := &smtpFixture{port: port, roots: roots, messageMu: make(chan string, 1)}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			f.connections.Add(1)
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				if implicit {
					c = tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
					if err := c.(*tls.Conn).Handshake(); err != nil {
						return
					}
				}
				serveSMTP(c, cert, f, mode, implicit)
			}(conn)
		}
	}()
	t.Cleanup(func() { _ = listener.Close() })
	return f
}

func serveSMTP(conn net.Conn, cert tls.Certificate, f *smtpFixture, mode string, implicit bool) {
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	send := func(s string) bool {
		_, err := io.WriteString(w, s+"\r\n")
		if err == nil {
			err = w.Flush()
		}
		return err == nil
	}
	if !send("220 fixture ESMTP") {
		return
	}
	tlsUp := implicit
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO ") || strings.HasPrefix(upper, "HELO "):
			if !tlsUp && !implicit {
				if !send("250-fixture") || !send("250-STARTTLS") || !send("250 AUTH PLAIN") {
					return
				}
			} else if !send("250-fixture") || !send("250 AUTH PLAIN") {
				return
			}
		case upper == "STARTTLS":
			if tlsUp {
				_ = send("454 already TLS")
				return
			}
			if !send("220 begin TLS") {
				return
			}
			secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
			if secure.Handshake() != nil {
				return
			}
			conn = secure
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
			tlsUp = true
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			if strings.TrimSpace(strings.TrimPrefix(line, "AUTH PLAIN")) == "" {
				if !send("334 ") {
					return
				}
				_, err = r.ReadString('\n')
				if err != nil {
					return
				}
			}
			if !send("235 authenticated") {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			if !send("250 sender ok") {
				return
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			if mode == "reject-recipient" {
				_ = send("550 rejected")
				return
			}
			if !send("250 recipient ok") {
				return
			}
		case upper == "DATA":
			if !send("354 end with dot") {
				return
			}
			f.dataCount.Add(1)
			var body strings.Builder
			for {
				part, e := r.ReadString('\n')
				if e != nil {
					return
				}
				part = strings.TrimRight(part, "\r\n")
				if part == "." {
					break
				}
				if strings.HasPrefix(part, "..") {
					part = part[1:]
				}
				body.WriteString(part)
				body.WriteString("\r\n")
			}
			select {
			case f.messageMu <- body.String():
			default:
			}
			if mode == "drop-after-data" {
				return
			}
			_ = send("250 queued")
			return
		case upper == "QUIT":
			_ = send("221 bye")
			return
		default:
			_ = send("500 unknown command")
			return
		}
	}
}

func fixtureCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsed)
	return cert, roots
}

func smtpRequest(host, port string, ssl bool) sdk.SendRequest {
	return sdk.SendRequest{DeliveryID: "smtp-delivery", Channel: "email", Recipient: sdk.Recipient{Kind: "email", Address: "target@example.com"}, Content: sdk.Content{Kind: "text", Title: "test subject", Text: "line1\r\n.line2 & text"}, Config: map[string]any{"host": host, "port": mustInt(port), "from_email": "noreply@example.com", "from_password": "unit-secret", "ssl": ssl}}
}
func mustInt(s string) int { n, _ := strconv.Atoi(s); return n }
