package diagnostics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type LogicalState string
type LogicalUpdate struct {
	Message *LogicalMessage
	State   LogicalState
	Reason  string
}
type LogicalMessage struct{ TenantID, DeviceID, ID, Family string }

func (m *LogicalMessage) Record(state LogicalState, reason string) {
	if m != nil {
		GetInstance().enqueueLogical(LogicalUpdate{Message: m, State: state, Reason: reason})
	}
}

const (
	LogicalPending       LogicalState = "pending"
	LogicalCommitted     LogicalState = "committed"
	LogicalRejected      LogicalState = "rejected"
	LogicalStorageFailed LogicalState = "storage_failed"
)

// Each device deduplicates only its last 4096 observation identities. Keys
// expire after seven idle days; continuously active counters are cumulative,
// not a rolling seven-day count. Replays after identity eviction may recount.
// Business receipt idempotency remains in SQL. This bounded Redis observation
// window is best effort, not a cross-store exactly-once commit guarantee.
const logicalObservationScript = `
local previous=redis.call('HGET',KEYS[2],ARGV[1])
if previous and previous~='pending' then return 0 end
if not previous then redis.call('HINCRBY',KEYS[1],'uplink_total',1) end
local state=ARGV[2]
if state=='committed' then
 redis.call('HINCRBY',KEYS[1],'uplink_success',1)
 redis.call('HINCRBY',KEYS[1],'storage_total',1)
 redis.call('HINCRBY',KEYS[1],'storage_success',1)
elseif state=='rejected' or state=='storage_failed' then
 redis.call('HINCRBY',KEYS[1],'uplink_failed',1)
 if state=='storage_failed' then redis.call('HINCRBY',KEYS[1],'storage_total',1);redis.call('HINCRBY',KEYS[1],'storage_failed',1) end
 redis.call('LPUSH',KEYS[4],ARGV[4]);redis.call('LTRIM',KEYS[4],0,4)
end
redis.call('HSET',KEYS[2],ARGV[1],state)
redis.call('ZADD',KEYS[3],ARGV[3],ARGV[1])
local excess=redis.call('ZCARD',KEYS[3])-4096
if excess>0 then
 local old=redis.call('ZRANGE',KEYS[3],0,excess-1)
 for _,id in ipairs(old) do redis.call('HDEL',KEYS[2],id);redis.call('ZREM',KEYS[3],id) end
end
for _,key in ipairs(KEYS) do redis.call('EXPIRE',key,604800) end
return 1
`

func (m *Metrics) logicalKeys(device string) []string {
	prefix := fmt.Sprintf("device:{%s}:diagnostics:v2:", device)
	return []string{prefix + "stats", prefix + "observations", prefix + "order", prefix + "failures"}
}

func (m *Metrics) ObserveLogical(ctx context.Context, tenant, device, id, family string, state LogicalState, reason string) error {
	return m.ObserveLogicalBatch(ctx, []LogicalUpdate{{Message: &LogicalMessage{tenant, device, id, family}, State: state, Reason: reason}})
}
func (m *Metrics) ObserveLogicalBatch(ctx context.Context, updates []LogicalUpdate) error {
	if m == nil || m.logicalClient == nil || len(updates) > 2000 {
		return fmt.Errorf("logical diagnostics unavailable or batch too large")
	}
	pipe := m.logicalClient.Pipeline()
	for _, u := range updates {
		o := u.Message
		if o == nil {
			continue
		}
		if o.TenantID == "" || o.DeviceID == "" || o.ID == "" {
			return fmt.Errorf("logical diagnostic scope unavailable")
		}
		if u.State != LogicalPending && u.State != LogicalCommitted && u.State != LogicalRejected && u.State != LogicalStorageFailed {
			return fmt.Errorf("invalid logical diagnostic outcome")
		}
		stage := StageProcessor
		if u.State == LogicalStorageFailed {
			stage = StageStorage
		}
		record, _ := json.Marshal(FailureRecord{Timestamp: time.Now().UTC(), Direction: DirectionUplink, Stage: stage, Error: u.Reason})
		h := sha256.Sum256([]byte(o.TenantID + "\x00" + o.DeviceID + "\x00" + o.Family + "\x00" + o.ID))
		pipe.Eval(ctx, logicalObservationScript, m.logicalKeys(o.DeviceID), hex.EncodeToString(h[:]), string(u.State), time.Now().UnixMilli(), string(record))
	}
	_, err := pipe.Exec(ctx)
	return err
}

// RecordLogical never changes business results on diagnostic failure. The API
// propagates Redis read failures instead of presenting fabricated success.
func (c *Collector) RecordLogical(tenant, device, id, family string, state LogicalState, reason string) {
	c.RecordLogicalBatch(context.Background(), []LogicalUpdate{{Message: &LogicalMessage{tenant, device, id, family}, State: state, Reason: reason}})
}
func (c *Collector) RecordLogicalBatch(parent context.Context, updates []LogicalUpdate) {
	if !c.IsEnabled() || len(updates) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 500*time.Millisecond)
	defer cancel()
	if err := c.metrics.ObserveLogicalBatch(ctx, updates); err != nil {
		c.logger.WithError(err).Warn("logical uplink diagnostics unavailable")
	}
}

// Storage commits enqueue observations without waiting on Redis for every child.
// A bounded buffer is best effort: overflow is observable but never blocks or
// changes a committed business result. Each periodic flush has one deadline.
func (c *Collector) enqueueLogical(u LogicalUpdate) {
	if !c.IsEnabled() || u.Message == nil {
		return
	}
	c.logicalMu.Lock()
	if len(c.logicalBuffer) >= 2000 {
		c.logicalDropped++
		c.logicalMu.Unlock()
		return
	}
	copy := *u.Message
	u.Message = &copy
	c.logicalBuffer = append(c.logicalBuffer, u)
	c.logicalMu.Unlock()
}
func (c *Collector) flushLogical() {
	c.logicalMu.Lock()
	batch := c.logicalBuffer
	dropped := c.logicalDropped
	c.logicalDropped = 0
	c.logicalBuffer = nil
	c.logicalMu.Unlock()
	if dropped > 0 {
		c.logger.Warnf("logical diagnostic buffer full; %d observations dropped", dropped)
	}
	c.RecordLogicalBatch(context.Background(), batch)
}
