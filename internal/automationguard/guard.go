// Package automationguard makes the legacy publish endpoints authoritative about
// which command is the current one.
//
// # Why this exists
//
// The automation service can check "is my run still current?" before it sends.
// That check is not authoritative: a newer trigger can arrive between the check
// and the send, and an older command can arrive after a newer one. The only place
// where the order of two competing commands can actually be decided is the point
// where they are accepted — here, in front of the legacy publish handlers.
//
// What it does
//
//   - Generation authority. A command carries the run scope and its generation.
//     The guard records the highest generation seen per scope; anything lower is
//     refused with `superseded` and is never sent to the device.
//   - Idempotent acceptance. A command carries a stable idempotency key. The same
//     key with the same content replays the exact response that was returned the
//     first time, so a transport retry can never become a second action. The same
//     key with different content is a conflict, not a new action.
//   - No blind resend. If a previous attempt is still in flight, or died while in
//     flight, the answer is "unknown", not "send it again". A non-idempotent
//     device action must not be repeated just because nobody wrote down what
//     happened.
//
// # Backward compatibility
//
// A request without X-Idempotency-Key passes through untouched. Every existing
// caller (the web console, the App, scripts) keeps its current behaviour; only the
// automation service opts in.
package automationguard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	HeaderIdempotencyKey = "X-Idempotency-Key"
	HeaderScope          = "X-Automation-Scope"
	HeaderGeneration     = "X-Automation-Generation"
	// HeaderSuperseded tells the caller why it was refused, without making it
	// parse a Chinese error message.
	HeaderSuperseded = "X-Automation-Superseded"

	// inFlightLease bounds how long one attempt may hold a key. It is shorter
	// than the automation side's transport retry (30s) on purpose: a stuck first
	// attempt must not block the retry for long.
	inFlightLease = 45 * time.Second

	codeSuperseded  = 4090
	codeKeyConflict = 4091
	codeInFlight    = 4092
	codeUnknown     = 4093
)

// Guard returns the middleware. db must be the shared database handle.
func Guard(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader(HeaderIdempotencyKey))
		if key == "" {
			// Not an automation-issued command: keep the legacy path byte for byte.
			c.Next()
			return
		}
		if db == nil {
			// Without a database the guard cannot decide whether this is a
			// repeat. Fail closed for automation callers only: the legacy path
			// above is untouched, and an automation command is refused rather
			// than risked as a duplicate.
			abort(c, http.StatusServiceUnavailable, codeUnknown, "命令受理守卫不可用，拒绝下发")
			return
		}

		raw, err := io.ReadAll(c.Request.Body)
		if err != nil {
			abort(c, http.StatusBadRequest, codeUnknown, "无法读取请求体")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(raw))

		scope := strings.TrimSpace(c.GetHeader(HeaderScope))
		generation, _ := strconv.ParseInt(strings.TrimSpace(c.GetHeader(HeaderGeneration)), 10, 64)
		deviceID := extractDeviceID(raw)
		kind := actionKind(c.FullPath())
		hash := payloadHash(kind, deviceID, raw)

		// 1. Generation authority. This runs first: if the command is already
		//    superseded, nothing else about it matters.
		if scope != "" {
			current, err := bumpScopeGeneration(db, scope, generation)
			if err != nil {
				abort(c, http.StatusInternalServerError, codeUnknown, "代际登记失败: "+err.Error())
				return
			}
			if generation < current {
				body := fmt.Sprintf(`{"code":%d,"message":"该动作已被更新的触发取代（代际 %d < %d），未下发"}`,
					codeSuperseded, generation, current)
				record(c, db, key, scope, generation, kind, deviceID, hash, "superseded",
					http.StatusConflict, body, "")
				writeAborted(c, http.StatusConflict, body, true)
				return
			}
		}

		// 2. Idempotent acceptance.
		inserted, err := reserveKey(db, key, scope, generation, kind, deviceID, hash)
		if err != nil {
			abort(c, http.StatusInternalServerError, codeUnknown, "幂等键登记失败: "+err.Error())
			return
		}
		if !inserted {
			replay(c, db, key, hash)
			return
		}

		// 3. Execute once, capture the exact response.
		rec := &recorder{ResponseWriter: c.Writer, status: http.StatusOK}
		c.Writer = rec
		c.Next()
		c.Writer = rec.ResponseWriter

		body := rec.body.String()
		state := classify(rec.status, body)
		record(c, db, key, scope, generation, kind, deviceID, hash, state,
			rec.status, body, extractRef(body))

		c.Writer.WriteHeader(rec.status)
		_, _ = c.Writer.Write(rec.body.Bytes())
	}
}

// bumpScopeGeneration records the generation and returns the highest seen.
//
// GREATEST keeps the maximum under concurrency: two commands racing with
// generations 7 and 8 end with 8 recorded no matter which arrives first, so the
// late arrival of 7 is refused either way.
func bumpScopeGeneration(db *gorm.DB, scope string, generation int64) (int64, error) {
	if err := db.Exec(`
        INSERT INTO automation_scope_generation (scope_key, generation, updated_at)
        VALUES (?, ?, now())
        ON CONFLICT (scope_key) DO UPDATE
           SET generation = GREATEST(automation_scope_generation.generation, EXCLUDED.generation),
               updated_at = now()`, scope, generation).Error; err != nil {
		return 0, err
	}
	var current int64
	if err := db.Raw(`SELECT generation FROM automation_scope_generation WHERE scope_key = ?`, scope).
		Scan(&current).Error; err != nil {
		return 0, err
	}
	return current, nil
}

// reserveKey claims the idempotency key. false means somebody already holds it.
func reserveKey(db *gorm.DB, key, scope string, generation int64, kind, deviceID, hash string) (bool, error) {
	res := db.Exec(`
        INSERT INTO automation_command_guard
            (idempotency_key, scope_key, generation, action_kind, device_id, payload_hash,
             state, lease_until, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, 'in_flight', now() + make_interval(secs => ?), now(), now())
        ON CONFLICT (idempotency_key) DO NOTHING`,
		key, scope, generation, kind, deviceID, hash, inFlightLease.Seconds())
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

type guardRow struct {
	State        string
	PayloadHash  string
	HTTPStatus   int
	Response     string `gorm:"column:response_body"`
	AcceptedRef  string
	LeaseExpired bool
}

// replay answers a repeated key from the recorded outcome.
func replay(c *gin.Context, db *gorm.DB, key, hash string) {
	var row guardRow
	if err := db.Raw(`
        SELECT state, payload_hash, http_status, response_body, accepted_ref,
               (lease_until IS NULL OR lease_until < now()) AS lease_expired
          FROM automation_command_guard WHERE idempotency_key = ?`, key).Scan(&row).Error; err != nil {
		abort(c, http.StatusInternalServerError, codeUnknown, "幂等记录读取失败: "+err.Error())
		return
	}
	if row.PayloadHash != hash {
		// Same key, different content. Accepting this would silently replace an
		// action that may already have reached the device.
		abort(c, http.StatusConflict, codeKeyConflict,
			"同一幂等键被用于不同内容；这是冲突，不是重试")
		return
	}
	switch row.State {
	case "accepted", "failed", "superseded":
		writeAborted(c, row.HTTPStatus, row.Response, row.State == "superseded")
		return
	case "in_flight":
		if !row.LeaseExpired {
			abort(c, http.StatusConflict, codeInFlight, "同一动作正在处理中，请稍后重试")
			return
		}
		// The first attempt died while holding the key. The device may or may not
		// have acted, so the honest answer is unknown — not a second send.
		abort(c, http.StatusConflict, codeUnknown,
			"上一次尝试已中断，结果未知；不换新 ID 盲重发")
		return
	default:
		abort(c, http.StatusConflict, codeUnknown, "该动作结果未知")
		return
	}
}

func record(c *gin.Context, db *gorm.DB, key, scope string, generation int64, kind, deviceID, hash,
	state string, status int, body, ref string) {
	if err := db.Exec(`
        UPDATE automation_command_guard
           SET state = ?, http_status = ?, response_body = ?, accepted_ref = ?,
               lease_until = NULL, updated_at = now()
         WHERE idempotency_key = ?`, state, status, body, ref, key).Error; err != nil {
		// The response has already been decided; failing to record it must not
		// change what the caller sees, but it does mean the next retry will see
		// "in flight". Log through gin so it is visible in the request log.
		_ = c.Error(fmt.Errorf("record command guard outcome for %s: %w", key, err))
	}
}

func abort(c *gin.Context, status, code int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"code": code, "message": message})
}

// writeAborted writes a decided answer and stops the chain.
//
// Aborting is not optional here. Returning from a gin middleware without
// c.Abort() only ends that middleware: the engine keeps walking the remaining
// handlers, so a command that was refused would still be published to the
// device. That mistake is invisible in the response body, which is why it is
// centralised in one function.
func writeAborted(c *gin.Context, status int, body string, superseded bool) {
	if superseded {
		c.Header(HeaderSuperseded, "true")
	}
	c.Abort()
	c.Status(status)
	_, _ = c.Writer.WriteString(body)
	c.Writer.WriteHeaderNow()
}

// classify maps the owner's response onto the guard's state vocabulary.
func classify(status int, body string) string {
	var envelope struct {
		Code *int `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &envelope)
	switch {
	case status >= 500:
		return "unknown"
	case status >= 400:
		return "failed"
	case envelope.Code != nil && *envelope.Code != 200:
		// The legacy backend reports business errors as HTTP 200 with a code.
		return "failed"
	default:
		return "accepted"
	}
}

// extractRef pulls the owner's own id out of the response so a late device
// result can be correlated with this logical action.
func extractRef(body string) string {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil || len(envelope.Data) == 0 {
		return ""
	}
	var data map[string]any
	if json.Unmarshal(envelope.Data, &data) != nil {
		return ""
	}
	for _, k := range []string{"message_id", "command_id", "id", "log_id"} {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func extractDeviceID(raw []byte) string {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = json.Unmarshal(raw, &body)
	return body.DeviceID
}

// actionKind derives the legacy TEL/ATTR/CMD class from the route, so the stored
// record says what kind of action it was without trusting the body.
func actionKind(fullPath string) string {
	switch {
	case strings.Contains(fullPath, "/telemetry/"):
		return "telemetry"
	case strings.Contains(fullPath, "/attribute/"):
		return "attribute"
	case strings.Contains(fullPath, "/command/"):
		return "command"
	default:
		return "unknown"
	}
}

func payloadHash(kind, deviceID string, raw []byte) string {
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(deviceID))
	h.Write([]byte{0})
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// recorder captures the handler's response so it can be stored and replayed.
//
// It deliberately does not forward: the body must be written exactly once, after
// the outcome has been recorded, otherwise the client would receive it twice.
type recorder struct {
	gin.ResponseWriter
	status  int
	written bool
	body    bytes.Buffer
}

func (r *recorder) WriteHeader(code int) {
	if r.written {
		return
	}
	r.status = code
	r.written = true
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.body.Write(b)
}

func (r *recorder) WriteString(s string) (int, error) {
	if !r.written {
		r.written = true
	}
	return r.body.WriteString(s)
}
