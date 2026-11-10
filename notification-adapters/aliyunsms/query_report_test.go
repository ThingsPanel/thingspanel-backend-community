package aliyunsms

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPollAcceptedUsesOnlySignedQuerySendDetails(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Acs-Action") != "QuerySendDetails" || r.Header.Get("X-Acs-Version") != apiVersion {
			t.Errorf("unexpected request method/action/version: %s %q %q", r.Method, r.Header.Get("X-Acs-Action"), r.Header.Get("X-Acs-Version"))
		}
		if r.TLS == nil {
			t.Error("query was not sent over TLS")
		}
		query := r.URL.Query()
		for key, want := range map[string]string{
			"BizId": "biz-accepted-1", "PhoneNumber": "+8613800138000", "SendDate": "20261009", "PageSize": "50", "CurrentPage": "1",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("query %s=%q, want %q", key, got, want)
			}
		}
		if strings.Contains(r.URL.RawQuery, "fixture-secret") || r.URL.Query().Get("Action") != "" {
			t.Error("credentials or an unexpected action appeared in the query string")
		}
		canonical := canonicalQueryString(query)
		headers := map[string]string{
			"host":                  r.Host,
			"x-acs-action":          "QuerySendDetails",
			"x-acs-content-sha256":  sha256Hex(nil),
			"x-acs-date":            fixedAliDate,
			"x-acs-signature-nonce": "fixed-query-nonce",
			"x-acs-version":         apiVersion,
		}
		wantAuthorization := signV3(http.MethodPost, "/", canonical, headers, sha256Hex(nil), "fixture-ak", "fixture-secret")
		if got := r.Header.Get("Authorization"); got != wantAuthorization {
			t.Errorf("signature mismatch: got %q want %q", got, wantAuthorization)
		}
		_, _ = w.Write([]byte(`{"Code":"OK","TotalCount":"1","SmsSendDetailDTOs":{"SmsSendDetailDTO":[{"ErrCode":"DELIVERED","ReceiveDate":"2026-10-09 09:00:10","SendDate":"2026-10-09 09:00:00","PhoneNum":"+8613800138000","SendStatus":3}]}}`))
	}))
	defer server.Close()

	provider := newProvider(server.Client(), func() time.Time { return time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC) }, func() (string, error) { return "fixed-query-nonce", nil }, server.URL+"/")
	result, err := provider.PollAccepted(context.Background(), QueryCredentials{AccessKeyID: "fixture-ak", AccessKeySecret: "fixture-secret"}, QueryRequest{
		PhoneNumber: "+8613800138000", BizID: "biz-accepted-1", SendDate: "20261009", InstanceNamespace: "instance-9",
	})
	if err != nil || result.Pending || result.Receipt == nil || result.Receipt.Status != "delivered" || result.Receipt.ProviderMessageID != "biz-accepted-1" {
		t.Fatalf("query result=%+v err=%v", result, err)
	}
	if strings.Contains(result.Receipt.Message, "+8613800138000") || result.Receipt.EventID == "" || calls.Load() != 1 {
		t.Fatalf("unexpected receipt/request count: receipt=%+v calls=%d", result.Receipt, calls.Load())
	}
}

const fixedAliDate = "2026-10-09T01:00:00Z"

func TestPollAcceptedPagesAreBoundedAndMatchPhoneAndSendDate(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		page, _ := strconv.Atoi(r.URL.Query().Get("CurrentPage"))
		if page == 1 {
			rows := make([]string, queryDetailsPageSize)
			for i := range rows {
				rows[i] = `{"ErrCode":"DELIVERED","ReceiveDate":"2026-10-09 09:00:10","SendDate":"2026-10-09 09:00:00","PhoneNum":"+8613999999999","SendStatus":3}`
			}
			_, _ = fmt.Fprintf(w, `{"Code":"OK","TotalCount":"51","SmsSendDetailDTOs":{"SmsSendDetailDTO":[%s]}}`, strings.Join(rows, ","))
			return
		}
		_, _ = w.Write([]byte(`{"Code":"OK","TotalCount":"51","SmsSendDetailDTOs":{"SmsSendDetailDTO":[{"ErrCode":"DELIVERED","ReceiveDate":"2026-10-09 09:00:10","SendDate":"2026-10-09 09:00:00","PhoneNum":"+8613800138000","SendStatus":3}]}}`))
	}))
	defer server.Close()
	provider := newProvider(server.Client(), time.Now, randomNonce, server.URL+"/")
	result, err := provider.PollAccepted(context.Background(), QueryCredentials{AccessKeyID: "ak", AccessKeySecret: "secret"}, QueryRequest{
		PhoneNumber: "+8613800138000", BizID: "biz-1", SendDate: "20261009", InstanceNamespace: "instance-1",
	})
	if err != nil || result.Receipt == nil || calls.Load() != 2 {
		t.Fatalf("bounded page lookup result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestPollAcceptedMapsOnlyFinalStatusAndRequiresExactRecipientDate(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		errCode    string
		phone      string
		sendDate   string
		pending    bool
		wantStatus string
	}{
		{name: "waiting report", status: 1, errCode: "", pending: true},
		{name: "delivered", status: 3, errCode: "DELIVERED", wantStatus: "delivered"},
		{name: "failed", status: 2, errCode: "isv.INVALID_PARAMETERS", wantStatus: "failed"},
		{name: "unknown carrier code remains pending", status: 3, errCode: "", pending: true},
		{name: "recipient mismatch", status: 3, errCode: "DELIVERED", phone: "+8613999999999", pending: true},
		{name: "send date mismatch", status: 3, errCode: "DELIVERED", sendDate: "2026-10-08 23:59:00", pending: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			phone := tc.phone
			if phone == "" {
				phone = "+8613800138000"
			}
			sendDate := tc.sendDate
			if sendDate == "" {
				sendDate = "2026-10-09 09:00:00"
			}
			body := fmt.Sprintf(`{"Code":"OK","TotalCount":"1","SmsSendDetailDTOs":{"SmsSendDetailDTO":[{"ErrCode":%q,"ReceiveDate":"2026-10-09 09:00:10","SendDate":%q,"PhoneNum":%q,"SendStatus":%d}]}}`, tc.errCode, sendDate, phone, tc.status)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			provider := newProvider(server.Client(), time.Now, randomNonce, server.URL+"/")
			result, err := provider.PollAccepted(context.Background(), QueryCredentials{AccessKeyID: "ak", AccessKeySecret: "secret"}, QueryRequest{
				PhoneNumber: "+8613800138000", BizID: "biz-1", SendDate: "20261009", InstanceNamespace: "instance-1",
			})
			if err != nil || result.Pending != tc.pending || (result.Receipt == nil) != (tc.wantStatus == "") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.Receipt != nil && result.Receipt.Status != tc.wantStatus {
				t.Fatalf("receipt status=%q want=%q", result.Receipt.Status, tc.wantStatus)
			}
		})
	}
}

func TestPollAcceptedRejectsUnboundedOrUnsafeInputs(t *testing.T) {
	var called atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
		if r.URL.Query().Get("CurrentPage") == "1" {
			_, _ = w.Write([]byte(`{"Code":"OK","TotalCount":"101","SmsSendDetailDTOs":{"SmsSendDetailDTO":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"Code":"OK","TotalCount":"0","SmsSendDetailDTOs":{"SmsSendDetailDTO":[]}}`))
	}))
	defer server.Close()
	provider := newProvider(server.Client(), time.Now, randomNonce, server.URL+"/")
	credentials := QueryCredentials{AccessKeyID: "ak", AccessKeySecret: "secret"}
	base := QueryRequest{PhoneNumber: "+8613800138000", BizID: "biz-1", SendDate: "20261009", InstanceNamespace: "instance-1"}
	if _, err := provider.PollAccepted(context.Background(), credentials, base); err == nil || called.Load() != 1 {
		t.Fatalf("page bound result err=%v calls=%d", err, called.Load())
	}
	called.Store(0)
	base.SendDate = "2026-10-09"
	if _, err := provider.PollAccepted(context.Background(), credentials, base); err == nil || called.Load() != 0 {
		t.Fatalf("invalid date reached provider: err=%v calls=%d", err, called.Load())
	}
	base.SendDate = "20261009"
	base.BizID = strings.Repeat("x", 129)
	if _, err := provider.PollAccepted(context.Background(), credentials, base); err == nil || called.Load() != 0 {
		t.Fatalf("oversized BizId reached provider: err=%v calls=%d", err, called.Load())
	}
}

func TestProviderHTTPClientDisablesRedirectsProxiesAndKeepAlives(t *testing.T) {
	client := newHTTPClient()
	if client.CheckRedirect == nil {
		t.Fatal("provider client follows redirects by default")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || !transport.DisableKeepAlives {
		t.Fatal("provider query client must bypass proxies and disable connection reuse")
	}
	if client.Timeout <= 0 {
		t.Fatal("provider query client has no total timeout")
	}
}

func TestPollAcceptedRejectsHTTPEndpointBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unsafe endpoint was called") }))
	defer server.Close()
	provider := newProvider(server.Client(), time.Now, randomNonce, server.URL+"/")
	_, err := provider.PollAccepted(context.Background(), QueryCredentials{AccessKeyID: "ak", AccessKeySecret: "secret"}, QueryRequest{
		PhoneNumber: "+8613800138000", BizID: "biz-1", SendDate: "20261009", InstanceNamespace: "instance-1",
	})
	if err == nil {
		t.Fatal("HTTP endpoint accepted")
	}
}

func TestPollAcceptedRejectsRedirectWithoutFollowingIt(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		_, _ = w.Write([]byte(`{"Code":"OK","TotalCount":"0"}`))
	}))
	defer target.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	client := redirect.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	provider := newProvider(client, time.Now, randomNonce, redirect.URL+"/")
	_, err := provider.PollAccepted(context.Background(), QueryCredentials{AccessKeyID: "ak", AccessKeySecret: "secret"}, QueryRequest{
		PhoneNumber: "+8613800138000", BizID: "biz-1", SendDate: "20261009", InstanceNamespace: "instance-1",
	})
	if err == nil || targetCalls.Load() != 0 {
		t.Fatalf("redirect result err=%v target calls=%d", err, targetCalls.Load())
	}
}
