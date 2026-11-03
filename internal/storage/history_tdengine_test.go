package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTDengineSink(t *testing.T) *TDengineHistorySink {
	t.Helper()
	base := os.Getenv("TP_TDENGINE_TEST_URL")
	if base == "" {
		t.Skip("TP_TDENGINE_TEST_URL required")
	}
	user, pass := os.Getenv("TP_TDENGINE_TEST_USER"), os.Getenv("TP_TDENGINE_TEST_PASSWORD")
	if user == "" {
		user, pass = "root", "taosdata"
	}
	s := &TDengineHistorySink{BaseURL: base, User: user, Password: pass, Database: "hist_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	t.Cleanup(func() { _ = s.exec(context.Background(), "DROP DATABASE IF EXISTS "+s.Database) })
	return s
}

func tdQuery(t *testing.T, s *TDengineHistorySink, statement string) [][]interface{} {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/rest/sql", bytes.NewBufferString(statement))
	req.SetBasicAuth(s.User, s.Password)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int             `json:"code"`
		Desc string          `json:"desc"`
		Data [][]interface{} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Code != 0 {
		t.Fatalf("query %q: code=%d desc=%s err=%v", statement, out.Code, out.Desc, err)
	}
	return out.Data
}

func tdIntent(id int64, device, key string, ts int64) HistoryIntent {
	return HistoryIntent{ID: id, TenantID: "tenant-td", DeviceID: device, Key: key, TS: ts}
}

func TestTDengineSinkStoresAllValueTypesAndIsIdempotent(t *testing.T) {
	s := newTDengineSink(t)
	if err := s.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("schema creation must be repeatable: %v", err)
	}
	base := time.Now().Add(-time.Hour).UnixMilli()
	num, flag, text := 21.5, true, "hello"
	intents := []HistoryIntent{
		func() HistoryIntent { i := tdIntent(1, "dev-1", "temp", base); i.NumberV = &num; return i }(),
		func() HistoryIntent { i := tdIntent(2, "dev-1", "switch", base+1); i.BoolV = &flag; return i }(),
		func() HistoryIntent { i := tdIntent(3, "dev-2", "label", base+2); i.StringV = &text; return i }(),
		func() HistoryIntent { i := tdIntent(4, "dev-1", "temp", base+3); n := 22.0; i.NumberV = &n; return i }(),
	}
	for round := 0; round < 3; round++ {
		if err := s.Append(context.Background(), intents); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	if got := tdQuery(t, s, "SELECT COUNT(*) FROM "+s.Database+".telemetry")[0][0]; fmt.Sprint(got) != "4" {
		t.Fatalf("replaying must not duplicate rows, got %v", got)
	}
	rows := tdQuery(t, s, "SELECT number_v, device_id, k, tenant_id FROM "+s.Database+".telemetry WHERE k='temp' ORDER BY ts")
	if len(rows) != 2 || rows[0][0].(float64) != 21.5 || rows[1][0].(float64) != 22 || rows[0][1] != "dev-1" || rows[0][3] != "tenant-td" {
		t.Fatalf("temperature rows: %v", rows)
	}
	if rows := tdQuery(t, s, "SELECT bool_v FROM "+s.Database+".telemetry WHERE k='switch'"); len(rows) != 1 || rows[0][0] != true {
		t.Fatalf("bool row: %v", rows)
	}
	if rows := tdQuery(t, s, "SELECT string_v FROM "+s.Database+".telemetry WHERE k='label'"); len(rows) != 1 || rows[0][0] != "hello" {
		t.Fatalf("string row: %v", rows)
	}
}

func TestTDengineSinkQuotesHostileValuesAndKeepsLossyCaseExplicit(t *testing.T) {
	s := newTDengineSink(t)
	ts := time.Now().Add(-time.Hour).UnixMilli()
	hostile := `it's "quoted" \ back'); DROP DATABASE ` + s.Database + `; -- 中文`
	nan := math.NaN()
	a := tdIntent(1, "dev'x", `key\with'quote`, ts)
	a.StringV = &hostile
	b := tdIntent(2, "dev-nan", "n", ts)
	b.NumberV = &nan
	if err := s.Append(context.Background(), []HistoryIntent{a, b}); err != nil {
		t.Fatal(err)
	}
	rows := tdQuery(t, s, "SELECT string_v, device_id, k FROM "+s.Database+".telemetry WHERE device_id='dev\\'x'")
	if len(rows) != 1 || rows[0][0] != hostile || rows[0][1] != "dev'x" || rows[0][2] != `key\with'quote` {
		t.Fatalf("hostile values must round trip exactly: %v", rows)
	}
	if rows := tdQuery(t, s, "SELECT number_v FROM "+s.Database+".telemetry WHERE device_id='dev-nan'"); len(rows) != 1 || rows[0][0] != nil {
		t.Fatalf("a non-finite number is stored as NULL: %v", rows)
	}
}

func TestTDengineSinkRejectsUnstorableIntentsBeforeSending(t *testing.T) {
	s := newTDengineSink(t)
	ts := time.Now().Add(-time.Hour).UnixMilli()
	good := tdIntent(1, "dev-good", "k", ts)
	n := 1.0
	good.NumberV = &n
	long := strings.Repeat("a", tdengineMaxID+1)
	bad := tdIntent(2, long, "k", ts)
	if err := s.Append(context.Background(), []HistoryIntent{good, bad}); err == nil {
		t.Fatal("an over-long device id must be rejected")
	}
	if got := tdQuery(t, s, "SELECT COUNT(*) FROM "+s.Database+".telemetry")[0][0]; fmt.Sprint(got) != "0" {
		t.Fatalf("a rejected batch must write nothing, got %v", got)
	}
	huge := strings.Repeat("é", tdengineMaxString+1)
	c := tdIntent(3, "dev-c", "k", ts)
	c.StringV = &huge
	if err := s.Append(context.Background(), []HistoryIntent{c}); err == nil {
		t.Fatal("an over-long string value must be rejected")
	}
	if err := (&TDengineHistorySink{BaseURL: "http://127.0.0.1:1", User: "u", Password: "p", Database: "x"}).Append(context.Background(), []HistoryIntent{good}); err == nil {
		t.Fatal("an unreachable backend must return an error so the intent stays pending")
	}
	if err := (&TDengineHistorySink{BaseURL: s.BaseURL, User: s.User, Password: s.Password, Database: "bad name"}).Append(context.Background(), []HistoryIntent{good}); err == nil {
		t.Fatal("a database name that is not a plain identifier must be rejected")
	}
}

// PostgreSQL holds the intents; TDengine receives them through the replay service.
func TestIntentsReplayFromPostgresIntoTDengine(t *testing.T) {
	if os.Getenv("TP_TDENGINE_TEST_URL") == "" {
		t.Skip("TP_TDENGINE_TEST_URL required")
	}
	sink := newTDengineSink(t)
	db := newHistoryIntentDB(t)
	ts := time.Now().Add(-time.Hour).UnixMilli()
	points := make([]TelemetryData, 30)
	for i := range points {
		v := float64(i)
		points[i] = TelemetryData{DeviceID: fmt.Sprintf("dev-%d", i%3), Key: "temp", TS: ts + int64(i), NumberV: &v, TenantID: "tenant-td"}
	}
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, points); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	// First attempt fails because the backend is down; nothing may be lost.
	down := &TDengineHistorySink{BaseURL: "http://127.0.0.1:1", User: sink.User, Password: sink.Password, Database: sink.Database}
	if _, err := ReplayHistoryIntents(context.Background(), db, down, "w1", 100, time.Minute); err == nil {
		t.Fatal("outage must surface as an error")
	}
	if countState(t, db, "pending") != 30 {
		t.Fatal("an outage must leave every intent pending")
	}
	if err := db.Exec(`UPDATE telemetry_history_intents SET claimed_until = NOW() - interval '1 second'`).Error; err != nil {
		t.Fatal(err)
	}
	for {
		n, err := ReplayHistoryIntents(context.Background(), db, sink, "w2", 7, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	if got := tdQuery(t, sink, "SELECT COUNT(*) FROM "+sink.Database+".telemetry")[0][0]; fmt.Sprint(got) != "30" {
		t.Fatalf("tdengine rows=%v want 30", got)
	}
	if countState(t, db, "applied") != 30 {
		t.Fatal("every intent must be acknowledged after delivery")
	}
	if got := tdQuery(t, sink, "SELECT COUNT(*) FROM "+sink.Database+".telemetry WHERE device_id='dev-0'")[0][0]; fmt.Sprint(got) != "10" {
		t.Fatalf("per-device rows=%v want 10", got)
	}
}

func TestDeviceDeletionReplaysIntoTDengineAndSparesOtherDevices(t *testing.T) {
	sink := newTDengineSink(t)
	db := newHistoryIntentDB(t)
	ts := time.Now().Add(-time.Hour).UnixMilli()
	var points []TelemetryData
	for i := 0; i < 12; i++ {
		v := float64(i)
		points = append(points, TelemetryData{DeviceID: fmt.Sprintf("dev-%d", i%2), Key: fmt.Sprintf("k%d", i%3), TS: ts + int64(i), NumberV: &v, TenantID: "tenant-td"})
	}
	tx := db.Begin()
	if _, err := EnqueueHistoryIntents(tx, 0, points); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	for {
		n, err := ReplayHistoryIntents(context.Background(), db, sink, "w", 50, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	rows := func(device string) string {
		return fmt.Sprint(tdQuery(t, sink, "SELECT COUNT(*) FROM "+sink.Database+".telemetry WHERE device_id='"+device+"'")[0][0])
	}
	if rows("dev-0") != "6" || rows("dev-1") != "6" {
		t.Fatalf("seed rows dev-0=%s dev-1=%s", rows("dev-0"), rows("dev-1"))
	}
	// A device deletion whose transaction rolls back must not touch TDengine.
	tx = db.Begin()
	if err := EnqueueHistoryDeviceDeletion(tx, "dev-0"); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	if n, err := ReplayHistoryDeletions(context.Background(), db, sink, "d", 10, time.Minute); err != nil || n != 0 || rows("dev-0") != "6" {
		t.Fatalf("rollback must keep history: n=%d err=%v rows=%s", n, err, rows("dev-0"))
	}
	tx = db.Begin()
	if err := EnqueueHistoryDeviceDeletion(tx, "dev-0"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if rows("dev-0") != "6" {
		t.Fatal("nothing may be deleted before the replay service runs")
	}
	for i := 0; i < 2; i++ { // the second pass proves repeating is harmless
		if _, err := ReplayHistoryDeletions(context.Background(), db, sink, "d", 10, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if rows("dev-0") != "0" || rows("dev-1") != "6" {
		t.Fatalf("dev-0=%s dev-1=%s: only the deleted device may lose history", rows("dev-0"), rows("dev-1"))
	}
	if count(t, db, `SELECT count(*) FROM telemetry_history_deletions WHERE state='applied'`) != 1 {
		t.Fatal("the deletion must be acknowledged")
	}
	if err := sink.DeleteDevice(context.Background(), "never-existed"); err != nil {
		t.Fatalf("deleting an unknown device must succeed: %v", err)
	}
}
