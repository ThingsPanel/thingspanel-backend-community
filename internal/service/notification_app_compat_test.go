package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/global"
)

// These characterization tests send only to httptest and use the isolated
// notification_test database. They do not prove delivery to a phone.
func appPushFixture(t *testing.T, status int, response string) *[]model.MessagePushSend {
	t.Helper()
	openSourceCompatPGFixture(t)
	if err := global.DB.AutoMigrate(&model.MessagePushConfig{}, &model.MessagePushManage{}, &model.MessagePushLog{}, &model.NotificationHistory{}); err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Exec(`CREATE TABLE users (id text PRIMARY KEY, name text, tenant_id text)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.Exec(`INSERT INTO users VALUES ('app-user', 'App fixture', 'tenant-a'), ('other-user', 'Other fixture', 'tenant-b')`).Error; err != nil {
		t.Fatal(err)
	}
	var captured []model.MessagePushSend
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Error("unexpected push HTTP contract")
		}
		var message model.MessagePushSend
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
		}
		captured = append(captured, message)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, response)
	}))
	t.Cleanup(server.Close)
	if err := global.DB.Create(&model.MessagePushConfig{ID: "fixture", URL: server.URL, ConfigType: 1, CreateTime: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	return &captured
}

func TestAppPushLegacyProviderResponseLogging(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   int16
	}{
		{"accepted", 200, `{"errCode":0}`, 1},
		{"legacy_accepted", 200, `{"code":200}`, 1},
		{"provider_rejected", 200, `{"errCode":1001}`, 2},
		{"malformed_response", 200, `not-json`, 2},
		// Existing limitation: HTTP status is ignored by MessagePushSend.
		{"known_limitation_http_500_with_success_body", 500, `{"errCode":0}`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := appPushFixture(t, tc.status, tc.body)
			manage := model.MessagePushManage{ID: "fixture-device", UserID: "app-user", PushID: "fixture-client", DeviceType: "android", Status: 1, CreateTime: time.Now()}
			if err := global.DB.Create(&manage).Error; err != nil {
				t.Fatal(err)
			}
			GroupApp.MessagePush.MessagePushSendAndLog(model.MessagePushSend{Title: "local fixture", Content: "no real send", PushClientId: manage.PushID}, manage, 2)
			var logs []model.MessagePushLog
			if err := global.DB.Find(&logs).Error; err != nil {
				t.Fatal(err)
			}
			if len(*captured) != 1 || len(logs) != 1 || logs[0].Status != tc.want {
				t.Fatalf("requests=%d logs=%+v want status=%d", len(*captured), logs, tc.want)
			}
		})
	}
}

func TestAppPushLegacyMultiDeviceAndTenantSelection(t *testing.T) {
	captured := appPushFixture(t, 200, `{"errCode":0}`)
	for _, m := range []model.MessagePushManage{
		{ID: "android", UserID: "app-user", PushID: "client-android", DeviceType: "android", Status: 1},
		{ID: "ios", UserID: "app-user", PushID: "client-ios", DeviceType: "ios", Status: 1},
		{ID: "logout", UserID: "app-user", PushID: "client-logout", Status: 2},
		{ID: "foreign", UserID: "other-user", PushID: "client-foreign", Status: 1},
	} {
		m.CreateTime = time.Now()
		if err := global.DB.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	GroupApp.MessagePush.NotificationMessagePushSend("tenant-a", "fixture", "fixture content", map[string]interface{}{"alarm_config_id": "alarm-fixture"})
	got := map[string]bool{}
	for _, message := range *captured {
		got[message.PushClientId] = true
		if message.AlarmId == nil || *message.AlarmId != "alarm-fixture" {
			t.Fatal("alarm context lost")
		}
	}
	if len(*captured) != 2 || !got["client-android"] || !got["client-ios"] {
		t.Fatalf("unexpected target selection: %v", got)
	}
}

func TestAppPushLegacyKnownLimitationGroupSuccessAfterProviderRejection(t *testing.T) {
	captured := appPushFixture(t, 200, `{"errCode":1001}`)
	manage := model.MessagePushManage{ID: "rejected", UserID: "app-user", PushID: "client-rejected", Status: 1, CreateTime: time.Now()}
	if err := global.DB.Create(&manage).Error; err != nil {
		t.Fatal(err)
	}
	config := `{"MEMBER":[{"name":"app-user","notificationType":["APP"]}]}`
	group := &model.NotificationGroup{ID: "app-group", TenantID: "tenant-a", NotificationConfig: &config}
	if err := GroupApp.NotificationServicesConfig.handleMemberNotification(group, `{"id":"alarm-history-fixture"}`, "fixture", "fixture", "tenant-a"); err != nil {
		t.Fatal(err)
	}
	var pushLog model.MessagePushLog
	var history model.NotificationHistory
	if err := global.DB.First(&pushLog).Error; err != nil {
		t.Fatal(err)
	}
	if err := global.DB.First(&history).Error; err != nil {
		t.Fatal(err)
	}
	if len(*captured) != 1 || pushLog.Status != 2 || history.SendResult == nil || *history.SendResult != "SUCCESS" {
		t.Fatalf("legacy outcome changed; reassess adapter boundary: push=%d history=%v", pushLog.Status, history.SendResult)
	}
	t.Log("Confirmed limitation: provider rejects the device push but legacy group history records SUCCESS; this is not a native per-target delivery result.")
}
