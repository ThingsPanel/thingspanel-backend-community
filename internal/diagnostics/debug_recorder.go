package diagnostics

import (
	"context"
	"encoding/json"
	"time"

	"project/internal/model"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

const (
	DebugConfigPrefix          = "tp:devdebug:cfg:"
	DebugLogsPrefix            = "tp:devdebug:logs:"
	DebugRetentionGraceSeconds = 600
)

// Record into the existing device debug list only while the Redis session is active.
// Combine the check and write so a concurrent stop cannot race a write.
const appendDebugLog = `
local raw = redis.call('GET', KEYS[1])
if not raw then return 0 end
local ok, cfg = pcall(cjson.decode, raw)
if not ok or type(cfg) ~= 'table' or cfg.enabled ~= true then return 0 end
local expires = tonumber(cfg.expire_at)
local now = tonumber(redis.call('TIME')[1])
if not expires or expires <= now or tonumber(ARGV[2]) >= expires then return 0 end
local maxItems = math.max(1, math.min(5000, math.floor(tonumber(cfg.max_items) or 1000)))
redis.call('LPUSH', KEYS[2], ARGV[1])
redis.call('LTRIM', KEYS[2], 0, maxItems - 1)
redis.call('EXPIREAT', KEYS[2], expires + tonumber(ARGV[3]))
return 1
`

// DebugRecorder owns a bounded buffer. Calls from message processing never wait
// for Redis. Every write rechecks the session and excludes raw device payloads.
type debugRecord struct {
	entry model.DeviceDebugLogEntry
	at    int64
}

type DebugRecorder struct {
	queue   chan debugRecord
	dropped chan struct{}
}

func NewDebugRecorder(ctx context.Context, client *redis.Client, logger *logrus.Logger) *DebugRecorder {
	if client == nil {
		return nil
	}
	recorder := &DebugRecorder{queue: make(chan debugRecord, 256), dropped: make(chan struct{}, 1)}
	go recorder.run(ctx, client, logger)
	return recorder
}

func (r *DebugRecorder) Record(deviceID, direction, action, outcome string) {
	if r == nil || deviceID == "" {
		return
	}
	now := time.Now().UTC()
	entry := model.DeviceDebugLogEntry{
		Ts: now.Format(time.RFC3339Nano), DeviceID: deviceID, Protocol: "mqtt",
		Direction: direction, Action: action, Outcome: outcome,
	}
	select {
	case r.queue <- debugRecord{entry: entry, at: now.Unix()}:
	default:
		select {
		case r.dropped <- struct{}{}:
		default:
		}
	}
}

func (r *DebugRecorder) run(ctx context.Context, client *redis.Client, logger *logrus.Logger) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var reportAfter time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		count := min(len(r.queue), 128)
		if count == 0 {
			continue
		}
		pipeline := client.Pipeline()
		writeCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		for i := 0; i < count; i++ {
			record := <-r.queue
			raw, err := json.Marshal(record.entry)
			if err != nil {
				continue
			}
			pipeline.Eval(writeCtx, appendDebugLog, []string{DebugConfigPrefix + record.entry.DeviceID, DebugLogsPrefix + record.entry.DeviceID}, raw, record.at, DebugRetentionGraceSeconds)
		}
		_, err := pipeline.Exec(writeCtx)
		cancel()
		lost := false
		select {
		case <-r.dropped:
			lost = true
		default:
		}
		if (err != nil || lost) && logger != nil && time.Now().After(reportAfter) && ctx.Err() == nil {
			logger.WithFields(logrus.Fields{"redis_error": err != nil, "buffer_full": lost}).Warn("device debug logs were dropped")
			reportAfter = time.Now().Add(time.Minute)
		}
	}
}
