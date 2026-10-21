package app

import (
	"time"

	"project/internal/northbound"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// WithNorthboundSource wires the Community side of the northbound handoff.
//
// It does two things and nothing else:
//   - read the northbound settings once, so the in-transaction enrolment path
//     knows whether it is allowed to write a row at all;
//   - start the relay that moves enrolled rows to the northbound service.
//
// The enrolment itself is not a service: it runs inside the existing telemetry
// and attribute transactions, which is the whole point of the design. Turning
// the feature off leaves those transactions untouched, because every call
// checks Enabled() before doing any work.
func WithNorthboundSource() Option {
	return func(a *Application) error {
		cfg := northbound.DefaultConfig()

		if viper.IsSet("northbound.enabled") {
			cfg.Enabled = viper.GetBool("northbound.enabled")
		}
		if v := viper.GetString("northbound.encore_base_url"); v != "" {
			cfg.EncoreBaseURL = v
		}
		if v := viper.GetString("northbound.service_token"); v != "" {
			cfg.ServiceToken = v
		}
		if v := viper.GetString("northbound.source_profile"); v != "" {
			cfg.SourceProfile = v
		}
		if v := viper.GetInt("northbound.max_backlog_events"); v > 0 {
			cfg.MaxBacklogEvents = v
		}
		if v := viper.GetInt64("northbound.max_backlog_bytes"); v > 0 {
			cfg.MaxBacklogBytes = v
		}
		if v := viper.GetInt("northbound.max_event_bytes"); v > 0 {
			cfg.MaxEventBytes = v
		}
		if v := viper.GetInt("northbound.relay_batch"); v > 0 {
			cfg.RelayBatch = v
		}
		if v := viper.GetInt("northbound.max_attempts"); v > 0 {
			cfg.MaxAttempts = v
		}
		if d, ok := durationSetting("northbound.relay_interval"); ok {
			cfg.RelayInterval = d
		}
		if d, ok := durationSetting("northbound.subscribe_interval"); ok {
			cfg.SubscribeInterval = d
		}
		if d, ok := durationSetting("northbound.retention"); ok {
			cfg.Retention = d
		}
		if d, ok := durationSetting("northbound.lease"); ok {
			cfg.Lease = d
		}

		// An enabled handoff with no destination is a misconfiguration, not a
		// silent no-op: it would enrol rows forever and never move them.
		if cfg.Enabled && (cfg.EncoreBaseURL == "" || cfg.ServiceToken == "") {
			logrus.Warn("北向服务已启用，但未配置 northbound.encore_base_url 或 northbound.service_token，" +
				"登记的数据将留在本地 Outbox 中不会被交接")
		}

		northbound.Configure(cfg)
		logrus.Infof("北向源端配置：enabled=%v profile=%s base=%s", cfg.Enabled, cfg.SourceProfile, cfg.EncoreBaseURL)

		a.RegisterService(&northboundSourceService{db: a.DB})
		return nil
	}
}

// durationSetting accepts both "1s" and a bare millisecond number, matching how
// the rest of the configuration file is written.
func durationSetting(key string) (time.Duration, bool) {
	raw := viper.Get(key)
	if raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d, true
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Millisecond, true
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Millisecond, true
		}
	}
	return 0, false
}

type northboundSourceService struct {
	db    *gorm.DB
	relay *northbound.Relay
}

func (s *northboundSourceService) Name() string { return "northbound-source" }

func (s *northboundSourceService) Start() error {
	s.relay = northbound.Start(s.db, logrus.StandardLogger())
	return nil
}

func (s *northboundSourceService) Stop() error {
	if s.relay != nil {
		s.relay.Stop()
	}
	return nil
}
