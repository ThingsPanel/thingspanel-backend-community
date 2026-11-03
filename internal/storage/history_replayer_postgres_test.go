package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

// Through the real Storage service: messages become intents in the writer's
// transaction and the replayer delivers them to the history table.
func TestStorageIntentModeDeliversHistoryThroughReplayer(t *testing.T) {
	f := newTelemetryBrokerFixture(t)
	migration, err := os.ReadFile("../../sql/history_intent/001_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(string(migration)).Error; err != nil {
		t.Fatal(err)
	}
	cfg := Config{ChannelBufferSize: 16, TelemetryBatchSize: 4, TelemetryFlushInterval: 50, HistoryMode: HistoryModeIntent,
		HistoryReplayIntervalMillis: 20, HistoryReplayBatch: 10}
	svc := New(f.db, telemetryTestLogger{}, cfg)
	in := make(chan *Message, 16)
	ctx, cancel := contextWithCancelForTest(t)
	defer cancel()
	if err := svc.Start(ctx, in); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	for i := 0; i < 12; i++ {
		in <- &Message{DeviceID: "dev-e2e", TenantID: "tenant-e2e", DataType: DataTypeTelemetry, Timestamp: now + int64(i),
			Data: []TelemetryDataPoint{{Key: "temp", Value: float64(i)}}}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var history, pending int64
		f.db.Raw(`SELECT count(*) FROM telemetry_datas WHERE device_id='dev-e2e'`).Scan(&history)
		f.db.Raw(`SELECT count(*) FROM telemetry_history_intents WHERE state='pending'`).Scan(&pending)
		if history == 12 && pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("history=%d pending=%d after deadline", history, pending)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var latest float64
	f.db.Raw(`SELECT number_v FROM telemetry_current_datas WHERE device_id='dev-e2e' AND key='temp'`).Scan(&latest)
	if latest != 11 {
		t.Fatalf("current snapshot=%v want 11", latest)
	}
	if err := svc.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestStorageDefaultModeHasNoReplayer(t *testing.T) {
	s := New(nil, telemetryTestLogger{}, DefaultConfig()).(*storage)
	if s.replayer != nil {
		t.Fatal("default mode must not start a replayer")
	}
	if _, ok := s.telemetryWriter.historyStore().(postgresHistoryStore); !ok {
		t.Fatal("default mode must use the transactional PostgreSQL store")
	}
}

func contextWithCancelForTest(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}
