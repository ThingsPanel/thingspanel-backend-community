// Package brokermapper owns versioned topic mapping inside the community backend.
// Broker plugins transport approved original bytes and never execute these rules.
package brokermapper

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	bc "project/internal/brokercontract"
	"sort"
	"strings"
)

var ErrUnmapped = errors.New("broker mapper: topic has no approved mapping")

type Rule struct {
	ID             int64  `json:"id"`
	Direction      string `json:"direction"`
	SourceTopic    string `json:"source_topic"`
	TargetTopic    string `json:"target_topic"`
	Priority       int32  `json:"priority"`
	DataIdentifier string `json:"data_identifier,omitempty"`
}
type Snapshot struct {
	Version string
	rules   []Rule
}

// Standard returns the built-in no-custom-mapping profile without persistence.
func Standard() *Snapshot { return &Snapshot{Version: "standard-v1", rules: []Rule{}} }

// Compile freezes deterministic ordering and rejects expressions that the finite
// mapper cannot represent. No regexp supplied by a device is ever evaluated.
func Compile(rules []Rule) (*Snapshot, error) {
	copyRules := append([]Rule(nil), rules...)
	sort.Slice(copyRules, func(i, j int) bool {
		if copyRules[i].Priority == copyRules[j].Priority {
			return copyRules[i].ID < copyRules[j].ID
		}
		return copyRules[i].Priority < copyRules[j].Priority
	})
	seen := map[int64]bool{}
	for _, r := range copyRules {
		if seen[r.ID] {
			return nil, errors.New("duplicate rule ID")
		}
		seen[r.ID] = true
		if r.Direction != "up" && r.Direction != "down" {
			return nil, errors.New("invalid mapping direction")
		}
		if err := validateTemplate(r.SourceTopic); err != nil {
			return nil, err
		}
		if err := validateTemplate(r.TargetTopic); err != nil {
			return nil, err
		}
		if r.Direction == "up" && !approvedUplink(r.TargetTopic) {
			return nil, errors.New("mapping target is not an approved uplink family")
		}
		if r.Direction == "down" && !approvedDownlink(r.TargetTopic) {
			return nil, errors.New("mapping target is not an approved downlink family")
		}
	}
	body, _ := json.Marshal(copyRules)
	sum := sha256.Sum256(body)
	return &Snapshot{Version: "mapping-" + hex.EncodeToString(sum[:]), rules: copyRules}, nil
}
func validateTemplate(t string) error {
	if t == "" || (strings.HasPrefix(t, "$") || bc.IsInternalResource(t)) || strings.Contains(t, "#") {
		return bc.ErrInvalidTopic
	}
	t = strings.ReplaceAll(t, "{device_number}", "+")
	t = strings.ReplaceAll(t, "{message_id}", "+")
	if strings.ContainsAny(t, "{}") {
		return errors.New("unknown mapping template variable")
	}
	return bc.ValidateSubscribeFilter(t)
}
func approvedUplink(t string) bool {
	for _, root := range []string{"devices/", "gateway/"} {
		for _, suffix := range []string{"telemetry", "attributes/+", "attributes/{message_id}", "event/+", "event/{message_id}", "command/response/+", "command/response/{message_id}", "attributes/set/response/+", "attributes/set/response/{message_id}"} {
			if t == root+suffix {
				return true
			}
		}
	}
	return false
}
func approvedDownlink(t string) bool {
	for _, root := range []string{"devices/", "gateway/"} {
		for _, suffix := range []string{"telemetry/control/{device_number}", "attributes/get/{device_number}", "attributes/set/{device_number}/+", "attributes/set/{device_number}/{message_id}", "command/{device_number}/+", "command/{device_number}/{message_id}", "attributes/response/{device_number}/+", "event/response/{device_number}/+"} {
			if t == root+suffix {
				return true
			}
		}
	}
	return t == "ota/devices/inform/{device_number}"
}
func match(pattern, actual, device string) (map[string]string, bool) {
	p, a := strings.Split(pattern, "/"), strings.Split(actual, "/")
	if len(p) != len(a) {
		return nil, false
	}
	vars := map[string]string{"device_number": device}
	wildcard := ""
	for i, x := range p {
		if a[i] == "" {
			return nil, false
		}
		switch x {
		case "{device_number}":
			if a[i] != device {
				return nil, false
			}
		case "{message_id}":
			if old := vars["message_id"]; old != "" && old != a[i] {
				return nil, false
			}
			vars["message_id"] = a[i]
		case "+":
			wildcard = a[i]
		default:
			if x != a[i] {
				return nil, false
			}
		}
	}
	if vars["message_id"] == "" {
		vars["message_id"] = wildcard
	}
	return vars, true
}
func render(t string, vars map[string]string) (string, error) {
	for k, v := range vars {
		t = strings.ReplaceAll(t, "{"+k+"}", v)
	}
	if strings.Contains(t, "+") {
		if vars["message_id"] == "" {
			return "", ErrUnmapped
		}
		t = strings.ReplaceAll(t, "+", vars["message_id"])
	}
	if strings.ContainsAny(t, "{}") {
		return "", ErrUnmapped
	}
	return t, bc.ValidatePublishTopic(t)
}
func (s *Snapshot) Up(source, deviceNumber string) (string, error) {
	if bc.ValidatePublishTopic(source) != nil || bc.ValidatePublishTopic(deviceNumber) != nil || strings.Contains(deviceNumber, "/") {
		return "", bc.ErrInvalidTopic
	}
	for _, r := range s.rules {
		if r.Direction != "up" {
			continue
		}
		if vars, ok := match(r.SourceTopic, source, deviceNumber); ok {
			return render(r.TargetTopic, vars)
		}
	}
	// Standard topics already are canonical; custom mappings cannot fall back to an
	// arbitrary internal/control topic.
	if approvedUplink(source) {
		return source, nil
	}
	parts := strings.Split(source, "/")
	if len(parts) > 2 {
		copyParts := append([]string(nil), parts...)
		copyParts[len(parts)-1] = "+"
		if approvedUplink(strings.Join(copyParts, "/")) {
			return source, nil
		}
	}
	return "", ErrUnmapped
}

// Down returns exactly one device wire topic, avoiding the old dual publication.
// A data_identifier rule sends the original JSON params bytes when its method matches.
func (s *Snapshot) Down(canonical, deviceNumber string, payload []byte) (string, []byte, error) {
	var fallback *Rule
	var fallbackVars map[string]string
	canonicalMatched := false
	for i := range s.rules {
		r := &s.rules[i]
		if r.Direction != "down" {
			continue
		}
		vars, ok := match(r.TargetTopic, canonical, deviceNumber)
		if !ok {
			continue
		}
		canonicalMatched = true
		if r.DataIdentifier == "" {
			if fallback == nil {
				fallback = r
				fallbackVars = vars
			}
			continue
		}
		var command struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(payload, &command) != nil || command.Method != r.DataIdentifier {
			continue
		}
		target, err := render(r.SourceTopic, vars)
		if err != nil {
			return "", nil, err
		}
		out := []byte(command.Params)
		if len(out) == 0 {
			out = []byte("{}")
		}
		return target, out, nil
	}
	if fallback != nil {
		t, e := render(fallback.SourceTopic, fallbackVars)
		return t, payload, e
	}
	if canonicalMatched {
		return "", nil, ErrUnmapped
	}
	if bc.ValidatePublishTopic(canonical) != nil {
		return "", nil, bc.ErrInvalidTopic
	}
	// Verify that the canonical downlink actually addresses this device.
	patterns := []string{"devices/telemetry/control/{device_number}", "devices/attributes/get/{device_number}", "devices/attributes/set/{device_number}/+", "devices/command/{device_number}/+", "devices/attributes/response/{device_number}/+", "devices/event/response/{device_number}/+", "gateway/telemetry/control/{device_number}", "gateway/attributes/get/{device_number}", "gateway/attributes/set/{device_number}/+", "gateway/command/{device_number}/+", "gateway/attributes/response/{device_number}/+", "gateway/event/response/{device_number}/+", "ota/devices/inform/{device_number}"}
	for _, p := range patterns {
		if _, ok := match(p, canonical, deviceNumber); ok {
			return canonical, payload, nil
		}
	}
	return "", nil, ErrUnmapped
}
func (s *Snapshot) Rules() []Rule { return append([]Rule(nil), s.rules...) }
