package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-basic/uuid"
)

type otaProgress struct {
	DeviceKey string
	Step      int
	Desc      string
}

func parseOTAProgressPayload(raw []byte) (otaProgress, error) {
	var wrapped struct {
		DeviceID string          `json:"device_id"`
		Values   json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return otaProgress{}, fmt.Errorf("invalid ota progress json: %w", err)
	}
	body := raw
	if len(wrapped.Values) > 0 {
		body = unwrapOTAProgressValues(wrapped.Values)
	}
	var inner struct {
		Step interface{} `json:"step"`
		Desc string      `json:"desc"`
	}
	if err := json.Unmarshal(body, &inner); err != nil {
		return otaProgress{}, fmt.Errorf("invalid ota progress values: %w", err)
	}
	if inner.Step == nil {
		var nested struct {
			Values struct {
				Step interface{} `json:"step"`
				Desc string      `json:"desc"`
			} `json:"values"`
		}
		if err := json.Unmarshal(body, &nested); err == nil && nested.Values.Step != nil {
			inner.Step = nested.Values.Step
			if inner.Desc == "" {
				inner.Desc = nested.Values.Desc
			}
		}
	}
	step, err := coerceOTAStep(inner.Step)
	if err != nil {
		return otaProgress{}, err
	}
	return otaProgress{DeviceKey: strings.TrimSpace(wrapped.DeviceID), Step: step, Desc: inner.Desc}, nil
}

func unwrapOTAProgressValues(raw json.RawMessage) []byte {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		return trimmed
	}
	var encoded string
	if err := json.Unmarshal(trimmed, &encoded); err != nil {
		return trimmed
	}
	if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
		return decoded
	}
	if json.Valid([]byte(encoded)) {
		return []byte(encoded)
	}
	return trimmed
}

func coerceOTAStep(v interface{}) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	default:
		return 0, fmt.Errorf("unsupported step type %T", v)
	}
}

func nextOTAStatusFromStep(step int) (status int16, ok bool) {
	switch {
	case step == -1, step == -2, step == -3, step == -4:
		return 5, true
	case step >= 1 && step < 100:
		return 3, true
	case step == 100:
		return 4, true
	default:
		return 0, false
	}
}

func otaInProgressStatuses() []int16 {
	return []int16{1, 2, 3}
}

type OTAProgressLog struct {
	ID           string
	TaskDetailID string
	DeviceID     string
	Step         *int16
	Status       int16
	Description  string
	Payload      string
	CreatedAt    string
}

func buildOTAProgressLog(detailID, deviceID string, progress otaProgress, status int16, payload string) OTAProgressLog {
	step := int16(progress.Step)
	return OTAProgressLog{
		ID:           uuid.New(),
		TaskDetailID: detailID,
		DeviceID:     deviceID,
		Step:         &step,
		Status:       status,
		Description:  progress.Desc,
		Payload:      payload,
	}
}
