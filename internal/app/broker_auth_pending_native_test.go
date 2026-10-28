package app

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	bc "project/internal/brokercontract"
)

const nativeAuthPendingCapacity = 128

// TestBrokerAuthPendingNativeOverlap uses a test-only fake Owner grant. It
// checks native plugin admission overlap only; it does not certify real Owner
// authorization, PostgreSQL behavior, device effects, or broker performance.
type nativeAuthBarrier struct {
	token              string
	release            chan struct{}
	reached            chan struct{}
	mu                 sync.Mutex
	started            time.Time
	arrived            int
	active             int
	peak               int
	reachedOnce        sync.Once
	releaseOnce        sync.Once
	released           bool
	extraBeforeRelease int
}

func newNativeAuthBarrier(token string) *nativeAuthBarrier {
	return &nativeAuthBarrier{token: token, release: make(chan struct{}), reached: make(chan struct{})}
}

func (b *nativeAuthBarrier) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/authenticate" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+b.token || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
	if err != nil || bc.ValidateUniqueJSON(body) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var req bc.AuthRequest
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil || dec.Decode(new(any)) != io.EOF || bc.ValidateAuthRequest(req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	if b.arrived == 0 {
		b.started = time.Now()
	}
	b.arrived++
	b.active++
	if b.active > b.peak {
		b.peak = b.active
	}
	if b.arrived == nativeAuthPendingCapacity {
		b.reachedOnce.Do(func() { close(b.reached) })
	}
	gate := b.arrived <= nativeAuthPendingCapacity && !b.released
	if b.arrived > nativeAuthPendingCapacity && !b.released {
		b.extraBeforeRelease++
		b.active--
		b.mu.Unlock()
		http.Error(w, "unexpected extra owner request", http.StatusServiceUnavailable)
		return
	}
	b.mu.Unlock()

	if gate {
		select {
		case <-b.release:
		case <-r.Context().Done():
			b.mu.Lock()
			b.active--
			b.mu.Unlock()
			return
		}
	}
	b.mu.Lock()
	b.active--
	b.mu.Unlock()

	sum := sha256.Sum256([]byte(req.ConnectionNonce))
	grant := bc.Grant{
		Allow:     true,
		Principal: bc.Principal{TenantID: "qa-auth-tenant", DeviceID: "qa-auth-device", Kind: bc.PrincipalDevice},
		Policy: bc.Policy{
			Publish:       []string{"devices/telemetry"},
			MaxPayload:    bc.MaxPayloadBytes,
			MaxQoS:        1,
			RetainProfile: bc.RetainedLiveIngress,
		},
		ConnectionID:   "qa-auth-" + hex.EncodeToString(sum[:12]),
		RouteEpoch:     1,
		PolicyVersion:  "qa-auth-pending-v1",
		MappingVersion: "qa-auth-pending-v1",
		BindingPhase:   "active",
		ExpiresAt:      time.Now().UTC().Add(30 * time.Second),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(grant)
}

type nativeAuthWireClient struct {
	conn net.Conn
	ack  byte
	err  error
}

func TestBrokerAuthPendingNativeOverlap(t *testing.T) {
	if os.Getenv("TEST_MQTT_NATIVE_AUTH_BOUND_E2E") != "1" {
		t.Skip("set TEST_MQTT_NATIVE_AUTH_BOUND_E2E=1 to run isolated native authentication-capacity test")
	}
	root := repoRootForNativeE2E(t)
	mosquittoPath := nativeE2EPath("MOSQUITTO_BIN", "/tmp/tp-mqtt-20261019/build/install/sbin/mosquitto")
	includePath := nativeE2EPath("MOSQUITTO_INCLUDE_DIR", "/tmp/tp-mqtt-20261019/mosquitto/include")
	cjsonPath := nativeE2EPath("CJSON_INCLUDE_DIR", "/opt/homebrew/opt/cjson/include")
	for _, path := range []string{mosquittoPath, filepath.Join(includePath, "mosquitto_broker.h"), cjsonPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required native authentication test dependency unavailable: %s", filepath.Base(path))
		}
	}
	plugin := buildNativeE2EPlugin(t, root, includePath, cjsonPath)
	ownerTokenBytes := make([]byte, 48)
	if _, err := rand.Read(ownerTokenBytes); err != nil {
		t.Fatal("generate test owner token")
	}
	ownerToken := hex.EncodeToString(ownerTokenBytes)
	tokenPath := filepath.Join(t.TempDir(), "owner.token")
	if err := os.WriteFile(tokenPath, []byte(ownerToken), 0o600); err != nil {
		t.Fatal("write private owner token file")
	}

	barrier := newNativeAuthBarrier(ownerToken)
	ownerServer := httptest.NewServer(barrier)
	t.Cleanup(ownerServer.Close)
	addr, brokerLog, stopBroker := startNativeAuthCapacityBroker(t, mosquittoPath, plugin, ownerServer.URL, tokenPath, root)
	clients := make([]nativeAuthWireClient, 0, nativeAuthPendingCapacity+2)
	probeOutcome := "not attempted"
	initialSuccesses := 0
	recoverySuccess := false
	cleanupOnce := sync.Once{}
	cleanup := func() {
		cleanupOnce.Do(func() {
			barrier.releaseNow()
			for _, client := range clients {
				if client.conn != nil {
					_ = client.conn.Close()
				}
			}
			stopBroker()
			ownerServer.Close()
		})
	}
	t.Cleanup(cleanup)
	defer func() {
		arrived, active, peak, elapsed := barrier.snapshot()
		t.Log("scope: test-only fake Owner grant; no real Owner authorization, PostgreSQL, device effect, or performance certification")
		t.Logf("native auth counters: owner_arrived=%d active=%d peak=%d probe=%s pre_release_extra=%d initial_CONNACK_success=%d recovery_success=%t first_auth_to_now=%s", arrived, active, peak, probeOutcome, barrier.extraCount(), initialSuccesses, recoverySuccess, elapsed)
	}()

	const deadlineWindow = 1500 * time.Millisecond
	clients = make([]nativeAuthWireClient, nativeAuthPendingCapacity)
	var dialWG sync.WaitGroup
	for i := range clients {
		dialWG.Add(1)
		go func(i int) {
			defer dialWG.Done()
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err != nil {
				clients[i].err = err
				return
			}
			clients[i].conn = conn
			_ = conn.SetDeadline(time.Now().Add(1900 * time.Millisecond))
			clients[i].err = writeNativeAuthConnect(conn, fmt.Sprintf("qa-auth-pending-%03d", i), "qa-auth-user", "qa-auth-password")
		}(i)
	}
	dialWG.Wait()
	for i := range clients {
		if clients[i].err != nil {
			t.Fatalf("send one of %d initial CONNECT packets: %v", nativeAuthPendingCapacity, clients[i].err)
		}
	}
	select {
	case <-barrier.reached:
	case <-time.After(deadlineWindow):
		arrived, active, peak, elapsed := barrier.snapshot()
		t.Fatalf("owner barrier did not reach configured capacity before deadline: arrived=%d active=%d peak=%d elapsed=%s", arrived, active, peak, elapsed)
	}

	arrived, active, peak, elapsed := barrier.snapshot()
	if arrived != nativeAuthPendingCapacity || active != nativeAuthPendingCapacity || peak != nativeAuthPendingCapacity {
		t.Fatalf("unexpected owner overlap at barrier: arrived=%d active=%d peak=%d", arrived, active, peak)
	}
	if elapsed > deadlineWindow {
		t.Fatalf("initial overlap consumed too much authentication timeout: %s > %s", elapsed, deadlineWindow)
	}

	probeConn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("dial capacity probe: %v", err)
	}
	clients = append(clients, nativeAuthWireClient{conn: probeConn})
	_ = probeConn.SetDeadline(time.Now().Add(250 * time.Millisecond))
	if err := writeNativeAuthConnect(probeConn, "qa-auth-pending-probe", "qa-auth-user", "qa-auth-password"); err != nil {
		t.Fatalf("send capacity probe CONNECT: %v", err)
	}
	probeACK, probeErr := readNativeAuthConnack(probeConn)
	if probeErr != nil {
		if !errors.Is(probeErr, net.ErrClosed) && !errors.Is(probeErr, io.EOF) && !errors.Is(probeErr, syscall.ECONNRESET) {
			if netErr, ok := probeErr.(net.Error); !ok || !netErr.Timeout() {
				probeOutcome = "other-read-error"
				t.Fatalf("capacity probe did not receive a timely denial before release: %v", probeErr)
			}
			probeOutcome = "timeout"
			t.Fatal("capacity probe timed out before plugin returned an authentication denial")
		}
		probeOutcome = "closed-before-CONNACK"
	} else if probeACK == 0 {
		t.Fatal("capacity probe unexpectedly received successful CONNACK")
	} else {
		probeOutcome = fmt.Sprintf("CONNACK-%d", probeACK)
		if probeACK != 5 {
			t.Fatalf("capacity probe received unexpected non-success CONNACK reason=%d; want 5", probeACK)
		}
	}
	arrived, active, peak, elapsed = barrier.snapshot()
	if arrived != nativeAuthPendingCapacity || active != nativeAuthPendingCapacity || peak != nativeAuthPendingCapacity {
		t.Fatalf("capacity probe reached Owner or disturbed held requests: arrived=%d active=%d peak=%d", arrived, active, peak)
	}
	if elapsed > deadlineWindow {
		t.Fatalf("probe exceeded authentication overlap deadline: %s > %s", elapsed, deadlineWindow)
	}

	barrier.releaseNow()
	releasedAt := time.Now()
	firstToRelease := releasedAt.Sub(barrier.firstStarted())
	if firstToRelease > deadlineWindow {
		t.Fatalf("first Owner request to barrier release exceeded deadline: %s > %s", firstToRelease, deadlineWindow)
	}
	for i := range clients[:nativeAuthPendingCapacity] {
		ack, err := readNativeAuthConnack(clients[i].conn)
		if err != nil {
			t.Fatalf("read successful CONNACK %d: %v (broker log %s)", i, err, filepath.Base(brokerLog))
		}
		clients[i].ack = ack
		if ack != 0 {
			t.Fatalf("initial client %d was denied after barrier release: CONNACK reason=%d", i, ack)
		}
		initialSuccesses++
	}

	arrived, active, peak, elapsed = barrier.snapshot()
	if arrived != nativeAuthPendingCapacity || active != 0 || peak != nativeAuthPendingCapacity {
		t.Fatalf("unexpected barrier accounting after release: arrived=%d active=%d peak=%d elapsed=%s", arrived, active, peak, elapsed)
	}

	recoveryConn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("dial post-release recovery client: %v", err)
	}
	clients = append(clients, nativeAuthWireClient{conn: recoveryConn})
	_ = recoveryConn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	if err := writeNativeAuthConnect(recoveryConn, "qa-auth-pending-recovered", "qa-auth-user", "qa-auth-password"); err != nil {
		t.Fatalf("send post-release recovery CONNECT: %v", err)
	}
	recoveryACK, err := readNativeAuthConnack(recoveryConn)
	if err != nil || recoveryACK != 0 {
		t.Fatalf("capacity did not recover for a new client: CONNACK=%d error=%v", recoveryACK, err)
	}
	recoverySuccess = true
	arrived, active, peak, elapsed = barrier.snapshot()
	if arrived != nativeAuthPendingCapacity+1 || active != 0 || peak != nativeAuthPendingCapacity {
		t.Fatalf("recovery request accounting mismatch: arrived=%d active=%d peak=%d elapsed=%s", arrived, active, peak, elapsed)
	}
	t.Logf("native auth overlap verified: owner_arrived=%d barrier_peak=%d active_at_probe=%d probe_denied=true initial_CONNACK_ok=%d recovery_CONNACK_ok=true first_to_release=%s", arrived-1, peak, nativeAuthPendingCapacity, nativeAuthPendingCapacity, firstToRelease)
}

func (b *nativeAuthBarrier) snapshot() (arrived, active, peak int, elapsed time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started.IsZero() {
		elapsed = time.Since(b.started)
	}
	return b.arrived, b.active, b.peak, elapsed
}

func (b *nativeAuthBarrier) releaseNow() {
	b.mu.Lock()
	b.released = true
	b.mu.Unlock()
	b.releaseOnce.Do(func() { close(b.release) })
}

func (b *nativeAuthBarrier) firstStarted() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.started
}

func (b *nativeAuthBarrier) extraCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.extraBeforeRelease
}

func startNativeAuthCapacityBroker(t *testing.T, binary, plugin, ownerURL, tokenPath, root string) (string, string, func()) {
	t.Helper()
	port := freeNativeE2EPort(t)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	dir := t.TempDir()
	configPath, logPath := filepath.Join(dir, "mosquitto.conf"), filepath.Join(dir, "mosquitto.log")
	config := fmt.Sprintf("listener %d 127.0.0.1\nallow_anonymous false\nsys_interval 1\npersistence false\nplugin %s\nlog_dest file %s\nlog_type all\n", port, plugin, logPath)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal("write isolated broker config")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal("open isolated broker log")
	}
	cmd := exec.Command(binary, "-c", configPath)
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"DYLD_LIBRARY_PATH=/tmp/tp-mqtt-20261019/build/install/lib",
		"TP_BROKER_OWNER_URL=" + ownerURL,
		"TP_BROKER_TOKEN_FILE=" + tokenPath,
		"TP_BROKER_CELL=community",
		"TP_BROKER_INSTANCE=node-a",
		"TP_BROKER_MAX_PENDING=128",
		"TP_BROKER_MAX_SESSIONS=256",
		"TP_BROKER_ROUTE_FENCING=0",
	}
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal("start isolated native broker")
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(done)
		_ = logFile.Close()
	}()
	var stopped atomic.Bool
	stop := func() {
		if stopped.Swap(true) || cmd.Process == nil {
			return
		}
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return addr, logPath, stop
		}
		select {
		case <-done:
			_ = logFile.Close()
			t.Fatalf("isolated native broker exited before listening: %v", safeNativeAuthBrokerExit(waitErr))
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	stop()
	t.Fatal("isolated native broker did not listen within startup deadline")
	return "", "", stop
}

func safeNativeAuthBrokerExit(err error) string {
	if err == nil {
		return "unexpected clean exit"
	}
	return "unexpected process exit"
}

func writeNativeAuthConnect(conn net.Conn, clientID, username, password string) error {
	var payload bytes.Buffer
	for _, value := range []string{clientID, username, password} {
		if len(value) > 0xffff {
			return errors.New("CONNECT field too long")
		}
		payload.WriteByte(byte(len(value) >> 8))
		payload.WriteByte(byte(len(value)))
		payload.WriteString(value)
	}
	remaining := 10 + payload.Len()
	packet := []byte{0x10}
	for remaining > 0x7f {
		packet = append(packet, byte(remaining%128)|0x80)
		remaining /= 128
	}
	packet = append(packet, byte(remaining))
	packet = append(packet, 0, 4, 'M', 'Q', 'T', 'T', 4, 0xc2, 0, 30)
	packet = append(packet, payload.Bytes()...)
	n, err := conn.Write(packet)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return io.ErrShortWrite
	}
	return nil
}

func readNativeAuthConnack(conn net.Conn) (byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return 0xff, err
	}
	if header[0] != 0x20 || header[1] != 2 {
		return 0xff, errors.New("unexpected packet while awaiting CONNACK")
	}
	var body [2]byte
	if _, err := io.ReadFull(conn, body[:]); err != nil {
		return 0xff, err
	}
	if body[0] != 0 || body[1] > 5 {
		return 0xff, errors.New("invalid CONNACK flags or reason code")
	}
	return body[1], nil
}
