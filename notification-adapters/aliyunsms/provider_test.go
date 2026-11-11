package aliyunsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"encore.app/notification-adapters/sdk"
)

func TestSignV3KnownIndependentVector(t *testing.T) {
	params := map[string][]string{
		"PhoneNumbers":  {"13800138000"},
		"SignName":      {"Test"},
		"TemplateCode":  {"SMS_123"},
		"TemplateParam": {`{"code":"1234"}`},
	}
	query := canonicalQueryString(params)
	headers := map[string]string{
		"host":                  apiHost,
		"x-acs-action":          "SendSms",
		"x-acs-content-sha256":  sha256Hex(nil),
		"x-acs-date":            "2025-01-02T03:04:05Z",
		"x-acs-signature-nonce": "000102030405060708090a0b0c0d0e0f",
		"x-acs-version":         apiVersion,
	}
	got := signV3(http.MethodPost, "/", query, headers, sha256Hex(nil), "test-ak", "unit-test-secret")
	const want = "ACS3-HMAC-SHA256 Credential=test-ak,SignedHeaders=host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version,Signature=4548b67f06e597e1eb9b633180b3770c4cab0723a796cf8f43d6a1261ef7e03d"
	if got != want {
		t.Fatalf("signature mismatch:\n got %s\nwant %s", got, want)
	}

	// Cross-check HMAC-SHA256 against RFC 4231 test case 1 as an independent
	// primitive vector used by the official V3 signing construction.
	mac := hmac.New(sha256.New, []byte(strings.Repeat("\x0b", 20)))
	_, _ = io.WriteString(mac, "Hi There")
	if hex.EncodeToString(mac.Sum(nil)) != "b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7" {
		t.Fatal("HMAC-SHA256 primitive vector mismatch")
	}
}

func TestSendSingleTemplateTargetAndMapsAccepted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/" || r.Host == "" {
			t.Errorf("unexpected request: %s host=%q url=%q", r.Method, r.Host, r.URL.Host)
		}
		if r.ContentLength != 0 || r.Header.Get("x-acs-action") != "SendSms" || r.Header.Get("x-acs-version") != apiVersion || !strings.HasPrefix(r.Header.Get("Authorization"), "ACS3-HMAC-SHA256 Credential=test-ak,") {
			t.Errorf("missing signed API headers: %v", r.Header)
		}
		query := r.URL.Query()
		if query.Get("PhoneNumbers") != "13800138000" || query.Get("SignName") != "Panel" || query.Get("TemplateCode") != "SMS_123" || query.Get("TemplateParam") != `{"code":"0000"}` || query.Get("Action") != "" {
			t.Errorf("unexpected SMS params: %v", query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Code":"OK","Message":"OK","BizId":"biz-123","RequestId":"req-1"}`)
	}))
	defer server.Close()
	provider := providerForTest(server, nil)
	if provider.Manifest().Capabilities.DeliveryReceipts {
		t.Fatal("manifest advertises receipts without a trusted queue/core sink")
	}
	request := smsRequest()
	result, err := provider.Send(context.Background(), request)
	if err != nil || calls.Load() != 1 || result.Outcome != "accepted" || result.ProviderMessageID != "biz-123" || result.ReceiptExpected || result.Error != nil {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestConcurrentInstanceConfigsStayBoundToProviderRequestsAndLedger(t *testing.T) {
	type observed struct {
		phone, signName, templateCode, auth string
		validSignature                      bool
	}
	observations := make(chan observed, 2)
	credentials := map[string]string{"ak-instance-a": "secret-instance-a", "ak-instance-b": "secret-instance-b"}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		ak := ""
		for candidate := range credentials {
			if strings.HasPrefix(r.Header.Get("Authorization"), "ACS3-HMAC-SHA256 Credential="+candidate+",") {
				ak = candidate
				break
			}
		}
		secret := credentials[ak]
		headerValues := map[string]string{
			"host": r.Host, "x-acs-action": r.Header.Get("x-acs-action"),
			"x-acs-content-sha256": r.Header.Get("x-acs-content-sha256"), "x-acs-date": r.Header.Get("x-acs-date"),
			"x-acs-signature-nonce": r.Header.Get("x-acs-signature-nonce"), "x-acs-version": r.Header.Get("x-acs-version"),
		}
		expected := signV3(http.MethodPost, "/", canonicalQueryString(query), headerValues, sha256Hex(nil), ak, secret)
		observations <- observed{phone: query.Get("PhoneNumbers"), signName: query.Get("SignName"), templateCode: query.Get("TemplateCode"), auth: r.Header.Get("Authorization"), validSignature: ak != "" && expected == r.Header.Get("Authorization")}
		switch query.Get("PhoneNumbers") {
		case "+8613800000001":
			_, _ = io.WriteString(w, `{"Code":"OK","BizId":"biz-instance-a","RequestId":"req-a"}`)
		case "+8613800000002":
			_, _ = io.WriteString(w, `{"Code":"OK","BizId":"biz-instance-b","RequestId":"req-b"}`)
		default:
			http.Error(w, "unexpected phone", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	ledger, err := OpenSendLedger(t.TempDir()+"/ledger.bin", []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal("open fixture encrypted send ledger")
	}
	defer ledger.Close()
	provider := newProvider(server.Client(), time.Now, randomNonce, server.URL+"/")
	provider.ledger = ledger
	requestA := smsRequest()
	requestA.TenantID, requestA.InstanceID, requestA.DeliveryID, requestA.ConfigVersion = "tenant-a", "instance-a", "delivery-a", 7
	requestA.Recipient.Address = "+8613800000001"
	requestA.Config = map[string]any{"access_key_id": "ak-instance-a", "access_key_secret": "secret-instance-a", "region": "cn-hangzhou", "sign_name": "Sign A", "template_allowlist": []any{"SMS_A"}}
	requestA.Content.Template.ID = "SMS_A"
	requestB := smsRequest()
	requestB.TenantID, requestB.InstanceID, requestB.DeliveryID, requestB.ConfigVersion = "tenant-b", "instance-b", "delivery-b", 11
	requestB.Recipient.Address = "+8613800000002"
	requestB.Config = map[string]any{"access_key_id": "ak-instance-b", "access_key_secret": "secret-instance-b", "region": "cn-shanghai", "sign_name": "Sign B", "template_allowlist": []any{"SMS_B"}}
	requestB.Content.Template.ID = "SMS_B"
	results := make(chan sdk.SendResult, 2)
	errors := make(chan error, 2)
	var senders sync.WaitGroup
	for _, request := range []sdk.SendRequest{requestA, requestB} {
		request := request
		senders.Add(1)
		go func() {
			defer senders.Done()
			result, err := provider.Send(context.Background(), request)
			results <- result
			errors <- err
		}()
	}
	senders.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("parallel provider call returned error: %v", err)
		}
	}
	resultByID := map[string]sdk.SendResult{}
	for result := range results {
		resultByID[result.DeliveryID] = result
	}
	if len(resultByID) != 2 || resultByID["delivery-a"].Outcome != "accepted" || resultByID["delivery-a"].ProviderMessageID != "biz-instance-a" || resultByID["delivery-b"].Outcome != "accepted" || resultByID["delivery-b"].ProviderMessageID != "biz-instance-b" {
		t.Fatalf("parallel result/config mix: %+v", resultByID)
	}
	seen := map[string]observed{}
	for i := 0; i < 2; i++ {
		request := <-observations
		seen[request.phone] = request
	}
	for phone, expected := range map[string]struct{ sign, template string }{ // per-instance wire fields must stay paired with signed keys
		"+8613800000001": {sign: "Sign A", template: "SMS_A"},
		"+8613800000002": {sign: "Sign B", template: "SMS_B"},
	} {
		got := seen[phone]
		if got.phone != phone || got.signName != expected.sign || got.templateCode != expected.template || !got.validSignature {
			t.Fatalf("request for %s crossed instance config or signature: %+v", phone, got)
		}
	}
	for _, item := range []struct {
		request sdk.SendRequest
		bizID   string
	}{
		{request: requestA, bizID: "biz-instance-a"},
		{request: requestB, bizID: "biz-instance-b"},
	} {
		ak := item.request.Config["access_key_id"].(string)
		region := item.request.Config["region"].(string)
		record, found, err := ledger.LookupAccepted(queueNamespace(ak, region), item.bizID, item.request.Recipient.Address)
		if err != nil || !found || record.TenantID != item.request.TenantID || record.InstanceID != item.request.InstanceID || record.DeliveryID != item.request.DeliveryID || record.ConfigVersion != item.request.ConfigVersion {
			t.Fatalf("ledger correlation crossed config version or target: record=%+v found=%v err=%v", record, found, err)
		}
	}
}

func TestSendRejectsTemplateAndUnsupportedParametersBeforeNetwork(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	provider := providerForTest(server, nil)
	request := smsRequest()
	request.Content.Template.ID = "SMS_NOT_ALLOWED"
	result, _ := provider.Send(context.Background(), request)
	if result.Outcome != "rejected" || result.Error.Code != "template_not_allowed" || calls.Load() != 0 {
		t.Fatalf("template allowlist failed: %+v calls=%d", result, calls.Load())
	}
	request = smsRequest()
	request.Content.Template.Params["nested"] = map[string]any{"bad": "shape"}
	result, _ = provider.Send(context.Background(), request)
	if result.Outcome != "rejected" || result.Error.Code != "invalid_template_params" || calls.Load() != 0 {
		t.Fatalf("nested parameters accepted: %+v calls=%d", result, calls.Load())
	}
}

func TestSendMapsProviderRejectionAndUnknownSafely(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		outcome    string
		code       string
		retryable  bool
	}{
		{"rate limit", `{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"private recipient detail"}`, http.StatusBadRequest, "rejected", "provider_rate_limited", true},
		{"unknown rejection", `{"Code":"provider.custom","Message":"private detail"}`, http.StatusBadRequest, "rejected", "provider_rejected", false},
		{"success without id", `{"Code":"OK"}`, http.StatusOK, "unknown", "submission_uncertain", false},
		{"oversized body", `{"Code":"OK","BizId":"` + strings.Repeat("x", maxBody) + `"}`, http.StatusOK, "unknown", "submission_uncertain", false},
		{"http error without json", `not-json`, http.StatusBadGateway, "unknown", "submission_uncertain", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			provider := providerForTest(server, nil)
			result, err := provider.Send(context.Background(), smsRequest())
			if err != nil || result.Outcome != tc.outcome || result.Error == nil || result.Error.Code != tc.code || result.Error.Retryable != tc.retryable {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if strings.Contains(result.Error.Message, "private recipient detail") || strings.Contains(result.Error.Message, "private detail") {
				t.Fatal("provider error leaked")
			}
		})
	}
}

func TestTransportFailureIsUnknownAndNeverRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server does not support hijacking")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close() // Connection loss after a request may hide provider acceptance.
	}))
	defer server.Close()
	provider := providerForTest(server, strictClientForTest(server))
	result, err := provider.Send(context.Background(), smsRequest())
	if err != nil || result.Outcome != "unknown" || result.Error == nil || result.Error.Retryable || calls.Load() != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	var sourceCalls atomic.Int32
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceCalls.Add(1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	provider := providerForTest(source, strictClientForTest(source))
	result, err := provider.Send(context.Background(), smsRequest())
	if err != nil || result.Outcome != "unknown" || sourceCalls.Load() != 1 || targetCalls.Load() != 0 {
		t.Fatalf("result=%+v err=%v source=%d target=%d", result, err, sourceCalls.Load(), targetCalls.Load())
	}
}

func TestDefaultOutboundEndpointCannotComeFromInstanceConfig(t *testing.T) {
	p := New()
	request := smsRequest()
	request.Config["endpoint"] = "http://127.0.0.1:9999/send"
	result, _ := p.Send(context.Background(), request)
	if result.Outcome != "rejected" || result.Error.Code != "invalid_configuration" {
		t.Fatalf("arbitrary endpoint was not rejected: %+v", result)
	}
}

func providerForTest(server *httptest.Server, client Doer) *Provider {
	if client == nil {
		client = server.Client()
	}
	return newProvider(client, func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }, func() (string, error) { return "000102030405060708090a0b0c0d0e0f", nil }, server.URL+"/")
}

func smsRequest() sdk.SendRequest {
	return sdk.SendRequest{
		DeliveryID: "delivery-1", Channel: "sms",
		Recipient: sdk.Recipient{Kind: "phone", Address: "13800138000"},
		Content:   sdk.Content{Kind: "template", Template: &sdk.Template{ID: "SMS_123", Params: map[string]any{"code": "0000"}}},
		Config:    map[string]any{"access_key_id": "test-ak", "access_key_secret": "test-secret", "region": "cn-hangzhou", "sign_name": "Panel", "template_allowlist": []any{"SMS_123"}},
	}
}

func strictClientForTest(server *httptest.Server) *http.Client {
	certPool := x509.NewCertPool()
	certPool.AddCert(server.Certificate())
	client := newHTTPClient()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: certPool}
	return client
}
