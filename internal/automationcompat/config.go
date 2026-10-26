// Package automationcompat is the thin adapter that keeps the App, the old
// panel and ThingsVis working while the automation engine is replaced (A30).
//
// What it is: a translation of the *old* HTTP contract onto the single new
// implementation. Every handler here ends up in one of four new endpoints —
// resolve a legacy id, read a rule, run a rule, flip a rule's switch. There is
// no second evaluator, no second scheduler and no second command dispatcher in
// this package, and there must never be one: two code paths that can move the
// same relay is the failure A28 exists to prevent.
//
// What it is not: a place to keep the old behaviour alive. When the old and the
// new disagree, the new one is right and the adapter says so in the payload
// (see translate.go, where "unknown" is not folded into "success" just because
// the old enum had two values).
//
// Every call is counted per consumer and per endpoint (metrics.go), and the
// whole surface carries an expiry date (config.go). Those two things are what
// make the exit condition measurable instead of a promise: when the counters for
// a consumer stay at zero for a release, its adapter can be deleted, and after
// the sunset date it refuses to serve at all.
package automationcompat

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// Mode says how the adapter is mounted.
type Mode string

const (
	// ModeOff means the adapter is not mounted at all. This is the default: the
	// old engine is still serving the old paths, and mounting a second writer
	// next to it would double-execute every scene activation.
	ModeOff Mode = "off"
	// ModeShadow mounts the adapter on /api/v1/compat/... so it can be replayed
	// against real traffic without taking the old paths over. This is what the
	// consumer tests and the smoke script use.
	ModeShadow Mode = "shadow"
	// ModeTakeover mounts the adapter on the real legacy paths, in front of the
	// old handlers. A33 flips this, not A30.
	ModeTakeover Mode = "takeover"
)

// Config is the adapter's runtime configuration.
type Config struct {
	Mode Mode
	// CoreBaseURL is the new automation service, e.g. http://automation:4000.
	CoreBaseURL string
	// Sunset is the last day the adapter answers. It is required: a compatibility
	// layer without an end date is a second implementation that nobody removes.
	Sunset time.Time
	// Timeout bounds one upstream call. The old clients already time out at 10s;
	// this must be shorter so the adapter can answer with a reason instead of
	// hanging until the client gives up.
	Timeout time.Duration
}

// Env names, spelled out so deployment manifests and the ledger agree.
const (
	EnvMode     = "AUTOMATION_COMPAT_MODE"
	EnvCoreURL  = "AUTOMATION_CORE_BASE_URL"
	EnvSunset   = "AUTOMATION_COMPAT_SUNSET"
	EnvTimeout  = "AUTOMATION_COMPAT_TIMEOUT"
	DefaultMode = ModeOff
)

// ConfigFromEnv reads the adapter configuration.
//
// It refuses to build a half-configured adapter: a mode other than "off"
// without an upstream URL or without a sunset date is a configuration mistake
// that would otherwise surface as a 500 on a customer's phone.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Mode:    Mode(strings.ToLower(strings.TrimSpace(envOr(EnvMode, string(DefaultMode))))),
		Timeout: 5 * time.Second,
	}
	switch cfg.Mode {
	case ModeOff:
		return cfg, nil
	case ModeShadow, ModeTakeover:
	default:
		return cfg, fmt.Errorf("%s=%q is not one of off|shadow|takeover", EnvMode, cfg.Mode)
	}

	raw := strings.TrimSpace(os.Getenv(EnvCoreURL))
	if raw == "" {
		return cfg, fmt.Errorf("%s is required when %s=%s", EnvCoreURL, EnvMode, cfg.Mode)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return cfg, fmt.Errorf("%s=%q is not an absolute URL", EnvCoreURL, raw)
	}
	cfg.CoreBaseURL = strings.TrimRight(raw, "/")

	sunset := strings.TrimSpace(os.Getenv(EnvSunset))
	if sunset == "" {
		return cfg, fmt.Errorf("%s is required: the adapter must have an end date", EnvSunset)
	}
	t, err := parseSunset(sunset)
	if err != nil {
		return cfg, err
	}
	cfg.Sunset = t

	if rawTimeout := strings.TrimSpace(os.Getenv(EnvTimeout)); rawTimeout != "" {
		d, err := time.ParseDuration(rawTimeout)
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("%s=%q is not a positive duration", EnvTimeout, rawTimeout)
		}
		cfg.Timeout = d
	}
	return cfg, nil
}

// parseSunset accepts a date or an RFC3339 instant. A bare date means the whole
// day is still served, so the adapter stops at the start of the next day.
func parseSunset(v string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if d, err := time.Parse("2006-01-02", v); err == nil {
		return d.Add(24 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("%s=%q is neither YYYY-MM-DD nor RFC3339", EnvSunset, v)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
