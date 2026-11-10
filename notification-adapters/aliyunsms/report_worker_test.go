package aliyunsms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"encore.app/notification-adapters/sdk"
)

func TestReportWorkerRoutesExactLedgerScopeAndPostsBeforeQueueAck(t *testing.T) {
	queue := &fakeQueue{
		namespace: "scope",
		message:   QueueMessage{MessageID: "mns-stable", AckHandle: "mns-current-handle", Body: []byte("[{\"phone_number\":\"13800138000\",\"success\":true,\"biz_id\":\"biz-1\",\"report_time\":\"2026-10-09 01:00:00\"}]")},
	}
	ledger, err := OpenSendLedger(filepath.Join(t.TempDir(), "ledger.bin"), []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	record := SendLedgerRecord{QueueNamespace: "scope", BizID: "biz-1", Phone: "13800138000", TenantID: "tenant-1", InstanceID: "instance-1", DeliveryID: "delivery-1", ConfigVersion: 2, SentAt: testLedgerRecord().SentAt}
	if err := ledger.RecordAccepted(record); err != nil {
		t.Fatal(err)
	}
	routes := staticReceiptRoutes{route: ReceiptRoute{TenantID: "tenant-1", InstanceID: "instance-1", CoreURL: "https://core.example", ReceiptToken: "private-token"}}
	poster := &recordingPoster{queue: queue}
	worker, err := NewReportWorker(queue, ledger, routes, poster)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !queue.acked || queue.released != 0 || len(poster.receipts) != 1 {
		t.Fatalf("acked=%v released=%d receipts=%d", queue.acked, queue.released, len(poster.receipts))
	}
	got := poster.receipts[0]
	if got.DeliveryID != record.DeliveryID || got.ProviderMessageID != record.BizID || got.Status != "delivered" || poster.route.InstanceID != record.InstanceID {
		t.Fatalf("receipt=%+v route=%+v", got, poster.route)
	}
	if poster.wasAckedAtPost {
		t.Fatal("queue was acknowledged before durable receipt post")
	}
}

func TestReportWorkerUnmappedOrUnknownWrapperIsDelayedAndNeverAcknowledged(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{name: "unmapped", body: "[{\"phone_number\":\"13800138000\",\"success\":true,\"biz_id\":\"missing\",\"report_time\":\"2026-10-09 01:00:00\"}]", want: ErrUnmappedReport},
		{name: "unknown documented wrapper", body: "{\"arg\":\"{\\\"biz_id\\\":\\\"biz-1\\\"}\"}", want: ErrUnrecognizedReportBody},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			queue := &fakeQueue{namespace: "scope", message: QueueMessage{MessageID: "mns-id", AckHandle: "mns-handle", Body: []byte(tc.body)}}
			ledger, err := OpenSendLedger(filepath.Join(t.TempDir(), "ledger.bin"), []byte(strings.Repeat("k", 32)))
			if err != nil {
				t.Fatal(err)
			}
			defer ledger.Close()
			worker, err := NewReportWorker(queue, ledger, staticReceiptRoutes{}, &recordingPoster{queue: queue})
			if err != nil {
				t.Fatal(err)
			}
			if err := worker.RunOnce(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if queue.acked || queue.released != 1 {
				t.Fatalf("acked=%v released=%d", queue.acked, queue.released)
			}
		})
	}
}

func TestReportWorkerCoreFailureLeavesProviderMessageAvailable(t *testing.T) {
	queue := &fakeQueue{namespace: "scope", message: QueueMessage{MessageID: "mns-id", AckHandle: "mns-handle", Body: []byte("[{\"phone_number\":\"13800138000\",\"success\":false,\"biz_id\":\"biz-1\",\"report_time\":\"2026-10-09 01:00:00\"}]")}}
	ledger, err := OpenSendLedger(filepath.Join(t.TempDir(), "ledger.bin"), []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	record := SendLedgerRecord{QueueNamespace: "scope", BizID: "biz-1", Phone: "13800138000", TenantID: "tenant-1", InstanceID: "instance-1", DeliveryID: "delivery-1", ConfigVersion: 2, SentAt: testLedgerRecord().SentAt}
	if err := ledger.RecordAccepted(record); err != nil {
		t.Fatal(err)
	}
	poster := &recordingPoster{queue: queue, err: errors.New("503")}
	worker, err := NewReportWorker(queue, ledger, staticReceiptRoutes{route: ReceiptRoute{TenantID: "tenant-1", InstanceID: "instance-1", CoreURL: "https://core.example", ReceiptToken: "token"}}, poster)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.RunOnce(context.Background()); err == nil || queue.acked || queue.released != 1 {
		t.Fatalf("err=%v acked=%v released=%d", err, queue.acked, queue.released)
	}
}

func TestCoreReceiptPosterUsesFixedHTTPSBearerAndInstanceEndpoint(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/notification-instances/instance-1/receipts" || r.Header.Get("Authorization") != "Bearer route-token" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected core receipt request: %s %s headers=%v", r.Method, r.URL, r.Header)
		}
		var receipt sdk.Receipt
		if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil || receipt.DeliveryID != "delivery-1" || receipt.ProviderMessageID != "biz-1" {
			t.Errorf("receipt body=%+v err=%v", receipt, err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "{}")
	}))
	defer server.Close()
	certs := x509.NewCertPool()
	certs.AddCert(server.Certificate())
	poster := NewCoreReceiptPoster()
	poster.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: certs}
	route := ReceiptRoute{TenantID: "tenant-1", InstanceID: "instance-1", CoreURL: server.URL, ReceiptToken: "route-token"}
	receipt := sdk.Receipt{EventID: "event-1", ProviderMessageID: "biz-1", DeliveryID: "delivery-1", Status: "delivered"}
	if err := poster.PostReceipt(context.Background(), route, receipt); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	route.CoreURL = "http://core.example"
	if err := poster.PostReceipt(context.Background(), route, receipt); err == nil {
		t.Fatal("non-HTTPS core URL accepted")
	}
}

type staticReceiptRoutes struct{ route ReceiptRoute }

func (r staticReceiptRoutes) ResolveReceiptRoute(_ context.Context, record SendLedgerRecord) (ReceiptRoute, error) {
	if r.route.TenantID == "" || r.route.InstanceID == "" {
		return ReceiptRoute{}, errors.New("route missing")
	}
	if record.TenantID != r.route.TenantID || record.InstanceID != r.route.InstanceID {
		return ReceiptRoute{}, errors.New("route scope mismatch")
	}
	return r.route, nil
}

type recordingPoster struct {
	queue          *fakeQueue
	receipts       []sdk.Receipt
	route          ReceiptRoute
	err            error
	wasAckedAtPost bool
}

func (p *recordingPoster) PostReceipt(_ context.Context, route ReceiptRoute, receipt sdk.Receipt) error {
	p.wasAckedAtPost = p.queue.acked
	if p.err != nil {
		return p.err
	}
	p.route = route
	p.receipts = append(p.receipts, receipt)
	return nil
}
