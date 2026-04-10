package metrics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const defaultHeartbeatInterval = time.Hour

type TelemetryState struct {
	RegisteredAt        string `json:"registered_at,omitempty"`
	LastHeartbeatAt     string `json:"last_heartbeat_at,omitempty"`
	LastHeartbeatBucket string `json:"last_heartbeat_bucket,omitempty"`
	LastVersion         string `json:"last_version,omitempty"`
}

func TelemetryEnabled() bool {
	return viper.GetBool("telemetry.enabled")
}

func HeartbeatInterval() time.Duration {
	raw := strings.TrimSpace(viper.GetString("telemetry.heartbeat_interval"))
	if raw == "" {
		return defaultHeartbeatInterval
	}

	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return defaultHeartbeatInterval
	}

	return interval
}

func ReportTelemetryCycle(ins *InstanceInfo, trigger string) error {
	return reportTelemetryCycleAt(ins, trigger, time.Now().UTC())
}

func reportTelemetryCycleAt(ins *InstanceInfo, trigger string, now time.Time) error {
	if !TelemetryEnabled() {
		return nil
	}

	if strings.TrimSpace(ins.InstanceID) == "" {
		return errors.New("instance_id is empty")
	}

	state, err := loadTelemetryState()
	if err != nil {
		return err
	}

	if state.RegisteredAt == "" {
		if err := sendTelemetryEvent(EventInstanceRegistered, ins, trigger, now, nil); err != nil {
			return err
		}
		state.RegisteredAt = now.Format(time.RFC3339Nano)
		state.LastVersion = ins.Version
		if err := saveTelemetryState(state); err != nil {
			return err
		}
	}

	if state.LastVersion != "" && state.LastVersion != ins.Version {
		if err := sendTelemetryEvent(EventInstanceUpgraded, ins, trigger, now, map[string]interface{}{
			"from_version": state.LastVersion,
			"to_version":   ins.Version,
		}); err != nil {
			return err
		}
		state.LastVersion = ins.Version
		if err := saveTelemetryState(state); err != nil {
			return err
		}
	}

	currentBucket := heartbeatBucket(now, HeartbeatInterval())
	if state.LastHeartbeatBucket != currentBucket {
		if err := sendTelemetryEvent(EventInstanceHeartbeat, ins, trigger, now, nil); err != nil {
			return err
		}
		state.LastHeartbeatAt = now.Format(time.RFC3339Nano)
		state.LastHeartbeatBucket = currentBucket
		state.LastVersion = ins.Version
		if err := saveTelemetryState(state); err != nil {
			return err
		}
	}

	return nil
}

func sendTelemetryEvent(event string, ins *InstanceInfo, trigger string, now time.Time, extra map[string]interface{}) error {
	properties := baseTelemetryProperties(ins, event, trigger, now)
	for key, value := range extra {
		properties[key] = value
	}

	payload := map[string]interface{}{
		"event":      event,
		"timestamp":  now.Format(time.RFC3339Nano),
		"properties": properties,
	}

	return capturePayload(payload)
}

func baseTelemetryProperties(ins *InstanceInfo, event string, trigger string, now time.Time) map[string]interface{} {
	properties := map[string]interface{}{
		"distinct_id":              ins.InstanceID,
		"app_version":              ins.Version,
		"device_count":             ins.DeviceCount,
		"user_count":               ins.UserCount,
		"os":                       ins.OS,
		"arch":                     ins.Arch,
		"edition":                  TelemetryEdition,
		"telemetry_source":         TelemetrySource,
		"telemetry_schema_version": TelemetrySchemaVersion,
		"telemetry_trigger":        trigger,
		"reported_at_unix":         ins.Timestamp,
		"$lib":                     "thingspanel-go-client",
		"$set": map[string]interface{}{
			"current_version":      ins.Version,
			"current_os":           ins.OS,
			"current_arch":         ins.Arch,
			"current_device_count": ins.DeviceCount,
			"current_user_count":   ins.UserCount,
			"last_telemetry_at":    now.Format(time.RFC3339Nano),
			"last_telemetry_event": event,
			"edition":              TelemetryEdition,
			"telemetry_source":     TelemetrySource,
		},
		"$set_once": map[string]interface{}{
			"first_seen_at":    now.Format(time.RFC3339Nano),
			"initial_version":  ins.Version,
			"initial_os":       ins.OS,
			"initial_arch":     ins.Arch,
			"edition":          TelemetryEdition,
			"telemetry_source": TelemetrySource,
			"telemetry_schema": TelemetrySchemaVersion,
		},
	}

	return properties
}

func heartbeatBucket(now time.Time, interval time.Duration) string {
	return now.Truncate(interval).Format(time.RFC3339Nano)
}

func loadTelemetryState() (*TelemetryState, error) {
	statePath := telemetryStatePath()
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &TelemetryState{}, nil
		}
		return nil, err
	}

	state := &TelemetryState{}
	if len(data) == 0 {
		return state, nil
	}

	if err := json.Unmarshal(data, state); err != nil {
		return nil, err
	}

	return state, nil
}

func saveTelemetryState(state *TelemetryState) error {
	statePath := telemetryStatePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, statePath)
}

func telemetryStatePath() string {
	if configured := strings.TrimSpace(viper.GetString("telemetry.state_file")); configured != "" {
		return configured
	}
	return filepath.Join("configs", ".telemetry_state.json")
}
