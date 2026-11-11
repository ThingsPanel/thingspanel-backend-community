package aliyunsms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMNSRESTReceiveAckAndVisibilityUseCurrentReceiptHandle(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/queues/Alicom-Queue-1-SmsReport/messages" || r.Header.Get("x-mns-version") != mnsVersion || !strings.HasPrefix(r.Header.Get("Authorization"), "MNS fixture-ak:") || r.Header.Get("Date") == "" {
			t.Errorf("unexpected MNS request: path=%q headers=%v", r.URL.Path, r.Header)
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("waitseconds") != "10" {
				t.Errorf("waitseconds=%q", r.URL.Query().Get("waitseconds"))
			}
			w.Header().Set("Content-Type", "text/xml")
			_, _ = io.WriteString(w, "<Message xmlns=\"http://mns.aliyuncs.com/doc/v1/\"><MessageId>stable-id</MessageId><ReceiptHandle>latest-handle</ReceiptHandle><MessageBody>[{\"phone_number\":\"13800138000\",\"success\":true,\"biz_id\":\"biz\"}]</MessageBody></Message>")
		case http.MethodDelete:
			if r.URL.Query().Get("ReceiptHandle") != "latest-handle" {
				t.Errorf("delete did not use receipt handle: %v", r.URL.Query())
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			if r.URL.Query().Get("ReceiptHandle") != "latest-handle" || string(body) != "<VisibilityTimeout>60</VisibilityTimeout>" {
				t.Errorf("visibility request query=%v body=%q", r.URL.Query(), body)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	queue := testMNSQueue(t, server, 10)
	msg, err := queue.Pull(context.Background())
	if err != nil || msg.MessageID != "stable-id" || msg.AckHandle != "latest-handle" || !strings.Contains(string(msg.Body), "biz") {
		t.Fatalf("message=%+v err=%v", msg, err)
	}
	if err := queue.MakeVisible(context.Background(), msg.AckHandle, 60); err != nil {
		t.Fatal(err)
	}
	if err := queue.Ack(context.Background(), msg.AckHandle); err != nil || calls != 3 {
		t.Fatalf("ack err=%v calls=%d", err, calls)
	}
}

func TestMNSRESTDoesNotFollowRedirectOrUseProxy(t *testing.T) {
	var redirectedCalls int
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirectedCalls++ }))
	defer redirect.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", redirect.URL)
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	queue := testMNSQueue(t, server, 1)
	if queue.client.(*http.Client).Transport.(*http.Transport).Proxy != nil {
		t.Fatal("MNS transport reads proxy configuration")
	}
	if _, err := queue.Pull(context.Background()); err == nil || redirectedCalls != 0 {
		t.Fatalf("redirect followed or accepted: err=%v redirectedCalls=%d", err, redirectedCalls)
	}
}

func TestMNS404OnlyTreatsMessageNotExistAsEmptyQueue(t *testing.T) {
	for _, tc := range []struct {
		code string
		want bool
	}{{code: "MessageNotExist", want: true}, {code: "QueueNotExist", want: false}} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>"+tc.code+"</Code></Error>")
		}))
		queue := testMNSQueue(t, server, 1)
		_, err := queue.Pull(context.Background())
		if tc.want && err != ErrNoQueueMessage {
			t.Errorf("code=%s err=%v want empty queue", tc.code, err)
		}
		if !tc.want && (err == nil || err == ErrNoQueueMessage) {
			t.Errorf("code=%s err=%v hid queue configuration failure", tc.code, err)
		}
		server.Close()
	}
}

func TestMNSRequestSignatureMatchesDocumentedConstruction(t *testing.T) {
	date := "Wed, 28 May 2012 22:32:00 GMT"
	headers := http.Header{}
	headers.Set("Date", date)
	headers.Set("x-mns-version", mnsVersion)
	got := signMNSRequest(http.MethodGet, "/queues/q/messages?waitseconds=10", headers, "fixture-ak", "fixture-secret")
	stringToSign := "GET\n\n\n" + date + "\nx-mns-version:" + mnsVersion + "\n/queues/q/messages?waitseconds=10"
	mac := hmac.New(sha1.New, []byte("fixture-secret"))
	_, _ = io.WriteString(mac, stringToSign)
	want := "MNS fixture-ak:" + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Fatalf("signature mismatch got=%q want=%q", got, want)
	}
}

func TestMNSConfigPinsHTTPSRegionAndQueue(t *testing.T) {
	base := MNSConfig{AccountID: "123456", Region: "cn-hangzhou", QueueName: "Alicom-Queue-1-SmsReport", AccessKeyID: "ak", AccessKeySecret: "secret"}
	queue, err := newMNSQueue(base, newMNSHTTPClient(), time.Now, false)
	if err != nil {
		t.Fatal(err)
	}
	if queue.endpoint.String() != "https://123456.mns.cn-hangzhou.aliyuncs.com" {
		t.Fatalf("unexpected endpoint: %s", queue.endpoint)
	}
	for _, bad := range []MNSConfig{
		{AccountID: "account", Region: "cn-hangzhou", QueueName: "bad/name", AccessKeyID: "ak", AccessKeySecret: "secret"},
		{AccountID: "account", Region: "cn-hangzhou", QueueName: "queue", AccessKeyID: "ak", AccessKeySecret: "secret", WaitSeconds: 31},
	} {
		if _, err := newMNSQueue(bad, newMNSHTTPClient(), time.Now, false); err == nil {
			t.Fatalf("invalid MNS config accepted: %+v", bad)
		}
	}
}

func testMNSQueue(t *testing.T, server *httptest.Server, wait int) *MNSQueue {
	t.Helper()
	certs := x509.NewCertPool()
	certs.AddCert(server.Certificate())
	client := newMNSHTTPClient()
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: certs}
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &MNSQueue{config: MNSConfig{AccountID: endpoint.Host, Region: "cn-hangzhou", QueueName: "Alicom-Queue-1-SmsReport", AccessKeyID: "fixture-ak", AccessKeySecret: "fixture-secret", WaitSeconds: wait}, endpoint: endpoint, client: client, now: func() time.Time { return time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC) }}
}
