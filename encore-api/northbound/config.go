package northbound

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

// config is read from the process environment. Encore injects `encore secret`
// values as environment variables, so the same keys work for local runs and for
// a released deployment. Nothing here is read from the database, and no value
// is ever logged.
type config struct {
	// ServiceToken authenticates the Community runtime on /inbox/events.
	// Empty means the internal API is closed (fail closed, not open).
	ServiceToken string
	// SecretKey is the AES-256 key material for tenant credentials.
	SecretKey string
	// Connectors is the fixed official plugin set for this release.
	Connectors []connectorConfig
	// Retention and capacity.
	EventRetention    time.Duration
	DeliveryRetention time.Duration
	MaxBacklogEvents  int64
	MaxBatchEvents    int
	MaxEventBytes     int
	Lease             time.Duration
	MaxAttempts       int
	Workers           int
}

type connectorConfig struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Endpoint string `json:"endpoint"`
	Digest   string `json:"digest"`
	Token    string `json:"token"`
	Disabled bool   `json:"disabled"`
}

func loadConfig() config {
	c := config{
		ServiceToken:      strings.TrimSpace(os.Getenv("NB_SERVICE_TOKEN")),
		SecretKey:         strings.TrimSpace(os.Getenv("NB_SECRET_KEY")),
		EventRetention:    envDuration("NB_EVENT_RETENTION", 7*24*time.Hour),
		DeliveryRetention: envDuration("NB_DELIVERY_RETENTION", 30*24*time.Hour),
		MaxBacklogEvents:  envInt64("NB_MAX_BACKLOG_EVENTS", 20000),
		MaxBatchEvents:    int(envInt64("NB_MAX_BATCH_EVENTS", 200)),
		MaxEventBytes:     int(envInt64("NB_MAX_EVENT_BYTES", 256*1024)),
		Lease:             envDuration("NB_LEASE", 30*time.Second),
		MaxAttempts:       int(envInt64("NB_MAX_ATTEMPTS", 12)),
		Workers:           int(envInt64("NB_WORKERS", 8)),
	}
	c.Connectors = loadConnectors()
	return c
}

// loadConnectors reads the fixed official plugin list. A release ships this as
// one environment value; it is not a plugin marketplace and it does not accept
// arbitrary URLs from tenants.
func loadConnectors() []connectorConfig {
	raw := strings.TrimSpace(os.Getenv("NB_CONNECTORS"))
	if raw == "" {
		return []connectorConfig{
			{
				ID:       "io.thingspanel.sink.http",
				Version:  "1.0.0",
				Endpoint: envDefault("NB_HTTP_PLUGIN_ENDPOINT", "http://127.0.0.1:18081"),
				Digest:   strings.TrimSpace(os.Getenv("NB_HTTP_PLUGIN_DIGEST")),
				Token:    strings.TrimSpace(os.Getenv("NB_HTTP_PLUGIN_TOKEN")),
			},
			{
				ID:       "io.thingspanel.sink.mqtt",
				Version:  "1.0.0",
				Endpoint: envDefault("NB_MQTT_PLUGIN_ENDPOINT", "http://127.0.0.1:18082"),
				Digest:   strings.TrimSpace(os.Getenv("NB_MQTT_PLUGIN_DIGEST")),
				Token:    strings.TrimSpace(os.Getenv("NB_MQTT_PLUGIN_TOKEN")),
			},
		}
	}
	var parsed []connectorConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	return parsed
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
