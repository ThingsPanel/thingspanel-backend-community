package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const frozenNativeDockerImage = "sha256:ac1a075beb345ea8475d385d2b37d0af9a5bde5cbdcc66ae906f0f061b07fa8e"

// startNativeDockerBroker runs the frozen Linux production image with its
// bundled Mosquitto/plugin. Owner traffic crosses a local TLS reverse proxy;
// the image is isolated, read-only, and has no persistent data volume.
func startNativeDockerBroker(t *testing.T, image, ownerURL, tokenPath string) (string, string, func()) {
	t.Helper()
	if image != frozenNativeDockerImage {
		t.Fatalf("TP_NATIVE_E2E_DOCKER_IMAGE must identify the frozen image %s", frozenNativeDockerImage)
	}
	for _, binary := range []string{"docker"} {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("Docker runtime unavailable: %v", err)
		}
	}
	if _, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Fatalf("frozen image unavailable locally: %v", err)
	}

	containerDir := t.TempDir()
	configPath := filepath.Join(containerDir, "mosquitto.conf")
	secretPath := filepath.Join(containerDir, "plugin.token")
	caPath, certPath, keyPath, proxyListener, err := newNativeDockerOwnerTLS(containerDir)
	if err != nil {
		t.Fatalf("prepare strict owner TLS proxy: %v", err)
	}
	secret, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, secret, 0o444); err != nil {
		t.Fatal(err)
	}
	port := freeNativeE2EPort(t)
	config := "listener 1883 0.0.0.0\nallow_anonymous false\nsys_interval 1\npersistence false\nmax_packet_size 2097152\nplugin /mosquitto/plugins/libtp-mosquitto-plugin.so\nlog_dest stdout\nlog_type all\n"
	if err := os.WriteFile(configPath, []byte(config), 0o444); err != nil {
		t.Fatal(err)
	}
	logsDir := filepath.Join(os.TempDir(), "tp-mqtt-20261020")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logsDir, "docker-native-"+uuid.NewString()+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL := "https://host.docker.internal:" + fmt.Sprint(proxyListener.Addr().(*net.TCPAddr).Port)
	containerName := "tp-native-e2e-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	args := []string{
		"run", "-d", "--name", containerName,
		"--read-only", "--user", "1883:1883",
		"--tmpfs", "/mosquitto/data:rw,noexec,nosuid,size=16m,uid=1883,gid=1883",
		"-p", fmt.Sprintf("127.0.0.1:%d:1883", port),
		"-v", configPath + ":/mosquitto/config/mosquitto.conf:ro",
		"-v", secretPath + ":/run/secrets/plugin.token:ro",
		"-v", caPath + ":/run/certs/owner-ca.pem:ro",
		"-e", "TP_BROKER_OWNER_URL=" + proxyURL,
		"-e", "TP_BROKER_TOKEN_FILE=/run/secrets/plugin.token",
		"-e", "TP_BROKER_CELL=" + nativeE2ECell,
		"-e", "TP_BROKER_INSTANCE=" + nativeE2ENode,
		"-e", "TP_BROKER_MAX_PENDING=32",
		"-e", "TP_BROKER_MAX_SESSIONS=64",
		"-e", "TP_BROKER_ROUTE_FENCING=1",
		"-e", "SSL_CERT_FILE=/run/certs/owner-ca.pem",
		image, "broker",
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start isolated frozen broker image: %v: %s", err, strings.TrimSpace(string(out)))
	}
	proxyTarget, err := url.Parse(ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(proxyTarget)
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxyServer := &http.Server{Handler: proxy, ReadHeaderTimeout: 2 * time.Second}
	proxyCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		_ = proxyServer.Serve(tls.NewListener(proxyListener, &tls.Config{Certificates: []tls.Certificate{proxyCert}, MinVersion: tls.VersionTLS12}))
	}()

	logsCtx, cancelLogs := context.WithCancel(context.Background())
	logsCmd := exec.CommandContext(logsCtx, "docker", "logs", "-f", containerName)
	logsCmd.Stdout, logsCmd.Stderr = logFile, logFile
	var logsWG sync.WaitGroup
	logsWG.Add(1)
	go func() { defer logsWG.Done(); _ = logsCmd.Run() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancelLogs()
			logsWG.Wait()
			if snapshot, snapshotErr := exec.Command("docker", "logs", containerName).CombinedOutput(); snapshotErr == nil && len(snapshot) > 0 {
				_, _ = logFile.Write(snapshot)
			}
			_ = exec.Command("docker", "rm", "-f", containerName).Run()
			_ = proxyServer.Shutdown(context.Background())
			_ = proxyListener.Close()
			<-proxyDone
			_ = logFile.Close()
			if t.Failed() {
				t.Logf("frozen Docker broker log (%s): %s", logPath, nativeE2ETail(logPath))
			}
		})
	}
	t.Cleanup(stop)

	waitNativeE2E(t, 12*time.Second, "frozen Linux Docker Mosquitto listener", func() bool {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = connection.Close()
		return true
	})
	return fmt.Sprintf("tcp://127.0.0.1:%d", port), logPath, stop
}

func newNativeDockerOwnerTLS(dir string) (caPath, certPath, keyPath string, listener net.Listener, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", nil, err
	}
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ThingsPanel Native E2E QA CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(30 * time.Minute), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return "", "", "", nil, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", "", nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return "", "", "", nil, err
	}
	leafTemplate := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "host.docker.internal"}, DNSNames: []string{"host.docker.internal", "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(30 * time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return "", "", "", nil, err
	}
	caPath, certPath, keyPath = filepath.Join(dir, "owner-ca.pem"), filepath.Join(dir, "owner-proxy.pem"), filepath.Join(dir, "owner-proxy-key.pem")
	write := func(path string, body []byte) error { return os.WriteFile(path, body, 0o444) }
	if err := write(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})); err != nil {
		return "", "", "", nil, err
	}
	if err := write(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})); err != nil {
		return "", "", "", nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return "", "", "", nil, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return "", "", "", nil, err
	}
	listener, err = net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return "", "", "", nil, err
	}
	return caPath, certPath, keyPath, listener, nil
}
