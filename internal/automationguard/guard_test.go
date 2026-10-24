package automationguard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	commandguardsql "project/sql"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The guard is the only place where the order of two competing commands can be
// decided, so these tests exercise it through a real router and a real database.
// A fake would not prove that the SQL statements actually hold under conflict,
// which is exactly the property being claimed.
//
// Set TEST_AUTOMATION_GUARD_DSN to run them, for example:
//
//	TEST_AUTOMATION_GUARD_DSN='host=127.0.0.1 port=55432 user=postgres \
//	  password=... dbname=qa_automation_guard_test sslmode=disable' go test ./internal/automationguard/
func openGuardDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("TEST_AUTOMATION_GUARD_DSN"))
	if dsn == "" {
		t.Skip("TEST_AUTOMATION_GUARD_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL: %v", err)
	}
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		t.Fatalf("read current database: %v", err)
	}
	if name != "qa_automation_guard_test" {
		t.Fatalf("refusing destructive guard test against database %q", name)
	}
	migrations, err := commandguardsql.CommandGuardMigrations()
	if err != nil {
		t.Fatalf("load guard migrations: %v", err)
	}
	for _, m := range migrations {
		if err := db.Exec(string(m.Contents)).Error; err != nil {
			t.Fatalf("apply %s: %v", m.FileName, err)
		}
	}
	if err := db.Exec("TRUNCATE automation_scope_generation, automation_command_guard").Error; err != nil {
		t.Fatalf("reset guard tables: %v", err)
	}
	return db
}

type guardHarness struct {
	db    *gorm.DB
	route *gin.Engine
	calls int32
	// reply is what the "owner" answers. status/code are configurable so the
	// tests can produce a business error the way the legacy handlers do.
	status int
	code   int
	ref    string
}

func newGuardHarness(t *testing.T, db *gorm.DB) *guardHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &guardHarness{db: db, status: http.StatusOK, code: 200, ref: "msg-1"}
	r := gin.New()
	r.POST("/api/v1/telemetry/datas/pub", Guard(db), h.handle)
	r.POST("/api/v1/command/datas/pub", Guard(db), h.handle)
	h.route = r
	return h
}

func (h *guardHarness) handle(c *gin.Context) {
	atomic.AddInt32(&h.calls, 1)
	if h.code == 200 {
		c.JSON(h.status, gin.H{"code": 200, "message": "success", "data": gin.H{"message_id": h.ref}})
		return
	}
	c.JSON(h.status, gin.H{"code": h.code, "message": "设备不存在"})
}

func (h *guardHarness) count() int { return int(atomic.LoadInt32(&h.calls)) }

type guardRequest struct {
	key        string
	scope      string
	generation string
	body       string
	path       string
}

func (h *guardHarness) post(r guardRequest) *httptest.ResponseRecorder {
	if r.path == "" {
		r.path = "/api/v1/telemetry/datas/pub"
	}
	if r.body == "" {
		r.body = `{"device_id":"dev-1","value":"{\"switch\":1}"}`
	}
	req := httptest.NewRequest(http.MethodPost, r.path, bytes.NewReader([]byte(r.body)))
	req.Header.Set("Content-Type", "application/json")
	if r.key != "" {
		req.Header.Set(HeaderIdempotencyKey, r.key)
	}
	if r.scope != "" {
		req.Header.Set(HeaderScope, r.scope)
	}
	if r.generation != "" {
		req.Header.Set(HeaderGeneration, r.generation)
	}
	w := httptest.NewRecorder()
	h.route.ServeHTTP(w, req)
	return w
}

func readGuardRow(t *testing.T, db *gorm.DB, key string) guardRowView {
	t.Helper()
	var row guardRowView
	err := db.Raw(`SELECT state, http_status, response_body, accepted_ref, generation, scope_key, action_kind
	                 FROM automation_command_guard WHERE idempotency_key = ?`, key).Scan(&row).Error
	if err != nil {
		t.Fatalf("read guard row %s: %v", key, err)
	}
	return row
}

type guardRowView struct {
	State       string
	HTTPStatus  int
	Response    string `gorm:"column:response_body"`
	AcceptedRef string
	Generation  int64
	ScopeKey    string
	ActionKind  string
}

func guardRows(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int
	if err := db.Raw(`SELECT count(*) FROM automation_command_guard`).Scan(&n).Error; err != nil {
		t.Fatalf("count guard rows: %v", err)
	}
	return n
}

func envelopeCode(t *testing.T, body string) int {
	t.Helper()
	var env struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, body)
	}
	return env.Code
}

// ---------------------------------------------------------------------------
// Contracts
// ---------------------------------------------------------------------------

// The console, the App and scripts must keep working exactly as before. That is
// not a side note: the rewrite may not require anyone else to change.
func TestLegacyCallerIsNotAffectedByTheGuard(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))

	w := h.post(guardRequest{})
	if w.Code != http.StatusOK {
		t.Fatalf("legacy publish must succeed unchanged, got %d: %s", w.Code, w.Body.String())
	}
	if h.count() != 1 {
		t.Fatalf("handler must run once, ran %d", h.count())
	}
	if n := guardRows(t, h.db); n != 0 {
		t.Fatalf("a request without an idempotency key must not be recorded, got %d rows", n)
	}
}

// A transport retry must not become a second physical action.
func TestSameKeyAndSameContentReplaysTheFirstAnswer(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	req := guardRequest{key: "run-1:2:cmd:dev-1", scope: "rule-1:dev-1", generation: "5"}

	first := h.post(req)
	second := h.post(req)

	if h.count() != 1 {
		t.Fatalf("the device must be commanded once, handler ran %d times", h.count())
	}
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("both attempts must report the first answer, got %d and %d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay must be byte-identical:\nfirst:  %s\nsecond: %s", first.Body.String(), second.Body.String())
	}
	row := readGuardRow(t, h.db, req.key)
	if row.State != "accepted" || row.AcceptedRef != "msg-1" {
		t.Fatalf("the acceptance and its owner id must be kept, got %+v", row)
	}
}

// Reusing a key for different content would silently replace an action that may
// already have reached the device, so it is refused instead.
func TestSameKeyWithDifferentContentIsAConflict(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	key := "run-1:2:cmd:dev-1"

	if w := h.post(guardRequest{key: key}); w.Code != http.StatusOK {
		t.Fatalf("first attempt must succeed, got %d", w.Code)
	}
	w := h.post(guardRequest{key: key, body: `{"device_id":"dev-1","value":"{\"switch\":0}"}`})

	if w.Code != http.StatusConflict {
		t.Fatalf("a key reuse with different content must be refused, got %d: %s", w.Code, w.Body.String())
	}
	if code := envelopeCode(t, w.Body.String()); code != codeKeyConflict {
		t.Fatalf("want conflict code %d, got %d", codeKeyConflict, code)
	}
	if h.count() != 1 {
		t.Fatalf("the conflicting content must not reach the device, handler ran %d times", h.count())
	}
}

// The generation check is the whole point: a late command from an older run must
// never arrive after a newer run already acted.
func TestAnOlderGenerationNeverReachesTheDevice(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	scope := "rule-1:dev-1"

	if w := h.post(guardRequest{key: "new", scope: scope, generation: "8"}); w.Code != http.StatusOK {
		t.Fatalf("the newest generation must be accepted, got %d", w.Code)
	}
	w := h.post(guardRequest{key: "old", scope: scope, generation: "7"})

	if w.Code != http.StatusConflict {
		t.Fatalf("an older generation must be refused, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get(HeaderSuperseded) != "true" {
		t.Fatal("the caller must be told it was superseded, not merely that it failed")
	}
	if code := envelopeCode(t, w.Body.String()); code != codeSuperseded {
		t.Fatalf("want superseded code %d, got %d", codeSuperseded, code)
	}
	if h.count() != 1 {
		t.Fatalf("a superseded command must not reach the device, handler ran %d times", h.count())
	}
}

// Generation is a floor, not a uniqueness constraint: a rule may legitimately
// send several actions within the same run.
func TestSeveralActionsOfTheSameGenerationAreAllAccepted(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	scope := "rule-1:dev-1"

	a := h.post(guardRequest{key: "run-9:1:cmd:dev-1", scope: scope, generation: "9"})
	b := h.post(guardRequest{key: "run-9:2:cmd:dev-1", scope: scope, generation: "9",
		body: `{"device_id":"dev-1","value":"{\"switch\":0}"}`})

	if a.Code != http.StatusOK || b.Code != http.StatusOK {
		t.Fatalf("both actions of the same generation must be accepted, got %d and %d", a.Code, b.Code)
	}
	if h.count() != 2 {
		t.Fatalf("both actions must reach the device, handler ran %d times", h.count())
	}
}

// An identical attempt that is still running is not a reason to send again.
func TestAnAttemptStillInFlightIsNotSentTwice(t *testing.T) {
	db := openGuardDB(t)
	h := newGuardHarness(t, db)
	key := "run-1:2:cmd:dev-1"
	body := `{"device_id":"dev-1","value":"{\"switch\":1}"}`

	insertGuardRow(t, db, key, "in_flight", "rule-1:dev-1", 5,
		payloadHash("telemetry", "dev-1", []byte(body)), "30 seconds")

	w := h.post(guardRequest{key: key, scope: "rule-1:dev-1", generation: "5"})
	if w.Code != http.StatusConflict {
		t.Fatalf("an in-flight duplicate must be refused, got %d: %s", w.Code, w.Body.String())
	}
	if code := envelopeCode(t, w.Body.String()); code != codeInFlight {
		t.Fatalf("want in-flight code %d, got %d", codeInFlight, code)
	}
	if h.count() != 0 {
		t.Fatalf("the device must not be commanded while the first attempt runs, handler ran %d times", h.count())
	}
}

// The first attempt died while holding the key. Nobody knows whether the device
// acted, and the honest answer stays "unknown" forever rather than turning into
// a resend on the second or third try.
func TestAnAttemptThatDiedMidFlightStaysUnknown(t *testing.T) {
	db := openGuardDB(t)
	h := newGuardHarness(t, db)
	key := "run-1:2:cmd:dev-1"
	body := `{"device_id":"dev-1","value":"{\"switch\":1}"}`

	insertGuardRow(t, db, key, "in_flight", "rule-1:dev-1", 5,
		payloadHash("telemetry", "dev-1", []byte(body)), "-1 minute")

	for i := 0; i < 3; i++ {
		w := h.post(guardRequest{key: key, scope: "rule-1:dev-1", generation: "5"})
		if w.Code != http.StatusConflict {
			t.Fatalf("attempt %d: an abandoned attempt must be refused, got %d", i+1, w.Code)
		}
		if code := envelopeCode(t, w.Body.String()); code != codeUnknown {
			t.Fatalf("attempt %d: want unknown code %d, got %d", i+1, codeUnknown, code)
		}
	}
	if h.count() != 0 {
		t.Fatalf("a non-idempotent action must never be re-sent blindly, handler ran %d times", h.count())
	}
}

// The owner reports business errors as HTTP 200 with a code, so the guard has to
// read the envelope instead of trusting the status line.
func TestABusinessErrorIsRecordedAsFailedAndReplayed(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	h.code = 500
	req := guardRequest{key: "run-1:3:cmd:dev-1"}

	first := h.post(req)
	second := h.post(req)

	if first.Code != http.StatusOK {
		t.Fatalf("the owner's status must be passed through, got %d", first.Code)
	}
	row := readGuardRow(t, h.db, req.key)
	if row.State != "failed" {
		t.Fatalf("a business error must be recorded as failed, got %q", row.State)
	}
	if h.count() != 1 || first.Body.String() != second.Body.String() {
		t.Fatalf("the failure must be replayed, not retried (handler ran %d times)", h.count())
	}
}

// A superseded answer must survive a transport retry too, otherwise the retry
// would look like a fresh attempt and be refused with a different reason.
func TestASupersededAnswerIsReplayable(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	scope := "rule-1:dev-1"
	if w := h.post(guardRequest{key: "new", scope: scope, generation: "8"}); w.Code != http.StatusOK {
		t.Fatalf("setup: %d", w.Code)
	}
	req := guardRequest{key: "old", scope: scope, generation: "7"}
	first := h.post(req)
	second := h.post(req)

	if first.Code != http.StatusConflict || second.Code != http.StatusConflict {
		t.Fatalf("both attempts must be refused, got %d and %d", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("superseded answer must be replayed verbatim:\n%s\n%s", first.Body.String(), second.Body.String())
	}
	if second.Header().Get(HeaderSuperseded) != "true" {
		t.Fatal("the replayed answer must still say it was superseded")
	}
	if h.count() != 1 {
		t.Fatalf("only the newest generation may reach the device, handler ran %d times", h.count())
	}
}

// Without a database the guard cannot tell a repeat from a new action. Automation
// callers are refused rather than risked; legacy callers are untouched.
func TestGuardWithoutADatabaseRefusesAutomationCallersOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &guardHarness{}
	r := gin.New()
	r.POST("/api/v1/telemetry/datas/pub", Guard(nil), h.handle)
	h.route = r

	if w := h.post(guardRequest{}); w.Code != http.StatusOK {
		t.Fatalf("legacy callers must still pass, got %d", w.Code)
	}
	w := h.post(guardRequest{key: "run-1:1:cmd:dev-1"})
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("automation callers must be refused when the guard is unavailable, got %d", w.Code)
	}
	if h.count() != 1 {
		t.Fatalf("only the legacy call may reach the device, handler ran %d times", h.count())
	}
}

// The stored record must say which kind of action it was and which scope it
// belonged to, without trusting the request body for it.
func TestTheRecordCarriesTheRouteKindAndTheScope(t *testing.T) {
	h := newGuardHarness(t, openGuardDB(t))
	req := guardRequest{key: "run-2:1:cmd:dev-1", scope: "rule-2:dev-1", generation: "3",
		path: "/api/v1/command/datas/pub", body: `{"device_id":"dev-1","identify":"open","value":"{}"}`}

	if w := h.post(req); w.Code != http.StatusOK {
		t.Fatalf("command publish must succeed, got %d: %s", w.Code, w.Body.String())
	}
	row := readGuardRow(t, h.db, req.key)
	if row.ActionKind != "command" {
		t.Fatalf("action kind must come from the route, got %q", row.ActionKind)
	}
	if row.ScopeKey != "rule-2:dev-1" || row.Generation != 3 {
		t.Fatalf("scope and generation must be recorded, got %+v", row)
	}
}

func insertGuardRow(t *testing.T, db *gorm.DB, key, state, scope string, generation int64, hash, leaseOffset string) {
	t.Helper()
	err := db.Exec(fmt.Sprintf(`
        INSERT INTO automation_command_guard
            (idempotency_key, scope_key, generation, action_kind, device_id, payload_hash,
             state, lease_until, created_at, updated_at)
        VALUES (?, ?, ?, 'telemetry', 'dev-1', ?, ?, now() + interval '%s', now(), now())`, leaseOffset),
		key, scope, generation, hash, state).Error
	if err != nil {
		t.Fatalf("insert guard row %s: %v", key, err)
	}
}
