package dal

import (
	"testing"

	"project/internal/model"
)

var _ HistoryBackend = postgresHistoryBackend{}

type recordingHistory struct {
	HistoryBackend
	calls []string
}

func (r *recordingHistory) GetHistoryTelemetrData(deviceId, key string, _, _ int64) ([]*model.TelemetryData, error) {
	r.calls = append(r.calls, "history:"+deviceId+"/"+key)
	return nil, nil
}

func (r *recordingHistory) DeleteTelemetrData(deviceId, key string) error {
	r.calls = append(r.calls, "delete:"+deviceId+"/"+key)
	return nil
}

func TestHistoryBackendCanBeReplacedAndRestored(t *testing.T) {
	if _, ok := History.(postgresHistoryBackend); !ok {
		t.Fatalf("default backend must be PostgreSQL, got %T", History)
	}
	fake := &recordingHistory{}
	restore := SetHistoryBackend(fake)
	if _, err := History.GetHistoryTelemetrData("d1", "temp", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := History.DeleteTelemetrData("d1", "temp"); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.calls); got != 2 || fake.calls[0] != "history:d1/temp" || fake.calls[1] != "delete:d1/temp" {
		t.Fatalf("calls=%v", fake.calls)
	}
	restore()
	if _, ok := History.(postgresHistoryBackend); !ok {
		t.Fatalf("restore must reinstate the previous backend, got %T", History)
	}
}
