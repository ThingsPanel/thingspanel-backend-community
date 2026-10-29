package ports

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestAutomationNotificationMatchesActualCoreWireSchema(t *testing.T) {
	raw, err := os.ReadFile("../../l3-core/notification/wire_schema/source-event-v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("https://notification.test/source.json", document); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile("https://notification.test/source.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if err := schema.Validate(body); err != nil {
			t.Errorf("automation produced an invalid Core request: %v", err)
			w.WriteHeader(400)
			return
		}
		if r.Header.Get("Idempotency-Key") != "notification-key" {
			t.Error("idempotency key lost")
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`{"code":0,"data":{"accepted":true,"notificationId":"notice-1","deliveryIds":["delivery-1"],"intakeStatus":"ready"}}`))
	}))
	defer server.Close()
	owner := NewHTTPNotificationOwner(Config{NotificationURL: server.URL, NotificationToken: "service-token", DeploymentID: "deployment-a"})
	result, err := owner.Submit(context.Background(), NotificationRequest{TenantID: "tenant-a", PolicyID: "group-a", GroupRevision: 1, Subject: "acceptance", Text: "body", SourceEventID: "event-a", ActionID: "action-a", IdempotencyKey: "notification-key", OccurredAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil || result.State != NotificationAccepted {
		t.Fatalf("valid notification not accepted: %+v %v", result, err)
	}
}
