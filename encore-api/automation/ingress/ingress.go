// Package ingress turns the owner modules' uplink envelopes into normalized
// automation events.
//
// Three rules hold for every adapter here:
//
//  1. Decode and transform happen *before* this package. The values arriving
//     here are already post-transform: unit conversion and the thing-model
//     mapping were applied by the uplink pipeline. A rule therefore never
//     evaluates a raw payload, and the pipeline is never asked to transform
//     the same sample twice.
//
//  2. The event id comes from the source, never from the receive time. A broker
//     redelivery, a relay retry or a process restart all land on the same inbox
//     row instead of producing a second business action.
//
//  3. Attributes and instantaneous events are different things. An attribute is
//     a durable state value that can be read later; an event only means "this
//     happened now" and is never treated as a lasting condition.
package ingress

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"encore.app/automation/domain"
	"encore.app/automation/store"
)

// Class is the input class. The gateway_* variants exist so an explain view can
// tell gateway-relayed sub-device traffic apart from direct traffic without
// guessing from the device id.
type Class string

const (
	ClassTelemetry        Class = "telemetry"
	ClassAttribute        Class = "attribute"
	ClassEvent            Class = "event"
	ClassStatus           Class = "status"
	ClassAlarm            Class = "alarm"
	ClassGatewayTelemetry Class = "gateway_telemetry"
	ClassGatewayAttribute Class = "gateway_attribute"
	ClassGatewayEvent     Class = "gateway_event"
)

// Signal maps an input class to the evaluator's signal kind.
func (c Class) Signal() domain.SignalKind {
	switch c {
	case ClassTelemetry, ClassGatewayTelemetry:
		return domain.SignalTelemetry
	case ClassAttribute, ClassGatewayAttribute:
		return domain.SignalAttribute
	case ClassEvent, ClassGatewayEvent:
		return domain.SignalEvent
	case ClassStatus:
		return domain.SignalStatus
	case ClassAlarm:
		return domain.SignalAlarm
	}
	return ""
}

// IsGateway reports whether the class was relayed by a gateway.
func (c Class) IsGateway() bool {
	return strings.HasPrefix(string(c), "gateway_")
}

// Envelope is the shared shape every adapter accepts. It mirrors the community
// uplink message (type, device, timestamp, payload) plus the fields a relay
// adds on top.
type Envelope struct {
	TenantID string
	DeviceID string

	// GatewayID is set for gateway-relayed sub-device traffic.
	GatewayID string

	// MessageID is the source message id when the topic carries one
	// (devices/attributes/{id}, devices/event/{id}). It is the strongest
	// dedupe key available, so it is preferred over a content hash.
	MessageID string

	// ObservedAt is the source timestamp. Zero means "use the receive time",
	// which is recorded as such so an explain view can tell the two apart.
	ObservedAt time.Time

	// Values is the decoded, post-transform payload.
	Values map[string]any

	// Params carries event parameters. Only events use it.
	Params map[string]any

	// Units maps a key to the unit the pipeline reported for it.
	Units map[string]string

	// Quality is the source quality; empty means good.
	Quality string

	// Source labels the transport ("mqtt", "http", "relay").
	Source string

	// CausalRoot and CausalHop carry the alarm-driven chain, so a rule that is
	// started by an alarm transition can inherit the origin and be stopped at
	// the hop limit instead of looping.
	CausalRoot string
	CausalHop  int
}

// Normalize converts one envelope into inbox events.
//
// A telemetry or attribute envelope produces one event per key, because the
// candidate index is per (signal, key): bundling them would force a full rule
// scan for every sample.
func Normalize(class Class, env Envelope, receivedAt time.Time) ([]store.IngestEvent, error) {
	if strings.TrimSpace(env.TenantID) == "" {
		return nil, errors.New("tenant_id is required")
	}
	if strings.TrimSpace(env.DeviceID) == "" {
		return nil, errors.New("device_id is required")
	}
	if receivedAt.IsZero() {
		receivedAt = time.Now().UTC()
	}
	observed := env.ObservedAt.UTC()
	if env.ObservedAt.IsZero() {
		observed = receivedAt.UTC()
	}
	source := env.Source
	if source == "" {
		source = "uplink"
	}
	quality := domain.Quality(env.Quality)
	if quality == "" {
		quality = domain.QualityGood
	}

	switch class {
	case ClassTelemetry, ClassAttribute, ClassGatewayTelemetry, ClassGatewayAttribute:
		return normalizeValues(class, env, observed, source, quality)
	case ClassEvent, ClassGatewayEvent:
		return normalizeEvent(class, env, observed, source, quality)
	case ClassStatus:
		return normalizeStatus(env, observed, source, quality)
	case ClassAlarm:
		return normalizeAlarm(env, observed, source, quality)
	}
	return nil, fmt.Errorf("unsupported input class %q", class)
}

// normalizeValues emits one durable sample per key.
func normalizeValues(class Class, env Envelope, observed time.Time, source string, quality domain.Quality) ([]store.IngestEvent, error) {
	if len(env.Values) == 0 {
		return nil, errors.New("payload has no values")
	}
	keys := make([]string, 0, len(env.Values))
	for k := range env.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable output order keeps tests and logs readable

	out := make([]store.IngestEvent, 0, len(keys))
	for _, k := range keys {
		v := env.Values[k]
		out = append(out, store.IngestEvent{
			EventID:    valueEventID(class, env, k, v, observed),
			TenantID:   env.TenantID,
			DeviceID:   env.DeviceID,
			Signal:     class.Signal(),
			Key:        k,
			Value:      v,
			Unit:       env.Units[k],
			Quality:    quality,
			ObservedAt: observed,
			Source:     source,
			InputClass: string(class),
			GatewayID:  env.GatewayID,
			CausalRoot: env.CausalRoot,
			CausalHop:  env.CausalHop,
		})
	}
	return out, nil
}

// normalizeEvent emits a single instantaneous event.
//
// The key is the event identifier the device reported. The value is always the
// literal true: an event condition asks "did this happen in this sample", not
// "is this still true", so storing a payload as the value would invite exactly
// the mistake of reading an old event as a lasting state.
func normalizeEvent(class Class, env Envelope, observed time.Time, source string, quality domain.Quality) ([]store.IngestEvent, error) {
	key := firstString(env.Values, "identify", "identifier", "key", "event")
	if key == "" {
		return nil, errors.New("event payload has no identifier")
	}
	params := env.Params
	if params == nil {
		params = env.Values
	}
	base := "event"
	if env.MessageID != "" {
		base = "event\x1f" + env.MessageID
	} else {
		base = "event\x1f" + observed.Format(time.RFC3339Nano) + "\x1f" + canonical(params)
	}
	return []store.IngestEvent{{
		EventID:    StableID(class.String(), env.TenantID, env.DeviceID, key, base),
		TenantID:   env.TenantID,
		DeviceID:   env.DeviceID,
		Signal:     domain.SignalEvent,
		Key:        key,
		Value:      true,
		Quality:    quality,
		ObservedAt: observed,
		Params:     params,
		Source:     source,
		InputClass: string(class),
		GatewayID:  env.GatewayID,
		CausalRoot: env.CausalRoot,
		CausalHop:  env.CausalHop,
	}}, nil
}

// normalizeStatus emits one online/offline transition.
//
// The id includes the timestamp, so two genuine transitions stay distinct while
// the runtime's state comparison can still recognise a repeated "online" that
// carries no new information.
func normalizeStatus(env Envelope, observed time.Time, source string, quality domain.Quality) ([]store.IngestEvent, error) {
	online, ok := statusValue(env.Values)
	if !ok {
		return nil, errors.New("status payload is not online/offline")
	}
	return []store.IngestEvent{{
		EventID: StableID("status", env.TenantID, env.DeviceID,
			strconv.FormatBool(online), observed.Format(time.RFC3339Nano)),
		TenantID:   env.TenantID,
		DeviceID:   env.DeviceID,
		Signal:     domain.SignalStatus,
		Key:        "online",
		Value:      online,
		Quality:    quality,
		ObservedAt: observed,
		Source:     source,
		InputClass: string(ClassStatus),
		GatewayID:  env.GatewayID,
	}}, nil
}

// normalizeAlarm turns an alarm lifecycle transition into an input event.
//
// The transition id is authoritative: the alarm owner generates it once and
// republishes the same id on retry, so a redelivered transition can never start
// the same linked action twice.
func normalizeAlarm(env Envelope, observed time.Time, source string, quality domain.Quality) ([]store.IngestEvent, error) {
	kind := firstString(env.Values, "kind", "transition", "state")
	if kind == "" {
		return nil, errors.New("alarm payload has no transition kind")
	}
	transitionID := firstString(env.Values, "transition_id", "id")
	if transitionID == "" {
		// Without the owner's id we fall back to content plus time. This is
		// weaker, so it is recorded in the source label for the explain view.
		source = source + ":synthetic-transition-id"
		transitionID = observed.Format(time.RFC3339Nano) + "\x1f" + canonical(env.Values)
	}
	root := env.CausalRoot
	if root == "" {
		root = transitionID
	}
	return []store.IngestEvent{{
		EventID:    StableID("alarm", env.TenantID, env.DeviceID, kind, transitionID),
		TenantID:   env.TenantID,
		DeviceID:   env.DeviceID,
		Signal:     domain.SignalAlarm,
		Key:        kind,
		Value:      firstString(env.Values, "state", "kind"),
		Quality:    quality,
		ObservedAt: observed,
		Params:     env.Values,
		Source:     source,
		InputClass: string(ClassAlarm),
		CausalRoot: root,
		// The alarm owner republishes the hop of the run that raised it, so a
		// chain of alarm-triggered rules cannot outrun the limit.
		CausalHop: env.CausalHop + 1,
	}}, nil
}

// valueEventID prefers the source message id and falls back to a content hash.
//
// Including the timestamp is deliberate: two readings of 31.5 at different times
// are two pieces of evidence and both must reach the pending window, while the
// same reading replayed by the broker is one.
func valueEventID(class Class, env Envelope, key string, value any, observed time.Time) string {
	if env.MessageID != "" {
		return StableID(class.String(), env.TenantID, env.DeviceID, key, "mid", env.MessageID)
	}
	return StableID(class.String(), env.TenantID, env.DeviceID, key,
		observed.Format(time.RFC3339Nano), canonical(value))
}

// StableID builds a dedupe key from parts. The separator cannot occur in an id,
// so two different tuples can never collide by plain concatenation.
func StableID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0x1f})
	}
	return "evt_" + hex.EncodeToString(h.Sum(nil)[:16])
}

// canonical renders a value as stable JSON. encoding/json sorts map keys, so the
// same logical payload always produces the same text.
func canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// String returns the class as a plain string.
func (c Class) String() string { return string(c) }

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// statusValue accepts the shapes the community uplink actually produces: the
// MQTT topic carries "0"/"1" as text, the internal bus carries a bool, and an
// HTTP relay may carry either.
func statusValue(values map[string]any) (bool, bool) {
	if len(values) == 0 {
		return false, false
	}
	for _, k := range []string{"online", "status", "is_online"} {
		if v, ok := values[k]; ok {
			return coerceBool(v)
		}
	}
	// A single unnamed value is the legacy "0"/"1" payload.
	if len(values) == 1 {
		for _, v := range values {
			return coerceBool(v)
		}
	}
	return false, false
}

func coerceBool(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "true", "online", "on":
			return true, true
		case "0", "false", "offline", "off":
			return false, true
		}
	case float64:
		return t != 0, true
	case int:
		return t != 0, true
	case int64:
		return t != 0, true
	case json.Number:
		n, err := t.Int64()
		if err == nil {
			return n != 0, true
		}
	}
	return false, false
}
