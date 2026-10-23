package notification

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
	"github.com/google/uuid"
)

func createWorkerFixtureInstance(t *testing.T, store *Store, tenantID, origin string) string {
	t.Helper()
	ctx := context.Background()
	registrationID := "worker-plugin-" + uuid.NewString()
	manifest := contract.PluginManifestV1{
		APIVersion: "1.0", PluginID: registrationID, Name: "Worker fixture", PluginVersion: "1.0.0",
		Channels: []contract.Channel{contract.ChannelEmail}, ContentModes: []string{"text"},
		ConfigSchema: json.RawMessage(`{"type":"object"}`), RecipientSchema: json.RawMessage(`{"type":"object"}`),
		SecretFields: []string{"token"},
	}
	plugin, err := store.RegisterPlugin(ctx, tenantID, "worker-reg-"+uuid.NewString(), strings.Repeat("a", 64), contract.PluginRegistration{
		PluginID: manifest.PluginID, PluginVersion: manifest.PluginVersion, Origin: origin,
		AuthSecret: "fixture-worker-auth", ManifestDigest: strings.Repeat("b", 64),
	}, manifest)
	if err != nil {
		t.Fatalf("register worker fixture plugin: %v", err)
	}
	grantFixturePlugin(t, store, tenantID, plugin.ID)
	instance, err := store.CreateInstance(ctx, tenantID, "worker-instance-"+uuid.NewString(), strings.Repeat("c", 64), contract.InstanceCreate{
		PluginRegistrationID: plugin.ID, Name: "worker fixture", Channel: contract.ChannelEmail,
		Config: json.RawMessage(`{"region":"fixture"}`), ProviderIdentity: map[string]string{"account": "fixture"},
	}, map[string]string{"token": "fixture-worker-secret"})
	if err != nil {
		t.Fatalf("create worker fixture instance: %v", err)
	}
	enabled := true
	if _, err := store.UpdateInstance(ctx, tenantID, instance.ID, "worker-enable-"+uuid.NewString(), strings.Repeat("d", 64), contract.InstanceUpdate{ExpectedVersion: 1, Enabled: &enabled}); err != nil {
		t.Fatalf("enable worker fixture instance: %v", err)
	}
	return instance.ID
}

func submitWorkerFixtures(t *testing.T, store *Store, tenantID, instanceID string, recipients int) contract.AcceptedData {
	t.Helper()
	text := "fixture notification"
	expires := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339)
	request := contract.NotificationSubmit{Source: contract.SourceRef{Type: "test", ID: "worker-fixture"}, ExpiresAt: expires}
	for i := 0; i < recipients; i++ {
		request.Deliveries = append(request.Deliveries, contract.SubmitDelivery{
			InstanceID: instanceID, Recipient: contract.Recipient{Kind: "email", Address: "worker-" + uuid.NewString() + "@example.test"},
			Content: contract.Content{Kind: "text", Text: &text},
		})
	}
	accepted, err := store.AcceptSubmission(context.Background(), tenantID, "worker-submit-"+uuid.NewString(), strings.Repeat("e", 64), request)
	if err != nil {
		t.Fatalf("accept worker fixture submission: %v", err)
	}
	return accepted
}

func TestWorkerPostgresFairClaimsRotateAcrossTenantBacklogs(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	ctx := context.Background()
	instanceA := createWorkerFixtureInstance(t, store, "worker-tenant-a", "https://fixture-a.example.test")
	instanceB := createWorkerFixtureInstance(t, store, "worker-tenant-b", "https://fixture-b.example.test")
	submitWorkerFixtures(t, store, "worker-tenant-a", instanceA, 3)
	submitWorkerFixtures(t, store, "worker-tenant-b", instanceB, 3)

	var tenants []string
	cursor := ""
	for cycle := 0; cycle < 6; cycle++ {
		claims, next, err := store.ClaimDeliveriesFair(ctx, 1, time.Minute, cursor)
		if err != nil {
			t.Fatalf("fair claim cycle %d: %v", cycle, err)
		}
		if len(claims) != 1 {
			t.Fatalf("fair claim cycle %d returned tenants/tasks: %+v", cycle, claims)
		}
		if cycle > 0 && claims[0].TenantID == tenants[len(tenants)-1] {
			t.Fatalf("cursor failed to rotate after previous tenant: %v then %v", tenants[len(tenants)-1], claims[0].TenantID)
		}
		for _, claim := range claims {
			tenants = append(tenants, claim.TenantID)
			if err := store.FinishDelivery(ctx, DeliveryFinish{TenantID: claim.TenantID, DeliveryID: claim.DeliveryID, AttemptID: claim.AttemptID, LeaseToken: claim.LeaseToken, Result: uncertainPluginSubmission(claim.DeliveryID), FinishedAt: time.Now().UTC()}); err != nil {
				t.Fatalf("finish fairness fixture claim: %v", err)
			}
		}
		cursor = next
	}
	if len(tenants) != 6 || tenants[0] != "worker-tenant-a" || tenants[1] != "worker-tenant-b" || tenants[2] != "worker-tenant-a" || tenants[3] != "worker-tenant-b" || tenants[4] != "worker-tenant-a" || tenants[5] != "worker-tenant-b" {
		t.Fatalf("backlogged tenants were not served in a rotating order: %v", tenants)
	}
}

func TestWorkerPostgresHTTPDisconnectAfterSideEffectBecomesUnknownAndIsNotResent(t *testing.T) {
	store, _ := newMigratedTestStore(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/v1/send" || r.Header.Get("Authorization") != "Bearer fixture-worker-auth" || r.Header.Get("Idempotency-Key") != "" {
			t.Error("unexpected plugin request route or credentials")
		}
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error("could not simulate provider response loss")
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	instanceID := createWorkerFixtureInstance(t, store, "worker-tenant-disconnect", server.URL)
	accepted := submitWorkerFixtures(t, store, "worker-tenant-disconnect", instanceID, 1)
	client, err := NewSafePluginClient(PluginHTTPOptions{TestMode: true, AllowHTTPTestOrigin: true, Timeout: time.Second})
	if err != nil {
		t.Fatal("construct loopback-only plugin client")
	}
	sender := &InstancePluginSender{Client: client, Runtimes: store}
	worker, err := NewWorker(WorkerOptions{Store: store, Sender: sender, Concurrency: 1, BatchSize: 1, PluginTimeout: time.Second, FinishTimeout: time.Second, LeaseFor: 3 * time.Second})
	if err != nil {
		t.Fatalf("construct worker: %v", err)
	}
	cycle, err := worker.RunOnce(context.Background())
	if err != nil || cycle.Claimed != 1 || cycle.Unknown != 1 {
		t.Fatalf("lost HTTP response was not persisted as unknown: cycle=%+v err=%v", cycle, err)
	}
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("second worker cycle: %v", err)
	}
	var status string
	if err := store.db.QueryRowContext(context.Background(), `SELECT dispatch_status FROM notification_deliveries WHERE tenant_id=$1 AND id=$2`, "worker-tenant-disconnect", accepted.DeliveryIDs[0]).Scan(&status); err != nil {
		t.Fatal("read fixture delivery status")
	}
	if status != "unknown" || calls.Load() != 1 {
		t.Fatalf("ambiguous side effect was retried: status=%q HTTP calls=%d", status, calls.Load())
	}
}
