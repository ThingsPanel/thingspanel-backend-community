package closedloop

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Rule struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TargetDevice string `json:"target_device_id"`
	TriggerKey   string `json:"trigger_key"`
	TriggerValue string `json:"trigger_value"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
}

type Notification struct {
	ID        string `json:"id"`
	RuleID    string `json:"rule_id"`
	DeviceID  string `json:"device_id"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

var global = &store{
	rules:         map[string]Rule{},
	notifications: []Notification{},
}

type store struct {
	mu            sync.Mutex
	ruleSeq       int
	notifSeq      int
	rules         map[string]Rule
	notifications []Notification
}

func CreateRule(name, deviceID, triggerKey, triggerValue string) Rule {
	global.mu.Lock()
	defer global.mu.Unlock()
	global.ruleSeq++
	id := "rule-" + strconv.Itoa(global.ruleSeq)
	r := Rule{
		ID:           id,
		Name:         strings.TrimSpace(name),
		TargetDevice: strings.TrimSpace(deviceID),
		TriggerKey:   strings.TrimSpace(triggerKey),
		TriggerValue: strings.TrimSpace(triggerValue),
		Enabled:      true,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	global.rules[id] = r
	return r
}

func SwitchRule(id string, enabled bool) (Rule, bool) {
	global.mu.Lock()
	defer global.mu.Unlock()
	r, ok := global.rules[id]
	if !ok {
		return Rule{}, false
	}
	r.Enabled = enabled
	global.rules[id] = r
	return r, true
}

func DeleteRule(id string) (Rule, bool) {
	global.mu.Lock()
	defer global.mu.Unlock()
	r, ok := global.rules[id]
	if !ok {
		return Rule{}, false
	}
	delete(global.rules, id)
	return r, true
}

func ListRules(deviceID string) []Rule {
	global.mu.Lock()
	defer global.mu.Unlock()
	out := make([]Rule, 0, len(global.rules))
	for _, r := range global.rules {
		if deviceID != "" && r.TargetDevice != deviceID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func NotificationTotal() int {
	global.mu.Lock()
	defer global.mu.Unlock()
	return len(global.notifications)
}

func ListNotifications(page, pageSize int) ([]Notification, int) {
	global.mu.Lock()
	defer global.mu.Unlock()
	total := len(global.notifications)
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	chunk := make([]Notification, end-start)
	copy(chunk, global.notifications[start:end])
	return chunk, total
}

func ProcessTelemetry(deviceID, key, value string) int {
	global.mu.Lock()
	defer global.mu.Unlock()
	delta := 0
	for _, r := range global.rules {
		if !r.Enabled {
			continue
		}
		if r.TargetDevice != deviceID {
			continue
		}
		if r.TriggerKey != key {
			continue
		}
		if r.TriggerValue != value {
			continue
		}
		global.notifSeq++
		nid := "notif-" + strconv.Itoa(global.notifSeq)
		global.notifications = append(global.notifications, Notification{
			ID:        nid,
			RuleID:    r.ID,
			DeviceID:  deviceID,
			Message:   fmt.Sprintf("rule %s triggered: %s=%s", r.Name, key, value),
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		delta++
	}
	return delta
}
