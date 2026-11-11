package unipush

import (
	"context"
	"encoding/json"
	"encore.app/notification-adapters/sdk"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

func TestPushPreviewLimits(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		limit       int
	}{
		{strings.Repeat("a", 19), strings.Repeat("a", 19), 19},
		{strings.Repeat("温", 20), strings.Repeat("温", 18) + "…", 19},
		{strings.Repeat("🙂", 10), strings.Repeat("🙂", 9) + "…", 19},
		{strings.Repeat("🙂", 25), strings.Repeat("🙂", 24) + "…", 49},
		{strings.Repeat("中", 49), strings.Repeat("中", 49), 49},
		{strings.Repeat("x", 50), strings.Repeat("x", 48) + "…", 49},
	} {
		got := pushPreview(tc.input, tc.limit)
		if got != tc.want || !utf8.ValidString(got) || len(utf16.Encode([]rune(got))) > tc.limit {
			t.Fatalf("preview(%q,%d)=%q", tc.input, tc.limit, got)
		}
	}
}

func TestTemplatePreviewsPreserveIdentity(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["title"] != strings.Repeat("告", 18)+"…" || body["content"] != strings.Repeat("🙂", 24)+"…" {
			t.Errorf("unbounded previews: %+v", body)
		}
		if body["alarm_id"] != "original-alarm-id" || body["message_id"] != "original-delivery-id" || body["push_clientid"] != "fixture-cid" || body["tenant_id"] != "tenant-a" || body["alarm_kind"] != "history" {
			t.Errorf("changed identity: %+v", body)
		}
		w.Write([]byte(`{"errCode":0}`))
	}))
	defer s.Close()
	p, _ := New(s.URL, true)
	r, err := p.Send(context.Background(), sdk.SendRequest{DeliveryID: "original-delivery-id", TenantID: "tenant-a", Channel: "im", Recipient: sdk.Recipient{Kind: "chat_id", Address: "fixture-cid"}, Content: sdk.Content{Kind: "template", Template: &sdk.Template{ID: "device_alarm", Params: map[string]any{"title": strings.Repeat("告", 20), "text": strings.Repeat("🙂", 25), "alarmId": "original-alarm-id"}}}})
	if err != nil || r.Outcome != "accepted" {
		t.Fatalf("send=%+v error=%v", r, err)
	}
}

func TestProviderOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"accepted", `{"code":200,"data":{"errCode":0,"data":{"taskId":"fixture"}}}`, "accepted", 200},
		{"raw_accepted", `{"errCode":0}`, "accepted", 200},
		{"rejected", `{"code":502,"data":{"errCode":1001}}`, "rejected", 200},
		{"old_wrapper_false_success", `{"code":200,"data":{"errCode":1001}}`, "rejected", 200},
		{"unconfirmed_wrapper", `{"code":200}`, "unknown", 200},
		{"http_error", `{"errCode":0}`, "unknown", 500},
		{"malformed", `broken`, "unknown", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["push_clientid"] != "fixture-cid" || body["content"] != "fixture" {
					t.Error("cloud request contract")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer s.Close()
			p, e := New(s.URL, true)
			if e != nil {
				t.Fatal(e)
			}
			r, e := p.Send(context.Background(), sdk.SendRequest{DeliveryID: "test", Channel: "im", Recipient: sdk.Recipient{Kind: "chat_id", Address: "fixture-cid"}, Content: sdk.Content{Kind: "text", Text: "fixture"}})
			if e != nil || r.Outcome != tc.want || r.ReceiptExpected || calls != 1 || (r.Error != nil && r.Error.Retryable) {
				t.Fatalf("result=%+v calls=%d error=%v", r, calls, e)
			}
		})
	}
}
func TestRejectBatchCIDAndTenantEndpoint(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer s.Close()
	p, _ := New(s.URL, true)
	for _, cid := range []string{"a,b", "a b", ""} {
		r, _ := p.Send(context.Background(), sdk.SendRequest{Channel: "im", Recipient: sdk.Recipient{Kind: "chat_id", Address: cid}, Content: sdk.Content{Kind: "text", Text: "fixture"}})
		if r.Outcome != "rejected" {
			t.Fatal(r)
		}
	}
	if p.Validate(context.Background(), sdk.ValidateRequest{Config: map[string]any{"url": "http://other"}}).Valid || calls != 0 {
		t.Fatal("tenant-controlled routing or invalid recipient reached provider")
	}
	if _, e := New(s.URL, false); e == nil {
		t.Fatal("HTTP must require local fixture opt-in")
	}
}
