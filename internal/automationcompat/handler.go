package automationcompat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

// Business codes returned by the adapter.
//
// They live in the old project's numeric space because that is what the clients
// switch on, but they are only ever produced here, so they cannot collide with a
// code the old backend still returns.
const (
	// codeReadOnly means "this object has moved; edit it in the console".
	codeReadOnly = 100460
	// codeAmbiguous means the old id maps to more than one new rule.
	codeAmbiguous = 100461
	// codeSunset means the adapter's service window is over.
	codeSunset = 100462
	// codeUpstream means the new service refused or could not be reached.
	codeUpstream = 100463
	// codeNotExecuted means the run was created but nothing happened. It is a
	// separate code from codeUpstream because the caller's action is different:
	// "try again later" versus "fix the rule or the device".
	codeNotExecuted = 100464
)

// Deps is what the handlers need.
type Deps struct {
	Core     *CoreClient
	Recorder *Recorder
	Sunset   time.Time
	// Now is injectable so the sunset tests do not have to sleep.
	Now func() time.Time
}

// Handlers is the compat surface. One struct so the routing table and the tests
// agree about which endpoints exist.
type Handlers struct {
	deps Deps
}

// NewHandlers builds the handler set.
func NewHandlers(d Deps) *Handlers {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Recorder == nil {
		d.Recorder = NewRecorder(d.Now)
	}
	return &Handlers{deps: d}
}

// Recorder exposes the counters (for /metrics and for the smoke script).
func (h *Handlers) Recorder() *Recorder { return h.deps.Recorder }

// ---------------------------------------------------------------------------
// Envelope
// ---------------------------------------------------------------------------

// envelope is the old response body.
//
// It is written by the handler rather than left to the old response middleware
// for one reason: the adapter must produce byte-identical bodies whether it is
// mounted inside the old router (takeover) or in front of it (shadow, and the
// standalone binary the smoke script drives). The middleware skips a response
// that is already written, so both paths agree.
type envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ok writes the old success body: HTTP 200 with code 200.
func (h *Handlers) ok(c *gin.Context, data any) {
	h.stamp(c)
	c.JSON(http.StatusOK, envelope{Code: 200, Message: "success", Data: data})
}

// fail writes the old failure body.
//
// The old contract is HTTP 200 for every business failure, and the clients rely
// on it: the App only reads `res.code`, and the console's axios instance treats
// a non-2xx as a transport error and retries. Returning 404 here would turn a
// readable "not migrated yet" into a retry storm.
func (h *Handlers) fail(c *gin.Context, code int, message string, reason string) {
	h.stamp(c)
	data := map[string]any{"reason_code": reason}
	c.JSON(http.StatusOK, envelope{Code: code, Message: message, Data: data})
}

// stamp adds the deprecation headers so an operator can see in a browser network
// panel that this endpoint has an end date.
func (h *Handlers) stamp(c *gin.Context) {
	c.Header("Deprecation", "true")
	if !h.deps.Sunset.IsZero() {
		c.Header("Sunset", h.deps.Sunset.UTC().Format(http.TimeFormat))
		c.Header("Warning", `299 - "automation compatibility adapter, ends `+
			h.deps.Sunset.UTC().Format("2006-01-02")+`"`)
	}
}

// expired reports whether the service window is over.
func (h *Handlers) expired() bool {
	return !h.deps.Sunset.IsZero() && !h.deps.Now().Before(h.deps.Sunset)
}

// ---------------------------------------------------------------------------
// Identity
// ---------------------------------------------------------------------------

// caller is who is asking.
type caller struct {
	TenantID string
	UserID   string
	// Token is forwarded to the new service untouched.
	Token string
}

// callerOf reads the identity without validating it.
//
// Validation is the new service's job. The adapter forwards the caller's own
// token, so the tenant that ends up owning the read is the tenant the token
// names — not a value this package computed. That is why there is no "trusted
// tenant" header here to get wrong.
func callerOf(c *gin.Context) caller {
	out := caller{Token: bearerOf(c)}
	if raw, exists := c.Get("claims"); exists {
		if claims, ok := raw.(*utils.UserClaims); ok && claims != nil {
			out.TenantID = claims.TenantID
			out.UserID = claims.ID
		}
	}
	return out
}

func bearerOf(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("x-token")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.GetHeader("Authorization")); v != "" {
		if len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
			return strings.TrimSpace(v[7:])
		}
		return v
	}
	return ""
}

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

// entry runs the common prologue: sunset and consumer attribution.
//
// It returns the request context rather than a derived one: the upstream client
// already carries its own timeout, and wrapping the context here would need a
// cancel that has to outlive this function, which is how contexts get leaked.
func (h *Handlers) entry(c *gin.Context, endpoint string) (context.Context, caller, Consumer, bool) {
	consumer := Classify(c.Request)
	if h.expired() {
		h.deps.Recorder.Observe(consumer, endpoint, "refused")
		h.fail(c, codeSunset, "自动化兼容适配已到期，请升级客户端", "compat_sunset")
		return nil, caller{}, consumer, false
	}
	return c.Request.Context(), callerOf(c), consumer, true
}

// upstreamFailure turns a CoreError into the old envelope and records it.
func (h *Handlers) upstreamFailure(c *gin.Context, consumer Consumer, endpoint string, err error) {
	var ce *CoreError
	if errors.As(err, &ce) {
		switch {
		case ce.Code == "legacy_ref_missing" || ce.IsNotFound():
			h.deps.Recorder.Observe(consumer, endpoint, "not_found")
			h.fail(c, 100404, "这个对象还没有迁移到新的自动化引擎", "legacy_ref_missing")
			return
		case ce.Code == "ambiguous_legacy_ref":
			h.deps.Recorder.Observe(consumer, endpoint, "refused")
			h.fail(c, codeAmbiguous, "旧 id 对应了多条新规则，需要人工确认", ce.Code)
			return
		case ce.IsConflict():
			h.deps.Recorder.Observe(consumer, endpoint, "refused")
			h.fail(c, 100409, "对象已被修改，请刷新后重试", ce.Code)
			return
		case ce.IsUnavailable():
			h.deps.Recorder.Observe(consumer, endpoint, "error")
			h.fail(c, codeUpstream, "自动化引擎暂时不可用", "upstream_unreachable")
			return
		}
	}
	h.deps.Recorder.Observe(consumer, endpoint, "error")
	msg := "自动化引擎拒绝了这次请求"
	if err != nil {
		msg = msg + "：" + err.Error()
	}
	h.fail(c, codeUpstream, msg, "upstream_refused")
}

// resolve maps an old id to a new rule id.
func (h *Handlers) resolve(ctx context.Context, c *gin.Context, token, legacyID string) (*legacyRef, bool) {
	ref, err := h.deps.Core.ResolveLegacy(ctx, token, legacyID)
	if err != nil {
		h.upstreamFailure(c, Classify(c.Request), "resolve", err)
		return nil, false
	}
	if ref.Ambiguous {
		h.deps.Recorder.Observe(Classify(c.Request), "resolve", "refused")
		h.fail(c, codeAmbiguous,
			"这个旧 id 对应了 "+strconv.Itoa(len(ref.Candidates))+" 条新规则，需要人工确认",
			"ambiguous_legacy_ref")
		return nil, false
	}
	return ref, true
}

// paging reads the old page/page_size pair.
func paging(c *gin.Context) (page, pageSize, limit, offset int) {
	page = atoiDefault(c.Query("page"), 1)
	pageSize = atoiDefault(c.Query("page_size"), 10)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	// A page size of 5000 is not a page, it is a table scan dressed up as a
	// request; the old UI never asked for more than 100.
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize, pageSize, (page - 1) * pageSize
}

func atoiDefault(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// newRequestID mints an idempotency key for a manual activation.
//
// The old clients do not send one, so a retry after a timeout runs the scene
// twice — the same thing that happens today against the old engine. The adapter
// does not try to invent a window ("same scene within 3s is probably the same
// press") because that would silently drop a legitimate second press. Clients
// that do send X-Request-Id get the new service's replay protection for free.
func newRequestID(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-Request-Id")); v != "" {
		return "legacy:" + v
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "legacy:" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "legacy:" + hex.EncodeToString(b[:])
}

// ---------------------------------------------------------------------------
// Scenes
// ---------------------------------------------------------------------------

// SceneList answers `GET /api/v1/scene`.
func (h *Handlers) SceneList(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "scene.list")
	if !ok {
		return
	}
	page, pageSize, limit, offset := paging(c)
	res, err := h.deps.Core.ListRules(ctx, cl.Token, "scene", limit, offset)
	if err != nil {
		h.upstreamFailure(c, consumer, "scene.list", err)
		return
	}
	rows := make([]map[string]any, 0, len(res.Rules))
	for i := range res.Rules {
		r := &res.Rules[i]
		rows = append(rows, SceneRow(legacyIDOf(r), r))
	}
	h.deps.Recorder.Observe(consumer, "scene.list", "ok")
	h.ok(c, Page(rows, res.Total, page, pageSize))
}

// SceneDetail answers `GET /api/v1/scene/detail/:id`.
func (h *Handlers) SceneDetail(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "scene.detail")
	if !ok {
		return
	}
	legacyID := c.Param("id")
	ref, ok := h.resolve(ctx, c, cl.Token, legacyID)
	if !ok {
		return
	}
	r, err := h.deps.Core.GetRule(ctx, cl.Token, ref.RuleID)
	if err != nil {
		h.upstreamFailure(c, consumer, "scene.detail", err)
		return
	}
	row, warnings := SceneDetail(legacyID, r)
	if len(warnings) > 0 {
		row["compat_warnings"] = warnings
	}
	h.deps.Recorder.Observe(consumer, "scene.detail", "ok")
	h.ok(c, row)
}

// SceneActive answers `POST /api/v1/scene/active/:id` — the App's and the
// console's "activate" button.
func (h *Handlers) SceneActive(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "scene.active")
	if !ok {
		return
	}
	legacyID := c.Param("id")
	ref, ok := h.resolve(ctx, c, cl.Token, legacyID)
	if !ok {
		return
	}
	// The old body is empty; some clients send {"device_id": "..."} anyway.
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	run, err := h.deps.Core.RunRule(ctx, cl.Token, ref.RuleID, body.DeviceID, newRequestID(c))
	if err != nil {
		h.upstreamFailure(c, consumer, "scene.active", err)
		return
	}
	// A run that was admitted is not the same as a run that happened.
	//
	// The old client shows "成功" for any code 200, so answering 200 for a run
	// the runtime rejected (device not in the directory, rule disabled, scope
	// busy) would put the fake-success defect back on the customer's phone: they
	// press "activate", the phone says it worked, and nothing moved. The statuses
	// that mean "it started" are the only ones that may answer 200.
	switch run.Status {
	case "succeeded", "accepted", "running", "queued":
		// started (or finished) — the old client's success toast is fair.
	default:
		h.deps.Recorder.Observe(consumer, "scene.active", "refused")
		reason := run.Reason
		if reason == "" {
			reason = run.ReasonCode
		}
		if reason == "" {
			reason = "运行状态是 " + run.Status
		}
		h.fail(c, codeNotExecuted, "这条场景没有执行："+reason, "run_"+run.Status)
		return
	}
	h.deps.Recorder.Observe(consumer, "scene.active", "ok")
	// The old endpoint answered `data: null` and the App only checks code==200,
	// so the run id is extra information the new clients can use and the old
	// ones ignore.
	h.ok(c, map[string]any{"run_id": run.ID, "status": run.Status})
}

// ---------------------------------------------------------------------------
// Scene automations
// ---------------------------------------------------------------------------

// AutomationList answers `GET /api/v1/scene_automations/list`.
func (h *Handlers) AutomationList(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "automation.list")
	if !ok {
		return
	}
	page, pageSize, limit, offset := paging(c)
	res, err := h.deps.Core.ListRules(ctx, cl.Token, "automation", limit, offset)
	if err != nil {
		h.upstreamFailure(c, consumer, "automation.list", err)
		return
	}
	rows := make([]map[string]any, 0, len(res.Rules))
	for i := range res.Rules {
		r := &res.Rules[i]
		rows = append(rows, AutomationRow(legacyIDOf(r), r))
	}
	h.deps.Recorder.Observe(consumer, "automation.list", "ok")
	h.ok(c, Page(rows, res.Total, page, pageSize))
}

// AutomationDetail answers `GET /api/v1/scene_automations/detail/:id`.
func (h *Handlers) AutomationDetail(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "automation.detail")
	if !ok {
		return
	}
	legacyID := c.Param("id")
	ref, ok := h.resolve(ctx, c, cl.Token, legacyID)
	if !ok {
		return
	}
	r, err := h.deps.Core.GetRule(ctx, cl.Token, ref.RuleID)
	if err != nil {
		h.upstreamFailure(c, consumer, "automation.detail", err)
		return
	}
	h.deps.Recorder.Observe(consumer, "automation.detail", "ok")
	h.ok(c, AutomationRow(legacyID, r))
}

// AutomationSwitch answers `POST /api/v1/scene_automations/switch/:id`.
//
// The old endpoint toggled and returned nothing, so the client flipped its own
// copy of the flag. Toggling from a read is a lost update waiting to happen
// (two phones, one stale list), so this reads the rule, computes the new value,
// and sends it with the version it read: a concurrent edit becomes a 409 the
// client can see instead of a silent revert.
func (h *Handlers) AutomationSwitch(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "automation.switch")
	if !ok {
		return
	}
	legacyID := c.Param("id")
	ref, ok := h.resolve(ctx, c, cl.Token, legacyID)
	if !ok {
		return
	}
	// An explicit target wins: the console sends {"enabled": true} in newer
	// builds, the App sends nothing and means "toggle".
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	_ = c.ShouldBindJSON(&body)

	current, err := h.deps.Core.GetRule(ctx, cl.Token, ref.RuleID)
	if err != nil {
		h.upstreamFailure(c, consumer, "automation.switch", err)
		return
	}
	next := !current.Enabled
	if body.Enabled != nil {
		next = *body.Enabled
	}
	updated, err := h.deps.Core.SetEnabled(ctx, cl.Token, ref.RuleID, next, current.Version)
	if err != nil {
		h.upstreamFailure(c, consumer, "automation.switch", err)
		return
	}
	h.deps.Recorder.Observe(consumer, "automation.switch", "ok")
	// The old body was `data: null`; returning the new value lets a client that
	// wants to re-render do so without a second request.
	h.ok(c, map[string]any{"enabled": EnabledFlag(updated.Enabled)})
}

// AutomationLog answers `GET /api/v1/scene_automations/log`.
func (h *Handlers) AutomationLog(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "automation.log")
	if !ok {
		return
	}
	legacyID := strings.TrimSpace(c.Query("scene_automation_id"))
	if legacyID == "" {
		legacyID = strings.TrimSpace(c.Query("id"))
	}
	if legacyID == "" {
		h.deps.Recorder.Observe(consumer, "automation.log", "refused")
		h.fail(c, 100002, "缺少 scene_automation_id", "missing_parameter")
		return
	}
	ref, ok := h.resolve(ctx, c, cl.Token, legacyID)
	if !ok {
		return
	}
	page, pageSize, limit, _ := paging(c)
	res, err := h.deps.Core.ListRuns(ctx, cl.Token, ref.RuleID, limit)
	if err != nil {
		h.upstreamFailure(c, consumer, "automation.log", err)
		return
	}
	rows := make([]map[string]any, 0, len(res.Runs))
	for i := range res.Runs {
		rows = append(rows, RunLogRow(&res.Runs[i]))
	}
	h.deps.Recorder.Observe(consumer, "automation.log", "ok")
	h.ok(c, Page(rows, res.Total, page, pageSize))
}

// ---------------------------------------------------------------------------
// Alarms
// ---------------------------------------------------------------------------

// AlarmInfo answers `GET /api/v1/alarm/info` — the App's alarm card.
func (h *Handlers) AlarmInfo(c *gin.Context) {
	h.alarmList(c, "alarm.info", "active")
}

// AlarmHistory answers `GET /api/v1/alarm/info/history`.
func (h *Handlers) AlarmHistory(c *gin.Context) {
	h.alarmList(c, "alarm.history", "cleared")
}

func (h *Handlers) alarmList(c *gin.Context, endpoint, state string) {
	ctx, cl, consumer, ok := h.entry(c, endpoint)
	if !ok {
		return
	}
	page, pageSize, limit, _ := paging(c)

	// The old filter names, mapped: device_id was a jsonb key on the device
	// list, alarm_status was L/M/H for active and N for recovered.
	subject := strings.TrimSpace(c.Query("device_id"))
	if raw := strings.TrimSpace(c.Query("alarm_status")); raw != "" {
		if strings.EqualFold(raw, "N") {
			state = "cleared"
		} else {
			state = "active"
		}
	}
	res, err := h.deps.Core.ListAlarms(ctx, cl.Token, subject, state, limit)
	if err != nil {
		h.upstreamFailure(c, consumer, endpoint, err)
		return
	}
	names := newPolicyNames(h.deps.Core, cl.Token)
	rows := make([]map[string]any, 0, len(res.Alarms))
	for i := range res.Alarms {
		a := &res.Alarms[i]
		rows = append(rows, AlarmRow(a, names.of(ctx, a.PolicyID)))
	}
	h.deps.Recorder.Observe(consumer, endpoint, "ok")
	// The new service has no count endpoint for alarms yet, so total is the size
	// of this page. It is reported as len(rows) rather than guessed, and the
	// ledger records it as a known gap (A31 adds the count).
	h.ok(c, Page(rows, len(rows), page, pageSize))
}

// AlarmDetail answers `GET /api/v1/alarm/info/history/:id` — the detail link in
// an App push notification.
func (h *Handlers) AlarmDetail(c *gin.Context) {
	ctx, cl, consumer, ok := h.entry(c, "alarm.detail")
	if !ok {
		return
	}
	id := c.Param("id")
	a, err := h.deps.Core.GetAlarm(ctx, cl.Token, id)
	if err != nil {
		h.upstreamFailure(c, consumer, "alarm.detail", err)
		return
	}
	names := newPolicyNames(h.deps.Core, cl.Token)
	h.deps.Recorder.Observe(consumer, "alarm.detail", "ok")
	h.ok(c, AlarmRow(a, names.of(ctx, a.PolicyID)))
}

// policyNames resolves a policy id to its name, once per request.
//
// The old alarm row carried the name because it was a join; the new episode only
// has the policy id. Resolving it per row would be an N+1 against the rules
// endpoint on every alarm page, so the cache lives for one request only — a
// long-lived cache would serve a renamed policy under its old name.
type policyNames struct {
	core  *CoreClient
	token string
	seen  map[string]string
}

func newPolicyNames(core *CoreClient, token string) *policyNames {
	return &policyNames{core: core, token: token, seen: map[string]string{}}
}

func (p *policyNames) of(ctx context.Context, policyID string) string {
	if policyID == "" {
		return ""
	}
	if v, ok := p.seen[policyID]; ok {
		return v
	}
	name := ""
	if r, err := p.core.GetRule(ctx, p.token, policyID); err == nil {
		name = r.Name
	}
	p.seen[policyID] = name
	return name
}

// legacyIDOf returns the id the old client knows this rule by.
//
// A rule created after the migration has no legacy id; it is returned by its new
// id so the row is still addressable, and the list keeps working. It is not
// written back anywhere — the mapping is one-way on purpose.
func legacyIDOf(r *rule) string {
	if r.LegacyID != "" {
		return r.LegacyID
	}
	return r.ID
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

// Stats answers `GET /api/v1/compat/stats` with the counters as JSON. The smoke
// script and the exit-condition check read this; Prometheus scrapes /metrics.
func (h *Handlers) Stats(c *gin.Context) {
	h.stamp(c)
	c.JSON(http.StatusOK, gin.H{
		"mode":         "compat",
		"sunset":       h.deps.Sunset,
		"expired":      h.expired(),
		"calls":        h.deps.Recorder.Snapshot(),
		"consumers":    h.deps.Recorder.ConsumersSeen(),
		"collected_at": h.deps.Now(),
	})
}

// Metrics answers `GET /api/v1/compat/metrics` in the Prometheus text format.
func (h *Handlers) Metrics(c *gin.Context) {
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.String(http.StatusOK, h.deps.Recorder.Prometheus(h.deps.Sunset))
}
