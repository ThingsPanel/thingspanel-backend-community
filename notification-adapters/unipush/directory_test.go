package unipush

import (
	"context"
	"encoding/json"
	"encore.app/notification-adapters/sdk"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMemberDeviceRecheckedBeforeSend(t *testing.T) {
	calls := 0
	mode := "active"
	cloud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["push_clientid"] != "phone-cid" || body["user_id"] != "member" || body["tenant_id"] != "tenant" || body["alarm_id"] != "alarm" || body["message_id"] != "delivery" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 32) {
			t.Error("wrong scoped alarm payload")
		}
		w.Write([]byte(`{"errCode":0}`))
	}))
	defer cloud.Close()
	directory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenantId") != "tenant" || r.Header.Get("X-Notification-Deployment") != "deployment" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("d", 32) {
			t.Error("wrong private directory scope")
		}
		if mode == "logout" {
			w.WriteHeader(404)
			return
		}
		tenant := "tenant"
		if mode == "cross-tenant" {
			tenant = "other"
		}
		json.NewEncoder(w).Encode(device{ID: "binding", TenantID: tenant, UserID: "member", AppID: "app", CID: "phone-cid"})
	}))
	defer directory.Close()
	p, _ := New(cloud.URL, true)
	if err := p.ConfigureDirectory(directory.URL, strings.Repeat("d", 32), "deployment", "app", strings.Repeat("s", 32), true); err != nil {
		t.Fatal(err)
	}
	request := sdk.SendRequest{DeliveryID: "delivery", TenantID: "tenant", Channel: "im", Recipient: sdk.Recipient{Kind: "user_id", Address: "binding"}, Content: sdk.Content{Kind: "template", Template: &sdk.Template{ID: "device_alarm", Params: map[string]any{"title": "Alarm", "text": "Over threshold", "alarmId": "alarm"}}}}
	result, _ := p.Send(context.Background(), request)
	if result.Outcome != "accepted" || calls != 1 {
		t.Fatal(result, calls)
	}
	for _, state := range []string{"logout", "cross-tenant"} {
		mode = state
		result, _ = p.Send(context.Background(), request)
		if result.Outcome != "rejected" || calls != 1 {
			t.Fatal("revoked device reached cloud", result, calls)
		}
	}
}
