package service

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"project/internal/dal"
	"project/internal/model"
	"project/pkg/global"

	"github.com/google/uuid"
)

// TestNativeE2E02RelayWorkerReceiptFaultMatrix is an opt-in, one-scenario
// two-database exercise. It combines a source-network outage, durable Core
// acceptance with a lost ACK, relay reconstruction, native worker dispatch,
// and a purpose-authenticated fixture receipt. It never calls a real provider.
func TestNativeE2E02RelayWorkerReceiptFaultMatrix(t *testing.T) {
	if os.Getenv("NOTIFICATION_NATIVE_E2E") != "1" {
		t.Skip("set NOTIFICATION_NATIVE_E2E=1 to run the local two-database integration")
	}
	if os.Getenv("NOTIFICATION_TEST_DSN") != nativeE2EDSN {
		t.Fatal("native integration requires the approved isolated PostgreSQL fixture")
	}
	for _, endpoint := range []struct{ raw, port string }{{nativeE2ECoreURL, "19404"}, {nativeE2ESidecarURL, "19583"}} {
		parsed, err := url.Parse(endpoint.raw)
		if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() != endpoint.port {
			t.Fatal("native integration endpoint is outside the approved local fixture")
		}
	}

	initialSends := nativeE2EFixtureSendCalls(t)
	db := openNativeSourceE2EPG(t) // Creates and cleans only one t07native_* schema.
	target, _ := url.Parse(nativeE2ECoreURL)
	var mu sync.Mutex
	mode := "down" // down -> drop first durable 202 -> pass replay
	droppedDurableACK := false
	var eventStatuses []int
	var eventBodies [][]byte
	var eventKeys []string
	var acceptedReplies []sourceAccepted
	proxyTransport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}
	t.Cleanup(proxyTransport.CloseIdleConnections)
	reverseProxy := httputil.NewSingleHostReverseProxy(target)
	proxyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sourceEventPath || r.Method != http.MethodPost {
			reverseProxy.ServeHTTP(w, r)
			return
		}
		requestBody, err := io.ReadAll(io.LimitReader(r.Body, maxSourceBody+1))
		_ = r.Body.Close()
		if err != nil || len(requestBody) == 0 || len(requestBody) > maxSourceBody {
			http.Error(w, "source request invalid", http.StatusBadRequest)
			return
		}
		mu.Lock()
		currentMode := mode
		eventBodies = append(eventBodies, append([]byte(nil), requestBody...))
		eventKeys = append(eventKeys, r.Header.Get("Idempotency-Key"))
		mu.Unlock()
		if currentMode == "down" {
			mu.Lock()
			eventStatuses = append(eventStatuses, http.StatusBadGateway)
			mu.Unlock()
			http.Error(w, "fixture network outage", http.StatusBadGateway)
			return
		}
		upstreamURL := *target
		upstreamURL.Path = r.URL.Path
		upstreamURL.RawPath = r.URL.RawPath
		upstreamURL.RawQuery = r.URL.RawQuery
		upstreamRequest := r.Clone(r.Context())
		upstreamRequest.URL = &upstreamURL
		upstreamRequest.Host = target.Host
		upstreamRequest.RequestURI = ""
		upstreamRequest.Body = io.NopCloser(bytes.NewReader(requestBody))
		upstreamRequest.ContentLength = int64(len(requestBody))
		response, err := proxyTransport.RoundTrip(upstreamRequest)
		if err != nil {
			http.Error(w, "native core unavailable", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBody+1))
		if err != nil || len(body) > maxSourceBody {
			http.Error(w, "native response invalid", http.StatusBadGateway)
			return
		}
		mu.Lock()
		eventStatuses = append(eventStatuses, response.StatusCode)
		if response.StatusCode == http.StatusAccepted {
			var envelope sourceEnvelope[sourceAccepted]
			if json.Unmarshal(body, &envelope) != nil || envelope.Code != 200 || !envelope.Data.Accepted {
				mu.Unlock()
				t.Error("Core 202 did not contain a durable accepted source intent")
				http.Error(w, "invalid native acceptance", http.StatusBadGateway)
				return
			}
			acceptedReplies = append(acceptedReplies, envelope.Data)
		}
		shouldDrop := currentMode == "drop" && !droppedDurableACK && response.StatusCode == http.StatusAccepted
		if shouldDrop {
			droppedDurableACK = true
			mode = "pass"
		}
		mu.Unlock()
		if shouldDrop {
			// Core already committed 202. Cut only the response to model a lost ACK.
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("TLS proxy cannot inject a lost acknowledgement")
				return
			}
			conn, _, err := hijacker.Hijack()
			if err != nil {
				t.Error("could not drop the first durable acknowledgement")
				return
			}
			partial := body[:len(body)/2]
			_, _ = fmt.Fprintf(conn, "HTTP/1.1 202 Accepted\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n", len(body)+32)
			_, _ = conn.Write(partial)
			_ = conn.Close()
			return
		}
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(proxyServer.Close)
	roots := x509.NewCertPool()
	roots.AddCert(proxyServer.Certificate())
	client := proxyServer.Client()
	client.Timeout = 5 * time.Second

	adminJWT := nativeE2EAdminJWT(t)
	pluginID := nativeE2EFindSMTPPlugin(t, client, proxyServer.URL, adminJWT)
	instanceID, _ := nativeE2ECreateSMTPInstance(t, client, proxyServer.URL, adminJWT, pluginID)
	groupID, revision := nativeE2ECreateEmailGroup(t, client, proxyServer.URL, adminJWT, instanceID)
	prefix := "e2e02-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	legacyGroupID := uuid.NewString() // The legacy route schema caps this identifier at 36 characters.
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO notification_groups (id, name, notification_type, status, notification_config, tenant_id, created_at, updated_at) VALUES (?, ?, 'EMAIL', 'OPEN', ?, ?, ?, ?)`, legacyGroupID, "E2E-02 source group", `{"EMAIL":"ops@example.test"}`, nativeE2ETenant, now, now).Error; err != nil {
		t.Fatal("create isolated legacy source group")
	}
	if err := db.Exec(`INSERT INTO notification_services_config (id, config, notice_type, status) VALUES (?, ?, 'EMAIL', 'OPEN')`, prefix+"-email-config", `{"host":"smtp.example.test","port":465,"from_password":"fixture-only-source-password","from_email":"sender@example.test","ssl":true}`).Error; err != nil {
		t.Fatal("create isolated legacy email identity")
	}
	projection := SourceGroupProjectionRequest{SourceDeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroupID: legacyGroupID, NotificationGroupID: groupID, GroupRevision: revision}
	legacyGroup, err := dal.GetNotificationGroupByTenantID(legacyGroupID, nativeE2ETenant)
	if err != nil {
		t.Fatal("read isolated legacy group")
	}
	makeChecker := func() *EmailSourceCompatibilityChecker {
		checker, err := newEmailSourceCompatibilityChecker(proxyServer.URL, nativeE2EProjToken, roots)
		if err != nil {
			t.Fatal("construct TLS-pinned source compatibility checker")
		}
		return checker
	}
	checker := makeChecker()
	snapshot, err := checker.fetchSnapshot(context.Background(), projection)
	if err != nil {
		checker.Close()
		t.Fatal("native group snapshot unavailable")
	}
	recipients, err := legacyEmailRecipients(legacyGroup)
	if err != nil {
		checker.Close()
		t.Fatal("legacy recipient set invalid")
	}
	legacyConfig, enabled, err := checker.legacyConfig(context.Background())
	if err != nil || !enabled || compareSourceEmailProjection(snapshot, recipients, legacyConfig) != nil {
		checker.Close()
		t.Fatal("native email projection does not match source configuration")
	}
	if err := checker.CheckEncoreCompatibility(context.Background(), legacyGroup, projection); err != nil {
		checker.Close()
		t.Fatal("source compatibility check failed")
	}
	newBridge := func(checker *EmailSourceCompatibilityChecker) *SourceBridge {
		bridge, err := newSourceBridge(SourceBridgeConfig{Enabled: true, BaseURL: proxyServer.URL, DeploymentID: nativeE2EDeployment, SourceBearerToken: nativeE2ESourceToken, ProjectionBearerToken: nativeE2EProjToken, RequestTimeout: 5 * time.Second}, checker, roots)
		if err != nil {
			checker.Close()
			t.Fatal("construct TLS-pinned source relay")
		}
		t.Cleanup(bridge.Close)
		return bridge
	}
	bridge := newBridge(checker)
	projectionKey := prefix + "-projection"
	if err := bridge.SwitchToEncore(context.Background(), projection, projectionKey, 0); err != nil {
		t.Fatal("switch only the verified source route to Encore")
	}
	routeKey := dal.SourceRouteKey{DeploymentID: nativeE2EDeployment, TenantID: nativeE2ETenant, LegacyGroup: legacyGroupID}
	eventID, actionID := uuid.NewString(), uuid.NewString()
	occurred := time.Now().UTC().Truncate(time.Microsecond)
	alarm := &model.AlarmInfo{ID: eventID, AlarmConfigID: prefix + "-alarm-config", Name: "E2E-02 fault matrix", AlarmTime: occurred, ProcessingResult: "UND", TenantID: nativeE2ETenant}
	route, err := dal.SaveAlarmInfoWithSource(context.Background(), alarm, routeKey, func(snapshot dal.SourceRouteSnapshot) (*dal.SourceOutboxRecord, error) {
		return bridge.buildOutbox(snapshot, nativeE2ETenant, legacyGroupID, eventID, actionID, occurred, "E2E-02 fault matrix", "fixture delivery for relay/worker/receipt recovery", `{"scenario":"E2E-02"}`)
	})
	if err != nil || route.Engine != "encore" || route.GroupRevision != revision {
		t.Fatalf("source/outbox was not atomically assigned to the expected native revision: engine=%q revision=%d err=%v", route.Engine, route.GroupRevision, err)
	}
	var outbox dal.SourceOutboxRecord
	if err := global.DB.Where("source_event_id = ? AND source_action_id = ?", eventID, actionID).Take(&outbox).Error; err != nil {
		t.Fatal("read committed source outbox")
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("initial offline relay cycle")
	}
	var state string
	var attempts int
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts); err != nil || state != "retry_wait" || attempts != 1 {
		t.Fatalf("offline relay did not retain the outbox: state=%q attempts=%d err=%v", state, attempts, err)
	}
	if err := global.DB.Exec(`UPDATE notification_source_outbox SET next_attempt_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), outbox.ID).Error; err != nil {
		t.Fatal("make retained source event eligible")
	}
	bridge.Close() // Simulates relay process exit; the SQL outbox remains the source of truth.
	checker.Close()

	mu.Lock()
	mode = "drop"
	mu.Unlock()
	bridge = newBridge(makeChecker())
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("relay pass with intentionally lost durable ACK")
	}
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts); err != nil || state != "retry_wait" || attempts != 2 {
		t.Fatalf("lost ACK was not left retryable: state=%q attempts=%d err=%v", state, attempts, err)
	}
	if err := global.DB.Exec(`UPDATE notification_source_outbox SET next_attempt_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second), outbox.ID).Error; err != nil {
		t.Fatal("make lost-ACK retry immediately eligible")
	}
	bridge.Close()
	if len(acceptedReplies) != 1 || acceptedReplies[0].NotificationID == "" || len(acceptedReplies[0].DeliveryIDs) != 1 {
		t.Fatalf("Core did not retain one accepted intent before process fault: accepted_replies=%d", len(acceptedReplies))
	}
	acceptedBeforeRestart := acceptedReplies[0]
	deadline := time.Now().Add(15 * time.Second)
	dispatchBeforeRestart, deliveryBeforeRestart := "", ""
	for time.Now().Before(deadline) {
		dispatchBeforeRestart, deliveryBeforeRestart = nativeE2EReadDelivery(client, proxyServer.URL, adminJWT, acceptedBeforeRestart.NotificationID, acceptedBeforeRestart.DeliveryIDs[0])
		if dispatchBeforeRestart == "accepted" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if dispatchBeforeRestart != "accepted" || deliveryBeforeRestart != "unsupported" {
		t.Fatalf("worker had not durably accepted the fixture send before Core crash: dispatch=%q delivery=%q", dispatchBeforeRestart, deliveryBeforeRestart)
	}
	if calls := nativeE2EFixtureSendCalls(t); calls != initialSends+1 {
		t.Fatalf("expected one fake SMTP send before Core crash, got %d expected %d", calls, initialSends+1)
	}
	counterBeforeRestart := nativeE2EReadFixtureCounter(t)
	if counterBeforeRestart.SendCalls != initialSends+1 || counterBeforeRestart.CallsByDelivery[acceptedBeforeRestart.DeliveryIDs[0]] != 1 {
		t.Fatalf("fixture per-delivery counter before Core crash is wrong: %+v", counterBeforeRestart)
	}

	mu.Lock()
	mode = "pass"
	mu.Unlock()
	coreRestart := nativeE2EKillAndRecoverCore(t, counterBeforeRestart)
	if calls := nativeE2EFixtureSendCalls(t); calls != initialSends+1 {
		t.Fatalf("Core restart caused another fixture send: calls=%d expected=%d", calls, initialSends+1)
	}
	bridge = newBridge(makeChecker()) // New relay object models a process restart.
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("reconstructed relay could not retry durable intent")
	}
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts); err != nil || state != "handed_off" || attempts != 3 {
		t.Fatalf("restarted relay did not record handoff: state=%q attempts=%d err=%v", state, attempts, err)
	}
	mu.Lock()
	if !droppedDurableACK || len(eventStatuses) != 3 || eventStatuses[0] != http.StatusBadGateway || eventStatuses[1] != http.StatusAccepted || eventStatuses[2] != http.StatusAccepted || len(eventBodies) != 3 || len(eventKeys) != 3 || string(eventBodies[0]) != string(outbox.RequestBody) || string(eventBodies[1]) != string(outbox.RequestBody) || string(eventBodies[2]) != string(outbox.RequestBody) || eventKeys[0] != outbox.IdempotencyKey || eventKeys[1] != outbox.IdempotencyKey || eventKeys[2] != outbox.IdempotencyKey || len(acceptedReplies) != 2 || acceptedReplies[0].NotificationID == "" || acceptedReplies[0].NotificationID != acceptedReplies[1].NotificationID || len(acceptedReplies[0].DeliveryIDs) != 1 || len(acceptedReplies[1].DeliveryIDs) != 1 || acceptedReplies[0].DeliveryIDs[0] != acceptedReplies[1].DeliveryIDs[0] {
		mu.Unlock()
		t.Fatal("relay did not preserve one frozen event across outage, lost ACK, and restart")
	}
	mu.Unlock()
	accepted := acceptedReplies[0]
	if accepted.IntakeStatus != "ready" {
		t.Fatalf("source event unexpectedly blocked: %s", accepted.IntakeStatus)
	}
	deliveryDeadline := time.Now().Add(15 * time.Second)
	dispatch, deliveryStatus := "", ""
	for time.Now().Before(deliveryDeadline) {
		dispatch, deliveryStatus = nativeE2EReadDelivery(client, proxyServer.URL, adminJWT, accepted.NotificationID, accepted.DeliveryIDs[0])
		if dispatch == "accepted" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if dispatch != "accepted" || deliveryStatus != "unsupported" {
		t.Fatalf("native worker did not accept exactly one fixture send: dispatch=%q delivery=%q", dispatch, deliveryStatus)
	}
	if calls := nativeE2EFixtureSendCalls(t); calls != initialSends+1 {
		t.Fatalf("worker send count after one logical source event=%d, expected %d", calls, initialSends+1)
	}

	var receiptCredential struct {
		Credential string `json:"credential"`
	}
	credentialRequest, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/api/v2/notification-instances/"+url.PathEscape(instanceID)+"/receipt-credential", nil)
	if err != nil {
		t.Fatal("build receipt credential request")
	}
	credentialRequest.Header.Set("Authorization", "Bearer "+adminJWT)
	credentialRequest.Header.Set("Idempotency-Key", prefix+"-receipt-credential")
	credentialResponse, err := client.Do(credentialRequest)
	if err != nil {
		t.Fatal("request instance-scoped receipt credential")
	}
	if credentialResponse.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(credentialResponse.Body, 16<<10)).Decode(&receiptCredential) != nil {
		credentialResponse.Body.Close()
		t.Fatalf("create instance-scoped receipt credential failed with HTTP %d", credentialResponse.StatusCode)
	}
	credentialResponse.Body.Close()
	if receiptCredential.Credential == "" {
		t.Fatal("receipt credential response was empty")
	}
	receiptEvent := map[string]any{"event_id": prefix + "-fixture-receipt", "provider_message_id": "named-manual-fixture-receipt", "delivery_id": accepted.DeliveryIDs[0], "status": "delivered", "occurred_at": time.Now().UTC().Format(time.RFC3339)}
	postReceipt := func(body map[string]any, wantStatus int) {
		t.Helper()
		wire, err := json.Marshal(body)
		if err != nil {
			t.Fatal("encode fixture receipt")
		}
		req, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/api/v1/notification-instances/"+url.PathEscape(instanceID)+"/receipts", bytes.NewReader(wire))
		if err != nil {
			t.Fatal("build receipt callback request")
		}
		req.Header.Set("Authorization", "Bearer "+receiptCredential.Credential)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal("post receipt to native Core")
		}
		defer res.Body.Close()
		if res.StatusCode != wantStatus {
			t.Fatalf("fixture receipt status=%d expected=%d", res.StatusCode, wantStatus)
		}
	}
	postReceipt(receiptEvent, http.StatusOK)
	postReceipt(receiptEvent, http.StatusOK) // Same event/digest is durable and idempotent.
	conflict := make(map[string]any, len(receiptEvent))
	for key, value := range receiptEvent {
		conflict[key] = value
	}
	conflict["status"] = "failed"
	postReceipt(conflict, http.StatusConflict) // Conflicting digest cannot downgrade the delivered fact.
	dispatch, deliveryStatus = nativeE2EReadDelivery(client, proxyServer.URL, adminJWT, accepted.NotificationID, accepted.DeliveryIDs[0])
	if dispatch != "accepted" || deliveryStatus != "delivered" {
		t.Fatalf("trusted fixture receipt did not persist terminal status: dispatch=%q delivery=%q", dispatch, deliveryStatus)
	}
	if calls := nativeE2EFixtureSendCalls(t); calls != initialSends+1 {
		t.Fatalf("receipt or duplicate replay caused another fixture send: calls=%d expected=%d", calls, initialSends+1)
	}
	if err := bridge.RunOnce(context.Background(), 10, 3); err != nil {
		t.Fatal("repeat relay poll after receipt")
	}
	if err := global.DB.Raw(`SELECT state, attempts FROM notification_source_outbox WHERE id = ?`, outbox.ID).Row().Scan(&state, &attempts); err != nil || state != "handed_off" || attempts != 3 {
		t.Fatalf("handed-off source row changed after replay: state=%q attempts=%d err=%v", state, attempts, err)
	}
	finalFixtureCounter := nativeE2EReadFixtureCounter(t)
	if finalFixtureCounter.SendCalls != initialSends+1 || finalFixtureCounter.CallsByDelivery[accepted.DeliveryIDs[0]] != 1 {
		t.Fatalf("final worker/relay replay changed exact fixture delivery count: %+v", finalFixtureCounter)
	}
	var outboxAfter dal.SourceOutboxRecord
	if err := global.DB.Where("id = ?", outbox.ID).Take(&outboxAfter).Error; err != nil || outboxAfter.State != "handed_off" || outboxAfter.IdempotencyKey != outbox.IdempotencyKey || outboxAfter.BodySHA256 != outbox.BodySHA256 || !bytes.Equal(outboxAfter.RequestBody, outbox.RequestBody) {
		t.Fatalf("source event identity/history changed after handoff: state=%q err=%v", outboxAfter.State, err)
	}
	var durableInbox int
	if err := readNativeCoreCount("SELECT count(*) FROM notification_receipt_inbox WHERE tenant_id='tenant-native' AND instance_id='"+sqlQuoteValue(instanceID)+"' AND event_id='"+sqlQuoteValue(receiptEvent["event_id"].(string))+"'", &durableInbox); err != nil || durableInbox != 1 {
		t.Fatalf("receipt inbox durability count=%d err=%v", durableInbox, err)
	}
	var coreRequests, coreDeliveries, sourceIdempotency int
	if err := readNativeCoreCount("SELECT count(*) FROM notification_requests WHERE tenant_id='tenant-native' AND id='"+sqlQuoteValue(accepted.NotificationID)+"'", &coreRequests); err != nil || coreRequests != 1 {
		t.Fatalf("Core request rows=%d err=%v", coreRequests, err)
	}
	if err := readNativeCoreCount("SELECT count(*) FROM notification_deliveries WHERE tenant_id='tenant-native' AND id='"+sqlQuoteValue(accepted.DeliveryIDs[0])+"'", &coreDeliveries); err != nil || coreDeliveries != 1 {
		t.Fatalf("Core delivery rows=%d err=%v", coreDeliveries, err)
	}
	if err := readNativeCoreCount("SELECT count(*) FROM notification_idempotency WHERE tenant_id='tenant-native' AND operation='source' AND idempotency_key='"+sqlQuoteValue(outbox.IdempotencyKey)+"' AND resource_id='"+sqlQuoteValue(accepted.NotificationID)+"'", &sourceIdempotency); err != nil || sourceIdempotency != 1 {
		t.Fatalf("Core source idempotency rows=%d err=%v", sourceIdempotency, err)
	}
	t.Logf("E2E-02 fixture passed: source_outbox=handed_off attempts=3 relay_http=[502,202,202] stable_body_key_ids=1 core_sigkill_restart=%s worker_send_delta=1 manual_fixture_receipt=delivered conflict=409 real_provider=false", coreRestart)
}

const nativeE2ECoreContainer = "tp-notification-core-native"
const nativeE2ESidecarContainer = "tp-notification-native-fixture"
const nativeE2EFixtureImage = "tp-notification-core:g2-checkpoint-9"
const nativeE2EFixtureServerMount = "/Users/junhong/Downloads/code/notification-workspaces/native-fixture/fixture-server"
const nativeE2EFixtureCounterMount = "/Users/junhong/Downloads/code/notification-workspaces/native-fixture/fault-counters"
const nativeE2EFixtureCounterFile = "/fixture-data/fault-calls.json"

type nativeE2EFixtureCounter struct {
	Durable         bool           `json:"durable"`
	SendCalls       int            `json:"sendCalls"`
	CallsByDelivery map[string]int `json:"callsByDelivery"`
}

func nativeE2EReadFixtureCounter(t *testing.T) nativeE2EFixtureCounter {
	t.Helper()
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get(nativeE2ESidecarURL + "/__fixture/calls")
	if err != nil {
		t.Fatal("read durable per-delivery fixture counter")
	}
	defer response.Body.Close()
	var counter nativeE2EFixtureCounter
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&counter) != nil || !counter.Durable {
		t.Fatalf("durable fixture counter unavailable: HTTP %d", response.StatusCode)
	}
	return counter
}

func nativeE2EKillAndRecoverCore(t *testing.T, counterBefore nativeE2EFixtureCounter) string {
	t.Helper()
	before, err := nativeE2EInspectCore(t)
	if err != nil || before.name != "/"+nativeE2ECoreContainer || before.image != nativeE2EFixtureImage || !before.running {
		t.Fatalf("refusing to restart an unexpected native Core container: state=%+v err=%v", before, err)
	}
	if err := nativeE2ECheckSidecarConfig(t, before.id); err != nil {
		t.Fatalf("refusing to recreate an unexpected fixture sidecar: %v", err)
	}
	t.Cleanup(func() {
		state, inspectErr := nativeE2EInspectCore(t)
		if inspectErr != nil {
			t.Errorf("could not verify native Core container during cleanup: %v", inspectErr)
			return
		}
		if state.name != "/"+nativeE2ECoreContainer || state.image != nativeE2EFixtureImage || state.id != before.id {
			t.Errorf("native Core container identity changed during test: %+v", state)
			return
		}
		if !state.running {
			if _, startErr := nativeE2EDocker(t, "start", nativeE2ECoreContainer); startErr != nil {
				t.Errorf("failed to restore native Core container after test: %v", startErr)
				return
			}
		}
		if err := nativeE2EWaitCoreHealth(t, 45*time.Second); err != nil {
			t.Errorf("native Core was not healthy after test cleanup: %v", err)
		}
		if err := nativeE2EEnsureSidecar(t, before.id, counterBefore, false); err != nil {
			t.Errorf("fixture sidecar was not restored during test cleanup: %v", err)
		}
	})
	if _, err := nativeE2EDocker(t, "kill", "--signal=KILL", nativeE2ECoreContainer); err != nil {
		t.Fatalf("SIGKILL native Core container: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	var after nativeE2EContainerState
	for time.Now().Before(deadline) {
		after, err = nativeE2EInspectCore(t)
		if err == nil && after.name == before.name && after.image == before.image && after.id == before.id && (!after.running || after.startedAt != before.startedAt) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("could not inspect native Core after SIGKILL: %v", err)
	}
	if !after.running {
		if _, err := nativeE2EDocker(t, "start", nativeE2ECoreContainer); err != nil {
			t.Fatalf("start native Core after SIGKILL: %v", err)
		}
	}
	if err := nativeE2EWaitCoreHealth(t, 45*time.Second); err != nil {
		t.Fatalf("native Core did not return HTTP health 200 after SIGKILL: %v", err)
	}
	after, err = nativeE2EInspectCore(t)
	if err != nil || after.name != before.name || after.image != before.image || after.id != before.id || !after.running || after.startedAt == before.startedAt {
		t.Fatalf("native Core recovery did not restart the same image/container: before=%+v after=%+v err=%v", before, after, err)
	}
	if err := nativeE2EEnsureSidecar(t, before.id, counterBefore, true); err != nil {
		t.Fatalf("recreate fixture sidecar in Core's recovered network namespace: %v", err)
	}
	return fmt.Sprintf("container=%s same_id=true restart_count=%d->%d", nativeE2ECoreContainer, before.restartCount, after.restartCount)
}

func nativeE2ECheckSidecarConfig(t *testing.T, coreID string) error {
	t.Helper()
	output, err := nativeE2EDocker(t, "inspect", "--format", "{{.Name}}|{{.Config.Image}}|{{json .Config.Entrypoint}}|{{.HostConfig.NetworkMode}}|{{range .Mounts}}{{.Source}}=>{{.Destination}}:{{.RW}};{{end}}|{{range .Config.Env}}{{println .}}{{end}}", nativeE2ESidecarContainer)
	if err != nil {
		return err
	}
	expected := "/" + nativeE2ESidecarContainer + "|" + nativeE2EFixtureImage + "|[\"/fixture-server\"]|container:" + coreID + "|" + nativeE2EFixtureServerMount + "=>/fixture-server:false;" + nativeE2EFixtureCounterMount + "=>/fixture-data:true;|NOTIFICATION_FIXTURE_COUNTER_FILE=" + nativeE2EFixtureCounterFile
	if strings.TrimSpace(output) != expected {
		return fmt.Errorf("sidecar identity/mount/config mismatch: %s", strings.ReplaceAll(strings.TrimSpace(output), "\n", ","))
	}
	return nil
}

func nativeE2EEnsureSidecar(t *testing.T, coreID string, expected nativeE2EFixtureCounter, forceRecreate bool) error {
	t.Helper()
	state, inspectErr := nativeE2EInspectCore(t)
	if inspectErr != nil || !state.running || state.id != coreID || state.image != nativeE2EFixtureImage {
		return errors.New("Core container is not the approved running image9 target")
	}
	current, err := nativeE2EDocker(t, "inspect", "--format", "{{.Name}}|{{.Config.Image}}|{{json .Config.Entrypoint}}|{{.HostConfig.NetworkMode}}|{{range .Mounts}}{{.Source}}=>{{.Destination}}:{{.RW}};{{end}}|{{range .Config.Env}}{{println .}}{{end}}", nativeE2ESidecarContainer)
	if err == nil {
		if checkErr := nativeE2ECheckSidecarConfig(t, coreID); checkErr != nil {
			return checkErr
		}
		if !strings.Contains(current, nativeE2EFixtureServerMount) {
			return errors.New("existing sidecar did not match the approved fixture mount")
		}
		if forceRecreate {
			if _, err := nativeE2EDocker(t, "rm", "--force", nativeE2ESidecarContainer); err != nil {
				return err
			}
			current = ""
		}
		if stateText, inspectErr := nativeE2EDocker(t, "inspect", "--format", "{{.State.Running}}", nativeE2ESidecarContainer); inspectErr == nil && strings.TrimSpace(stateText) == "false" {
			if _, err := nativeE2EDocker(t, "start", nativeE2ESidecarContainer); err != nil {
				return err
			}
		}
	}
	if err != nil || current == "" {
		if _, err := nativeE2EDocker(t, "run", "--detach", "--name", nativeE2ESidecarContainer,
			"--network", "container:"+nativeE2ECoreContainer,
			"--mount", "type=bind,source="+nativeE2EFixtureServerMount+",target=/fixture-server,readonly",
			"--mount", "type=bind,source="+nativeE2EFixtureCounterMount+",target=/fixture-data",
			"--env", "NOTIFICATION_FIXTURE_COUNTER_FILE="+nativeE2EFixtureCounterFile,
			"--entrypoint", "/fixture-server", nativeE2EFixtureImage); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(nativeE2ESidecarURL + "/__fixture/calls")
		if err == nil {
			var current nativeE2EFixtureCounter
			decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&current)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && decodeErr == nil && current.Durable {
				if current.SendCalls != expected.SendCalls || !reflect.DeepEqual(current.CallsByDelivery, expected.CallsByDelivery) {
					return fmt.Errorf("durable per-delivery counter changed across namespace recovery: before=%+v after=%+v", expected, current)
				}
				return nil
			}
		} else if response != nil {
			_ = response.Body.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("recreated fixture sidecar did not expose its durable counter")
}

type nativeE2EContainerState struct {
	name         string
	image        string
	running      bool
	restartCount int
	startedAt    string
	id           string
}

func nativeE2EInspectCore(t *testing.T) (nativeE2EContainerState, error) {
	t.Helper()
	output, err := nativeE2EDocker(t, "inspect", "--format", "{{.Name}}|{{.Config.Image}}|{{.State.Running}}|{{.RestartCount}}|{{.State.StartedAt}}|{{.Id}}", nativeE2ECoreContainer)
	if err != nil {
		return nativeE2EContainerState{}, err
	}
	var state nativeE2EContainerState
	parts := strings.Split(strings.TrimSpace(output), "|")
	if len(parts) != 6 {
		return nativeE2EContainerState{}, errors.New("unexpected Core container inspection field count")
	}
	state.name, state.image, state.startedAt, state.id = parts[0], parts[1], parts[4], parts[5]
	state.running, err = strconv.ParseBool(parts[2])
	if err != nil {
		return nativeE2EContainerState{}, errors.New("invalid Core container running state")
	}
	state.restartCount, err = strconv.Atoi(parts[3])
	if err != nil {
		return nativeE2EContainerState{}, errors.New("invalid Core container restart count")
	}
	return state, nil
}

func nativeE2EDocker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(output)), fmt.Errorf("docker %s failed: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

func nativeE2EWaitCoreHealth(t *testing.T, timeout time.Duration) error {
	t.Helper()
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(nativeE2ECoreURL + "/__encore/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return lastErr
}

func sqlQuoteValue(value string) string { return strings.ReplaceAll(value, "'", "''") }

func readNativeCoreCount(query string, count *int) error {
	command := exec.Command("docker", "exec", "tp-notification-execution-pg-clean", "psql", "-U", "notification_test", "-d", "notification_core_native", "-Atc", query)
	command.Env = append(os.Environ(), "PGPASSWORD=fixture-only-password")
	output, err := command.Output()
	if err != nil {
		return err
	}
	_, err = fmt.Sscanf(strings.TrimSpace(string(output)), "%d", count)
	return err
}
