package metrics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestReportTelemetryCycleLifecycle(t *testing.T) {
	t.Cleanup(resetTelemetryConfig)

	var (
		mu      sync.Mutex
		events  []string
		payload []map[string]interface{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var body map[string]interface{}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		mu.Lock()
		defer mu.Unlock()
		events = append(events, body["event"].(string))
		payload = append(payload, body)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	viper.Set("telemetry.enabled", true)
	viper.Set("telemetry.posthog_key", "phc_test")
	viper.Set("telemetry.posthog_host", server.URL)
	viper.Set("telemetry.state_file", filepath.Join(tmpDir, ".telemetry_state.json"))
	viper.Set("telemetry.instance_id_file", filepath.Join(tmpDir, ".instance_id"))

	ins := &InstanceInfo{
		InstanceID:  "instance-1",
		DeviceCount: 12,
		UserCount:   3,
		Version:     "1.0.0",
		OS:          "linux",
		Arch:        "amd64",
		Timestamp:   1775800000,
	}

	firstAt := time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC)
	require.NoError(t, reportTelemetryCycleAt(ins, "startup", firstAt))

	mu.Lock()
	require.Equal(t, []string{EventInstanceRegistered, EventInstanceHeartbeat}, events)
	require.Equal(t, "instance-1", payload[0]["properties"].(map[string]interface{})["distinct_id"])
	require.NotEmpty(t, payload[0]["timestamp"])
	mu.Unlock()

	secondAt := firstAt.Add(30 * time.Minute)
	require.NoError(t, reportTelemetryCycleAt(ins, "heartbeat", secondAt))

	mu.Lock()
	require.Len(t, events, 2)
	mu.Unlock()

	ins.Version = "1.1.0"
	thirdAt := firstAt.Add(90 * time.Minute)
	require.NoError(t, reportTelemetryCycleAt(ins, "heartbeat", thirdAt))

	mu.Lock()
	require.Equal(t, []string{
		EventInstanceRegistered,
		EventInstanceHeartbeat,
		EventInstanceUpgraded,
		EventInstanceHeartbeat,
	}, events)
	require.Equal(t, "1.0.0", payload[2]["properties"].(map[string]interface{})["from_version"])
	require.Equal(t, "1.1.0", payload[2]["properties"].(map[string]interface{})["to_version"])
	mu.Unlock()
}

func resetTelemetryConfig() {
	keys := []string{
		"telemetry.enabled",
		"telemetry.posthog_key",
		"telemetry.posthog_host",
		"telemetry.state_file",
		"telemetry.instance_id_file",
	}

	for _, key := range keys {
		viper.Set(key, nil)
	}
}
