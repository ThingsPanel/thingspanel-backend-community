package aliyunsms

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"encore.app/notification-adapters/sdk"
)

func TestNormalizeReportsUsesStableOpaqueEventIDAndFinalState(t *testing.T) {
	batch := []byte(`[{"phone_number":"13800138000","success":true,"biz_id":"biz-123","sms_size":"1","send_time":"2026-10-09 01:00:00","report_time":"2026-10-09 01:00:02","err_code":"DELIVERED","err_msg":"private description","out_id":"out-1"}]`)
	first, err := NormalizeReports(batch, "instance-1")
	if err != nil || len(first) != 1 {
		t.Fatalf("receipts=%+v err=%v", first, err)
	}
	second, err := NormalizeReports(batch, "instance-1")
	if err != nil || first[0].EventID != second[0].EventID || first[0].Status != "delivered" || first[0].ProviderMessageID != "biz-123" || first[0].OccurredAt.Location() != time.UTC {
		t.Fatalf("normalization is not stable: first=%+v second=%+v err=%v", first, second, err)
	}
	if strings.Contains(first[0].Message, "private") || strings.Contains(first[0].Message, "13800138000") {
		t.Fatal("receipt leaked recipient or provider description")
	}
	changed, err := NormalizeReports([]byte(strings.Replace(string(batch), `"success":true`, `"success":false`, 1)), "instance-1")
	if err != nil || len(changed) != 1 || changed[0].EventID == first[0].EventID || changed[0].Status != "failed" {
		t.Fatalf("status transition did not get a new stable event: %+v err=%v", changed, err)
	}
}

func TestNormalizeReportsRejectsIntermediateOrMalformedReports(t *testing.T) {
	for _, body := range []string{
		`[{"phone_number":"13800138000","success":null,"biz_id":"biz-1","report_time":"2026-10-09 00:00:00"}]`,
		`[{"phone_number":"13800138000","success":true,"biz_id":"biz-1"}]`,
		`[{"phone_number":"13800138000","success":true,"biz_id":"biz-1","report_time":"bad"}]`,
		`[{"phone_number":"13800138000","success":true,"biz_id":"biz-1","report_time":"2026-10-09 00:00:00","private_field":"x"}]`,
		`{"arg":"unknown-envelope"}`,
		`[{"phone_number":"13800138000","success":true,"success":false,"biz_id":"biz-1","report_time":"2026-10-09 00:00:00"}]`,
	} {
		if _, err := NormalizeReports([]byte(body), "instance-1"); err == nil {
			t.Fatalf("invalid report accepted: %s", body)
		}
	}
}

func TestConsumeOneAcknowledgesOnlyAfterSinkAccepts(t *testing.T) {
	queue := &fakeQueue{message: QueueMessage{MessageID: "queue-1", AckHandle: "handle-one", Body: []byte(`[{"phone_number":"13800138000","success":true,"biz_id":"biz-1","report_time":"2026-10-09 00:00:00"}]`)}}
	sink := &fakeSink{err: errors.New("not durable yet")}
	if err := ConsumeOne(context.Background(), queue, sink, "instance-1"); err == nil || queue.acked {
		t.Fatalf("queue was acked before durable receipt: err=%v acked=%v", err, queue.acked)
	}
	sink.err = nil
	if err := ConsumeOne(context.Background(), queue, sink, "instance-1"); err != nil || !queue.acked || len(sink.receipts) != 1 {
		t.Fatalf("receipt was not durably accepted: err=%v acked=%v receipts=%d", err, queue.acked, len(sink.receipts))
	}
}

type fakeQueue struct {
	message   QueueMessage
	acked     bool
	released  int
	namespace string
}

func (q *fakeQueue) Pull(context.Context) (QueueMessage, error) { return q.message, nil }
func (q *fakeQueue) Ack(_ context.Context, handle string) error {
	if handle != q.message.AckHandle || handle == q.message.MessageID {
		return errors.New("unexpected acknowledgment handle")
	}
	q.acked = true
	return nil
}
func (q *fakeQueue) MakeVisible(_ context.Context, handle string, delay int) error {
	if handle != q.message.AckHandle || delay < 1 {
		return errors.New("unexpected visibility request")
	}
	q.released++
	return nil
}
func (q *fakeQueue) QueueNamespace() string {
	if q.namespace == "" {
		return "scope"
	}
	return q.namespace
}

type fakeSink struct {
	err      error
	receipts []sdk.Receipt
}

func (s *fakeSink) Accept(_ context.Context, receipt sdk.Receipt) error {
	if s.err != nil {
		return s.err
	}
	s.receipts = append(s.receipts, receipt)
	return nil
}
