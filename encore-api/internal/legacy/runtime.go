package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Typed runtime reads used by the native v2 device runtime adapter. They keep the
// legacy JSON value tokens (json.RawMessage) instead of stringifying them.

type legacyEnvelope struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func decodeEnvelope(raw json.RawMessage) (json.RawMessage, error) {
	var env legacyEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("legacy response is not a JSON envelope: %w", err)
	}
	if env.Code != 200 {
		return nil, fmt.Errorf("%w: legacy response code=%d", ErrRejected, env.Code)
	}
	return env.Data, nil
}

// GetOnlineFlag returns the legacy is_online flag (0 offline, 1 online).
func (c *Client) GetOnlineFlag(ctx context.Context, deviceID string) (int, error) {
	raw, err := c.GetDeviceOnlineStatus(ctx, deviceID)
	if err != nil {
		return 0, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return 0, err
	}
	var out map[string]int
	if err := json.Unmarshal(data, &out); err != nil {
		return 0, fmt.Errorf("legacy online status has an unexpected shape: %w", err)
	}
	flag, ok := out["is_online"]
	if !ok || (flag != 0 && flag != 1) {
		return 0, errors.New("legacy online status is missing a valid is_online flag")
	}
	return flag, nil
}

// CurrentReading is one latest-telemetry row. Value is the original JSON token
// (number, boolean or string), or "null" when the legacy row carried no value.
type CurrentReading struct {
	Key   string
	Ts    time.Time
	Value json.RawMessage
}

func (c *Client) GetTelemetryCurrentReadings(ctx context.Context, deviceID string) ([]CurrentReading, error) {
	return c.currentReadings(ctx, "/api/v1/telemetry/datas/current/", deviceID)
}

// GetAttributeCurrentReadings returns the latest stored value of every attribute key.
func (c *Client) GetAttributeCurrentReadings(ctx context.Context, deviceID string) ([]CurrentReading, error) {
	return c.currentReadings(ctx, "/api/v1/attribute/datas/", deviceID)
}

func (c *Client) currentReadings(ctx context.Context, prefix, deviceID string) ([]CurrentReading, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := c.doRaw(ctx, "GET", prefix+url.PathEscape(deviceID), token, nil)
	if err != nil {
		return nil, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Key   string          `json:"key"`
		Ts    time.Time       `json:"ts"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("legacy current readings have an unexpected shape: %w", err)
	}
	out := make([]CurrentReading, 0, len(rows))
	for _, row := range rows {
		if row.Key == "" || row.Ts.IsZero() {
			return nil, errors.New("legacy current reading row is missing key or timestamp")
		}
		value := row.Value
		if len(value) == 0 {
			value = json.RawMessage("null")
		}
		out = append(out, CurrentReading{Key: row.Key, Ts: row.Ts, Value: value})
	}
	return out, nil
}

// EventRecord is one reported event row.
type EventRecord struct {
	ID       string
	Identify string
	T        time.Time
	Datum    *string
}

func (c *Client) ListEventDatas(ctx context.Context, deviceID, identify string, page, pageSize int) ([]EventRecord, int64, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	if identify != "" {
		q.Set("identify", identify)
	}
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	raw, err := c.doRaw(ctx, "GET", "/api/v1/event/datas?"+q.Encode(), token, nil)
	if err != nil {
		return nil, 0, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, 0, err
	}
	var body struct {
		Count int64 `json:"count"`
		List  []struct {
			ID       string    `json:"id"`
			Identify string    `json:"identify"`
			T        time.Time `json:"ts"`
			Datum    *string   `json:"data"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, 0, fmt.Errorf("legacy event list has an unexpected shape: %w", err)
	}
	out := make([]EventRecord, 0, len(body.List))
	for _, row := range body.List {
		if row.ID == "" || row.Identify == "" || row.T.IsZero() {
			return nil, 0, errors.New("legacy event row is missing id, identify or timestamp")
		}
		out = append(out, EventRecord{ID: row.ID, Identify: row.Identify, T: row.T, Datum: row.Datum})
	}
	return out, body.Count, nil
}

// SubmitCommand publishes a command through the Community downlink and returns the
// message_id that identifies it in the status endpoint. value is the JSON text of the
// command parameters ("" sends no params). A non-empty idempotencyKey makes a repeated
// submission return the same message_id instead of sending the command again.
func (c *Client) SubmitCommand(ctx context.Context, deviceID, identify, value, idempotencyKey string) (string, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return "", err
	}
	body := struct {
		DeviceID       string  `json:"device_id"`
		Identify       string  `json:"identify"`
		Value          *string `json:"value,omitempty"`
		IdempotencyKey string  `json:"idempotency_key,omitempty"`
	}{DeviceID: deviceID, Identify: identify, IdempotencyKey: idempotencyKey}
	if value != "" {
		body.Value = &value
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/command/datas/pub", token, body)
	if err != nil {
		return "", err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return "", err
	}
	var out struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.MessageID == "" {
		return "", errors.New("legacy command response is missing message_id")
	}
	return out.MessageID, nil
}

// CommandStatus is the Community delivery status of one command. Status is one of
// accepted, published, publish_failed, device_succeeded, device_failed, timeout,
// not_found or unknown.
type CommandStatus struct {
	Status       string
	ErrorMessage *string
}

func (c *Client) GetCommandStatus(ctx context.Context, messageID string) (CommandStatus, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return CommandStatus{}, err
	}
	raw, err := c.doRaw(ctx, "GET", "/api/v1/command/datas/status/"+url.PathEscape(messageID), token, nil)
	if err != nil {
		return CommandStatus{}, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return CommandStatus{}, err
	}
	var out struct {
		MessageID    string  `json:"message_id"`
		Status       string  `json:"status"`
		ErrorMessage *string `json:"error_message"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Status == "" || out.MessageID != messageID {
		return CommandStatus{}, errors.New("legacy command status has an unexpected shape")
	}
	return CommandStatus{Status: out.Status, ErrorMessage: out.ErrorMessage}, nil
}

// StatusChange is one stored online/offline transition.
type StatusChange struct {
	ID         int64
	Online     bool
	ChangeTime time.Time
}

// ListStatusHistory pages the stored transitions, newest first. fromSec filters
// change_time >= fromSec (whole seconds); status nil means both.
func (c *Client) ListStatusHistory(ctx context.Context, deviceID string, page, pageSize int, fromSec *int64, online *bool) ([]StatusChange, int64, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	if fromSec != nil {
		q.Set("start_time", strconv.FormatInt(*fromSec, 10))
	}
	if online != nil {
		if *online {
			q.Set("status", "1")
		} else {
			q.Set("status", "0")
		}
	}
	raw, err := c.doRaw(ctx, "GET", "/api/v1/device/status/history?"+q.Encode(), token, nil)
	if err != nil {
		return nil, 0, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, 0, err
	}
	var body struct {
		Total int64 `json:"total"`
		List  []struct {
			ID         int64     `json:"id"`
			Status     *int      `json:"status"`
			ChangeTime time.Time `json:"change_time"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, 0, fmt.Errorf("legacy status history has an unexpected shape: %w", err)
	}
	out := make([]StatusChange, 0, len(body.List))
	for _, row := range body.List {
		if row.ID <= 0 || row.Status == nil || (*row.Status != 0 && *row.Status != 1) || row.ChangeTime.IsZero() {
			return nil, 0, errors.New("legacy status history row is missing id, status or time")
		}
		out = append(out, StatusChange{ID: row.ID, Online: *row.Status == 1, ChangeTime: row.ChangeTime})
	}
	return out, body.Total, nil
}

// DebugState is the single per-device debug switch Community keeps in Redis.
type DebugState struct {
	Enabled  bool
	ExpireAt int64
}

func decodeDebugState(raw json.RawMessage) (DebugState, error) {
	data, err := decodeEnvelope(raw)
	if err != nil {
		return DebugState{}, err
	}
	var body struct {
		Enabled  *bool  `json:"enabled"`
		ExpireAt *int64 `json:"expire_at"`
	}
	if err := json.Unmarshal(data, &body); err != nil || body.Enabled == nil || body.ExpireAt == nil {
		return DebugState{}, errors.New("legacy debug status has an unexpected shape")
	}
	return DebugState{Enabled: *body.Enabled, ExpireAt: *body.ExpireAt}, nil
}

func (c *Client) GetDebugState(ctx context.Context, deviceID string) (DebugState, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return DebugState{}, err
	}
	raw, err := c.doRaw(ctx, "GET", "/api/v1/device/"+url.PathEscape(deviceID)+"/debug/status", token, nil)
	if err != nil {
		return DebugState{}, err
	}
	return decodeDebugState(raw)
}

// SetDebug turns the debug switch on until expireAt (unix seconds) or off. A payload
// limit of 0 keeps raw device payloads out of the stored log entries.
func (c *Client) SetDebug(ctx context.Context, deviceID string, enabled bool, expireAt int64) (DebugState, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return DebugState{}, err
	}
	body := map[string]any{"enabled": enabled}
	if enabled {
		body["expire_at"] = expireAt
		body["payload_max_bytes"] = 0
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/device/"+url.PathEscape(deviceID)+"/debug", token, body)
	if err != nil {
		return DebugState{}, err
	}
	return decodeDebugState(raw)
}

// DebugEntry is one stored debug log entry. Payload is deliberately not decoded.
type DebugEntry struct {
	Raw       string
	Time      time.Time
	Direction string
	Action    string
	Outcome   string
	Error     string
}

func (c *Client) ListDebugLogs(ctx context.Context, deviceID string, offset, limit int) ([]DebugEntry, int64, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("offset", itoa(offset))
	q.Set("limit", itoa(limit))
	raw, err := c.doRaw(ctx, "GET", "/api/v1/device/"+url.PathEscape(deviceID)+"/debug/logs?"+q.Encode(), token, nil)
	if err != nil {
		return nil, 0, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, 0, err
	}
	var body struct {
		Total int64             `json:"total"`
		List  []json.RawMessage `json:"list"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, 0, fmt.Errorf("legacy debug logs have an unexpected shape: %w", err)
	}
	out := make([]DebugEntry, 0, len(body.List))
	for _, item := range body.List {
		var e struct {
			Ts        string `json:"ts"`
			Direction string `json:"direction"`
			Action    string `json:"action"`
			Outcome   string `json:"outcome"`
			Error     string `json:"error"`
		}
		if err := json.Unmarshal(item, &e); err != nil {
			return nil, 0, errors.New("legacy debug log entry is not an object")
		}
		parsed, err := time.Parse(time.RFC3339Nano, e.Ts)
		if err != nil {
			return nil, 0, errors.New("legacy debug log entry has no valid timestamp")
		}
		out = append(out, DebugEntry{Raw: string(item), Time: parsed, Direction: e.Direction, Action: e.Action, Outcome: e.Outcome, Error: e.Error})
	}
	return out, body.Total, nil
}

// ExpectedRecord is one Community "send when the device comes online" message.
type ExpectedRecord struct {
	ID     string
	Status string // pending, sent or expired
}

// CreateExpectedCommand queues a command that Community sends when the device is
// online (immediately if it already is). params is the JSON text of the parameter
// object ("" for none).
func (c *Client) CreateExpectedCommand(ctx context.Context, deviceID, identify, params string, expiry *time.Time, label *string) (ExpectedRecord, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return ExpectedRecord{}, err
	}
	body := map[string]any{"device_id": deviceID, "send_type": "command", "identify": identify}
	if params != "" {
		body["payload"] = params
	}
	if expiry != nil {
		body["expiry"] = expiry.UTC().Format(time.RFC3339)
	}
	if label != nil {
		body["label"] = *label
	}
	raw, err := c.doRaw(ctx, "POST", "/api/v1/expected/data", token, body)
	if err != nil {
		return ExpectedRecord{}, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return ExpectedRecord{}, err
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.ID == "" || out.Status == "" {
		return ExpectedRecord{}, errors.New("legacy expected message response is missing id or status")
	}
	return ExpectedRecord{ID: out.ID, Status: out.Status}, nil
}

// ListExpectedCommands returns up to pageSize recent expected command messages.
func (c *Client) ListExpectedCommands(ctx context.Context, deviceID string, page, pageSize int) ([]ExpectedRecord, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("send_type", "command")
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	raw, err := c.doRaw(ctx, "GET", "/api/v1/expected/data/list?"+q.Encode(), token, nil)
	if err != nil {
		return nil, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, err
	}
	var body struct {
		List []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("legacy expected message list has an unexpected shape: %w", err)
	}
	out := make([]ExpectedRecord, 0, len(body.List))
	for _, row := range body.List {
		if row.ID == "" || row.Status == "" {
			return nil, errors.New("legacy expected message row is missing id or status")
		}
		out = append(out, ExpectedRecord{ID: row.ID, Status: row.Status})
	}
	return out, nil
}

// DeleteExpected removes a queued expected message. Community gives no way to tell a
// message that was just sent from one that was removed in time, so callers must check
// the status immediately before.
func (c *Client) DeleteExpected(ctx context.Context, id string) error {
	token, err := c.Login(ctx)
	if err != nil {
		return err
	}
	raw, err := c.doRaw(ctx, "DELETE", "/api/v1/expected/data/"+url.PathEscape(id), token, nil)
	if err != nil {
		return err
	}
	_, err = decodeEnvelope(raw)
	return err
}

// HistoryReading is one stored telemetry sample.
type HistoryReading struct {
	Key   string
	Ts    time.Time
	Value json.RawMessage
}

// ListTelemetryHistory pages one key's stored samples, newest first, for the
// inclusive millisecond range [startMs, endMs]. Community's time-series backends
// ignore paging and return the whole range, so a list longer than pageSize is
// sliced here and total is the full length.
func (c *Client) ListTelemetryHistory(ctx context.Context, deviceID, key string, startMs, endMs int64, page, pageSize int) ([]HistoryReading, int64, error) {
	token, err := c.Login(ctx)
	if err != nil {
		return nil, 0, err
	}
	q := url.Values{}
	q.Set("device_id", deviceID)
	q.Set("key", key)
	q.Set("start_time", strconv.FormatInt(startMs, 10))
	q.Set("end_time", strconv.FormatInt(endMs, 10))
	q.Set("page", itoa(page))
	q.Set("page_size", itoa(pageSize))
	raw, err := c.doRaw(ctx, "GET", "/api/v1/telemetry/datas/history/page?"+q.Encode(), token, nil)
	if err != nil {
		return nil, 0, err
	}
	data, err := decodeEnvelope(raw)
	if err != nil {
		return nil, 0, err
	}
	var body struct {
		Total int64 `json:"total"`
		List  []struct {
			Ts    int64           `json:"ts"`
			Key   string          `json:"key"`
			Value json.RawMessage `json:"value"`
		} `json:"list"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, 0, fmt.Errorf("legacy telemetry history has an unexpected shape: %w", err)
	}
	rows := body.List
	total := body.Total
	if len(rows) > pageSize {
		total = int64(len(rows))
		start := (page - 1) * pageSize
		if start >= len(rows) {
			rows = nil
		} else {
			end := start + pageSize
			if end > len(rows) {
				end = len(rows)
			}
			rows = rows[start:end]
		}
	}
	out := make([]HistoryReading, 0, len(rows))
	for _, row := range rows {
		if row.Ts <= 0 || row.Key == "" {
			return nil, 0, errors.New("legacy telemetry history row is missing key or timestamp")
		}
		value := row.Value
		if len(value) == 0 {
			value = json.RawMessage("null")
		}
		out = append(out, HistoryReading{Key: row.Key, Ts: time.UnixMilli(row.Ts).UTC(), Value: value})
	}
	return out, total, nil
}
