package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

// TestRealUniPushWorker runs the real Encore notification worker, PostgreSQL
// store and separately compiled UniPush plugin. Only directory and cloud are fixtures.
func TestRealUniPushWorker(t *testing.T) {
	bin := os.Getenv("UNIPUSH_FIXTURE_BIN")
	if bin == "" {
		t.Skip("UNIPUSH_FIXTURE_BIN is required")
	}
	var cloudCalls atomic.Int32
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cloudCalls.Add(1)
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["push_clientid"] != "fixture-cid" || body["tenant_id"] != "fixture-tenant" || body["user_id"] != "fixture-user" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
			t.Errorf("unexpected cloud request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"errCode":0}`)
	}))
	defer cloud.Close()
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/notification/source-app-devices/binding-1" || r.URL.Query().Get("tenantId") != "fixture-tenant" || r.Header.Get("X-Notification-Deployment") != "fixture-deployment" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("d", 32) {
			t.Errorf("unexpected directory request")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"binding-1","tenantId":"fixture-tenant","userId":"fixture-user","appId":"fixture-app","cid":"fixture-cid"}`)
	}))
	defer directory.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	_ = l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), "NOTIFICATION_UNIPUSH_ENDPOINT="+cloud.URL, "NOTIFICATION_UNIPUSH_ALLOW_HTTP=1", "NOTIFICATION_UNIPUSH_DIRECTORY_URL="+directory.URL, "NOTIFICATION_UNIPUSH_DIRECTORY_TOKEN="+strings.Repeat("d", 32), "NOTIFICATION_UNIPUSH_DEPLOYMENT_ID=fixture-deployment", "NOTIFICATION_UNIPUSH_APP_ID=fixture-app", "NOTIFICATION_UNIPUSH_CLOUD_TOKEN="+strings.Repeat("s", 32), "NOTIFICATION_PLUGIN_TOKEN=fixture-plugin-token", "NOTIFICATION_PLUGIN_LISTEN="+address)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	origin := "http://" + address
	var manifest contract.PluginManifestV1
	var raw []byte
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		raw, manifest, err = client.FetchManifest(ctx, origin, "fixture-plugin-token")
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("real plugin manifest: %v", err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	store, _ := newMigratedTestStore(t)
	plugin, err := store.RegisterPlugin(ctx, "fixture-tenant", "fixture-registration", strings.Repeat("a", 64), contract.PluginRegistration{PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: origin, AuthSecret: "fixture-plugin-token", ManifestDigest: digest}, manifest)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	grantFixturePlugin(t, store, "fixture-tenant", plugin.ID)
	instance, err := store.CreateInstance(ctx, "fixture-tenant", "fixture-instance", strings.Repeat("b", 64), contract.InstanceCreate{PluginRegistrationID: plugin.ID, Name: "fixture", Channel: contract.ChannelIM, Config: json.RawMessage(`{}`)}, map[string]string{})
	if err != nil {
		t.Fatalf("instance: %v", err)
	}
	enabled := true
	if _, err := store.UpdateInstance(ctx, "fixture-tenant", instance.ID, "fixture-enable", strings.Repeat("c", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	text := "fixture message"
	submission := contract.NotificationSubmit{Source: contract.SourceRef{Type: "test", ID: "fixture"}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339), Deliveries: []contract.SubmitDelivery{{InstanceID: instance.ID, Recipient: contract.Recipient{Kind: "user_id", Address: "binding-1"}, Content: contract.Content{Kind: "text", Text: &text}}}}
	accepted, err := store.AcceptSubmission(ctx, "fixture-tenant", "fixture-submit", strings.Repeat("e", 64), submission)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: &InstancePluginSender{Client: client, Runtimes: store}, Concurrency: 1, BatchSize: 1, PluginTimeout: time.Second * 3, FinishTimeout: time.Second * 3, LeaseFor: time.Second * 10})
	if err != nil {
		t.Fatal(err)
	}
	cycle, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.db.QueryRowContext(ctx, `SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "fixture-tenant", accepted.DeliveryIDs[0]).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if cycle.Claimed != 1 || status != "accepted" || cloudCalls.Load() != 1 {
		t.Fatalf("cycle=%+v status=%s calls=%d", cycle, status, cloudCalls.Load())
	}
}
