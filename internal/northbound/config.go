package northbound

import (
	"sync/atomic"
	"time"
)

// Config is the Community-side northbound settings. It is loaded once from
// configuration by the application bootstrap and is safe for concurrent reads.
type Config struct {
	Enabled           bool
	EncoreBaseURL     string
	ServiceToken      string
	MaxBacklogEvents  int
	MaxBacklogBytes   int64
	MaxEventBytes     int
	RelayBatch        int
	RelayInterval     time.Duration
	SubscribeInterval time.Duration
	MaxAttempts       int
	Retention         time.Duration
	Lease             time.Duration
	// SourceProfile is what this deployment actually runs. It is reported to the
	// northbound service so the page can state the guarantee boundary instead of
	// implying one.
	SourceProfile string
}

var current atomic.Pointer[Config]

// DefaultConfig keeps the runtime safe when no configuration is present: the
// feature is off and nothing is written to the outbox.
func DefaultConfig() Config {
	return Config{
		Enabled:           false,
		EncoreBaseURL:     "",
		ServiceToken:      "",
		MaxBacklogEvents:  50000,
		MaxBacklogBytes:   64 << 20,
		MaxEventBytes:     256 << 10,
		RelayBatch:        100,
		RelayInterval:     time.Second,
		SubscribeInterval: 15 * time.Second,
		MaxAttempts:       20,
		Retention:         7 * 24 * time.Hour,
		Lease:             30 * time.Second,
		SourceProfile:     "legacy",
	}
}

// Configure replaces the active configuration. Called once during bootstrap.
func Configure(c Config) {
	if c.MaxBacklogEvents <= 0 {
		c.MaxBacklogEvents = 50000
	}
	if c.MaxBacklogBytes <= 0 {
		c.MaxBacklogBytes = 64 << 20
	}
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = 256 << 10
	}
	if c.RelayBatch <= 0 || c.RelayBatch > 500 {
		c.RelayBatch = 100
	}
	if c.RelayInterval <= 0 {
		c.RelayInterval = time.Second
	}
	if c.SubscribeInterval <= 0 {
		c.SubscribeInterval = 15 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 20
	}
	if c.Retention <= 0 {
		c.Retention = 7 * 24 * time.Hour
	}
	if c.Lease <= 0 {
		c.Lease = 30 * time.Second
	}
	current.Store(&c)
}

// Current returns the active configuration, or a disabled default.
func Current() Config {
	if c := current.Load(); c != nil {
		return *c
	}
	return DefaultConfig()
}

// Enabled reports whether the outbox should be written at all.
func Enabled() bool {
	c := current.Load()
	return c != nil && c.Enabled
}
