package aliyunsms

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSendLedgerEncryptsRecordsAndLocksProcessExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "ledger.bin")
	key := []byte(strings.Repeat("k", 32))
	ledger, err := OpenSendLedger(path, key)
	if err != nil {
		t.Fatal(err)
	}
	record := testLedgerRecord()
	if err := ledger.RecordAccepted(record); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSendLedger(path, key); err == nil {
		t.Fatal("second process unexpectedly acquired exclusive ledger lock")
	}
	if got, ok, err := ledger.LookupAccepted(record.QueueNamespace, record.BizID, record.Phone); err != nil || !ok || got.DeliveryID != record.DeliveryID {
		t.Fatalf("lookup record=%+v ok=%v err=%v", got, ok, err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{record.BizID, record.Phone, record.TenantID, record.InstanceID, record.DeliveryID} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("encrypted ledger exposed %q", secret)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("ledger permissions=%v err=%v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(filepath.Dir(path)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("ledger directory permissions=%v err=%v", info.Mode().Perm(), err)
	}
	reopened, err := OpenSendLedger(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, ok, err := reopened.LookupAccepted(record.QueueNamespace, record.BizID, record.Phone); err != nil || !ok || got.TenantID != record.TenantID {
		t.Fatalf("reopened lookup record=%+v ok=%v err=%v", got, ok, err)
	}
}

func TestSendLedgerRejectsScopeConflictAndCanonicalKeyAmbiguity(t *testing.T) {
	ledger, err := OpenSendLedger(filepath.Join(t.TempDir(), "ledger"), []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	record := testLedgerRecord()
	if err := ledger.RecordAccepted(record); err != nil {
		t.Fatal(err)
	}
	if err := ledger.RecordAccepted(record); err != nil {
		t.Fatalf("same scope replay was not idempotent: %v", err)
	}
	conflict := record
	conflict.TenantID = "tenant-other"
	if err := ledger.RecordAccepted(conflict); !errors.Is(err, ErrLedgerConflict) {
		t.Fatalf("different tenant reused provider correlation: %v", err)
	}
	if ledgerRecordKey("ab", "c", "d") == ledgerRecordKey("a", "bc", "d") {
		t.Fatal("length-prefixed tuple key collided")
	}
	if queueNamespace("account", "cn-hangzhou") == queueNamespace("acc", "ountcn-hangzhou") {
		t.Fatal("length-prefixed queue scope collided")
	}
}

func TestProviderAcceptedLedgerFailureReturnsNonretryableUnknownOnce(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"Code":"OK","BizId":"biz-accepted","RequestId":"request"}`)
	}))
	defer server.Close()
	provider := providerForTest(server, nil)
	provider.ledger = failingLedger{err: errors.New("disk full")}
	request := smsRequest()
	request.TenantID = "tenant-1"
	request.InstanceID = "instance-1"
	request.DeliveryID = "delivery-1"
	request.ConfigVersion = 3
	result, err := provider.Send(context.Background(), request)
	if err != nil || result.Outcome != "unknown" || result.ProviderMessageID != "" || result.ReceiptExpected || result.Error == nil || result.Error.Retryable || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestProviderAcceptedPersistsExactScopeBeforeReturning(t *testing.T) {
	var calls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"Code":"OK","BizId":"biz-accepted","RequestId":"request"}`)
	}))
	defer server.Close()
	ledger, err := OpenSendLedger(filepath.Join(t.TempDir(), "ledger"), []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	provider := providerForTest(server, nil)
	provider.ledger = ledger
	request := smsRequest()
	request.TenantID = "tenant-1"
	request.InstanceID = "instance-1"
	request.DeliveryID = "delivery-1"
	request.ConfigVersion = 3
	result, err := provider.Send(context.Background(), request)
	if err != nil || result.Outcome != "accepted" || result.ReceiptExpected || calls != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
	record, found, err := ledger.LookupAccepted(queueNamespace("test-ak", "cn-hangzhou"), "biz-accepted", request.Recipient.Address)
	if err != nil || !found || record.TenantID != request.TenantID || record.InstanceID != request.InstanceID || record.DeliveryID != request.DeliveryID || record.ConfigVersion != request.ConfigVersion || !record.SentAt.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("record=%+v found=%v err=%v", record, found, err)
	}
}

type failingLedger struct{ err error }

func (f failingLedger) RecordAccepted(SendLedgerRecord) error { return f.err }
func (f failingLedger) LookupAccepted(string, string, string) (SendLedgerRecord, bool, error) {
	return SendLedgerRecord{}, false, f.err
}
func (f failingLedger) Close() error { return nil }

var _ SendLedger = failingLedger{}

func testLedgerRecord() SendLedgerRecord {
	return SendLedgerRecord{QueueNamespace: "queue-hash", BizID: "biz-private-123", Phone: "13800138000", TenantID: "tenant-private", InstanceID: "instance-private", DeliveryID: "delivery-private", ConfigVersion: 8, SentAt: time.Date(2026, 10, 9, 1, 2, 3, 0, time.UTC)}
}
