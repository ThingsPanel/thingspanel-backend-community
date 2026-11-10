package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/notification-adapters/sdk"
)

func TestSignKnownVector(t *testing.T) {
	timestamp := "1735787045000"
	secret := "test secret"
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = io.WriteString(mac, timestamp+"\n"+secret)
	got := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	const want = "/r8mr/wEpD6dzH/yVIeQlW+3jFs9PeG4IJNrWGBI8qQ="
	if got != want {
		t.Fatalf("signature mismatch: got %q want %q", got, want)
	}
}

func TestSendSignsAndPostsOnlyToFixedWebhookEndpoint(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/robot/send" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("access_token") != "private-token" || r.URL.Query().Get("timestamp") != "1735787045000" || r.URL.Query().Get("sign") != "Y0Tu/gmnSnWCzwOW8iJv9+8QPx9pXkKRT4DSlUoeYfQ=" {
			t.Errorf("missing signed query")
		}
		var body struct {
			MsgType string `json:"msgtype"`
			Text    struct {
				Content string `json:"content"`
			} `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MsgType != "text" || body.Text.Content != "group notice" {
			t.Errorf("bad payload: %+v err=%v", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"errcode":0,"errmsg":"ok"}`)
	}))
	defer server.Close()

	provider := newProvider(server.Client(), func() time.Time { return time.UnixMilli(1735787045000) }, server.URL+"/robot/send")
	result, err := provider.Send(context.Background(), dingtalkRequest("https://oapi.dingtalk.com/robot/send?access_token=private-token"))
	if err != nil || result.Outcome != "accepted" || result.ReceiptExpected || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestValidationBlocksSSRFAndUnsupportedTargets(t *testing.T) {
	p := New()
	request := dingtalkRequest("https://127.0.0.1:9000/robot/send?access_token=token")
	result, err := p.Send(context.Background(), request)
	if err != nil || result.Outcome != "rejected" || result.Error.Code != "invalid_configuration" {
		t.Fatalf("SSRF config not rejected: %+v %v", result, err)
	}
	request.Config["webhook"] = "https://oapi.dingtalk.com/robot/send?access_token=token"
	request.Recipient.Address = "some-other-group"
	result, err = p.Send(context.Background(), request)
	if err != nil || result.Outcome != "rejected" {
		t.Fatalf("non-default target not rejected: %+v %v", result, err)
	}
}

func TestDingTalkBusinessRejectAndUncertainResponsesDoNotLeak(t *testing.T) {
	for _, tc := range []struct {
		body       string
		statusCode int
		outcome    string
	}{
		{`{"errcode":310000,"errmsg":"private detail"}`, http.StatusOK, "rejected"},
		{`not-json`, http.StatusOK, "unknown"},
		{strings.Repeat("x", maxBody+1), http.StatusOK, "unknown"},
		{`{"errcode":0}`, http.StatusBadGateway, "unknown"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.statusCode)
			_, _ = io.WriteString(w, tc.body)
		}))
		provider := newProvider(server.Client(), func() time.Time { return time.UnixMilli(1) }, server.URL+"/robot/send")
		request := dingtalkRequest("https://oapi.dingtalk.com/robot/send?access_token=private-token")
		result, err := provider.Send(context.Background(), request)
		server.Close()
		if err != nil || result.Outcome != tc.outcome {
			t.Fatalf("body=%q status=%d result=%+v err=%v", tc.body[:min(len(tc.body), 32)], tc.statusCode, result, err)
		}
		if result.Error != nil && (strings.Contains(result.Error.Message, "private detail") || strings.Contains(result.Error.Message, "private-token")) {
			t.Fatal("provider error leaked")
		}
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls++ }))
	defer target.Close()
	var sourceCalls int
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls++
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider := newProvider(newHTTPClient(), func() time.Time { return time.UnixMilli(1) }, source.URL+"/robot/send")
	result, err := provider.Send(context.Background(), dingtalkRequest("https://oapi.dingtalk.com/robot/send?access_token=private-token"))
	if err != nil || result.Outcome != "unknown" || sourceCalls != 1 || targetCalls != 0 {
		t.Fatalf("result=%+v err=%v source=%d target=%d", result, err, sourceCalls, targetCalls)
	}
}

func dingtalkRequest(webhook string) sdk.SendRequest {
	return sdk.SendRequest{
		DeliveryID: "delivery-1", Channel: "im",
		Recipient: sdk.Recipient{Kind: "chat_id", Address: "default"},
		Content:   sdk.Content{Kind: "text", Text: "group notice"},
		Config:    map[string]any{"webhook": webhook, "secret": "signing-secret"},
	}
}
