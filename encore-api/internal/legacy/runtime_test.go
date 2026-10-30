package legacy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func runtimeTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{baseURL: srv.URL, apiKey: "k", http: srv.Client()}
}

func TestGetOnlineFlag(t *testing.T) {
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"device_status":1,"is_online":1}}`))
	})
	got, err := c.GetOnlineFlag(context.Background(), "d1")
	if err != nil || got != 1 {
		t.Fatalf("flag=%d err=%v", got, err)
	}
	bad := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"other":1}}`))
	})
	if _, err := bad.GetOnlineFlag(context.Background(), "d1"); err == nil {
		t.Fatal("missing is_online must be an error, not offline")
	}
	failed := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"data":null}`))
	})
	if _, err := failed.GetOnlineFlag(context.Background(), "d1"); err == nil {
		t.Fatal("non-200 envelope code must be an error")
	}
}

func TestGetTelemetryCurrentReadingsKeepsRawTokens(t *testing.T) {
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[
			{"key":"temp","ts":"2026-10-28T01:02:03.456Z","value":12345678901234567890},
			{"key":"on","ts":"2026-10-28T01:02:03Z","value":true},
			{"key":"none","ts":"2026-10-28T01:02:03Z"}]}`))
	})
	rows, err := c.GetTelemetryCurrentReadings(context.Background(), "d1")
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if string(rows[0].Value) != "12345678901234567890" || string(rows[1].Value) != "true" || string(rows[2].Value) != "null" {
		t.Fatalf("value tokens changed: %s %s %s", rows[0].Value, rows[1].Value, rows[2].Value)
	}
	missing := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[{"key":"x","value":1}]}`))
	})
	if _, err := missing.GetTelemetryCurrentReadings(context.Background(), "d1"); err == nil {
		t.Fatal("a row without a timestamp must be rejected")
	}
}

func TestListEventDatas(t *testing.T) {
	var query string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"code":200,"data":{"count":7,"list":[{"id":"e1","device_id":"d1","identify":"overheat","ts":"2026-10-28T01:02:03Z","data":"{\"a\":1}"}]}}`))
	})
	rows, total, err := c.ListEventDatas(context.Background(), "d1", "overheat", 2, 10)
	if err != nil || total != 7 || len(rows) != 1 || rows[0].ID != "e1" || rows[0].Datum == nil {
		t.Fatalf("rows=%v total=%d err=%v", rows, total, err)
	}
	for _, want := range []string{"device_id=d1", "identify=overheat", "page=2", "page_size=10"} {
		if !contains(query, want) {
			t.Fatalf("query %q missing %q", query, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSubmitCommandAndStatus(t *testing.T) {
	var body string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/command/datas/pub":
			buf := make([]byte, 512)
			n, _ := r.Body.Read(buf)
			body = string(buf[:n])
			_, _ = w.Write([]byte(`{"code":200,"data":{"message_id":"m-1","status":"accepted"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/command/datas/status/m-1":
			_, _ = w.Write([]byte(`{"code":200,"data":{"message_id":"m-1","status":"device_succeeded"}}`))
		case r.URL.Path == "/api/v1/command/datas/status/other":
			_, _ = w.Write([]byte(`{"code":200,"data":{"message_id":"someone-else","status":"accepted"}}`))
		default:
			http.NotFound(w, r)
		}
	})
	id, err := c.SubmitCommand(context.Background(), "d1", "reboot", `{"delay":5}`, "exec-1")
	if err != nil || id != "m-1" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if !contains(body, `"device_id":"d1"`) || !contains(body, `"identify":"reboot"`) || !contains(body, `"value":"{\"delay\":5}"`) || !contains(body, `"idempotency_key":"exec-1"`) {
		t.Fatalf("unexpected request body %s", body)
	}
	status, err := c.GetCommandStatus(context.Background(), "m-1")
	if err != nil || status.Status != "device_succeeded" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if _, err := c.GetCommandStatus(context.Background(), "other"); err == nil {
		t.Fatal("a status for a different message id must be rejected")
	}
}

func TestGetAttributeCurrentReadings(t *testing.T) {
	var path string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"code":200,"data":[{"id":"a1","device_id":"d1","key":"fw","ts":"2026-10-28T01:02:03Z","value":"1.2.3","unit":null}]}`))
	})
	rows, err := c.GetAttributeCurrentReadings(context.Background(), "d1")
	if err != nil || len(rows) != 1 || string(rows[0].Value) != `"1.2.3"` || path != "/api/v1/attribute/datas/d1" {
		t.Fatalf("rows=%v path=%s err=%v", rows, path, err)
	}
}

func TestListStatusHistory(t *testing.T) {
	var query string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"code":200,"data":{"total":2,"list":[{"id":9,"device_id":"d1","status":1,"change_time":"2026-10-28T01:02:03Z"},{"id":8,"device_id":"d1","status":0,"change_time":"2026-10-28T00:00:00Z"}]}}`))
	})
	from := int64(1700000000)
	online := true
	rows, total, err := c.ListStatusHistory(context.Background(), "d1", 1, 20, &from, &online)
	if err != nil || total != 2 || len(rows) != 2 || !rows[0].Online || rows[1].Online || rows[0].ID != 9 {
		t.Fatalf("rows=%v total=%d err=%v", rows, total, err)
	}
	if !strings.Contains(query, "start_time=1700000000") || !strings.Contains(query, "status=1") || !strings.Contains(query, "device_id=d1") {
		t.Fatalf("query %s", query)
	}
	bad := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"total":1,"list":[{"id":1,"status":7,"change_time":"2026-10-28T01:02:03Z"}]}}`))
	})
	if _, _, err := bad.ListStatusHistory(context.Background(), "d1", 1, 20, nil, nil); err == nil {
		t.Fatal("unknown status value must be rejected")
	}
}

func TestDebugSwitchAndLogs(t *testing.T) {
	var got string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got += r.Method + " " + r.URL.Path + "?" + r.URL.RawQuery + ";"
		switch {
		case strings.HasSuffix(r.URL.Path, "/debug/logs"):
			_, _ = w.Write([]byte(`{"code":200,"data":{"total":3,"offset":0,"limit":2,"list":[{"ts":"2026-10-28T01:02:03.5Z","device_id":"d1","direction":"uplink","action":"publish","outcome":"ok","payload":"secret"},{"ts":"2026-10-28T01:02:04Z","direction":"x","action":"auth","outcome":"error","error":"bad"}]}}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"data":{"enabled":true,"expire_at":1793145600,"remaining_seconds":5}}`))
		}
	})
	st, err := c.SetDebug(context.Background(), "d1", true, 1793145600)
	if err != nil || !st.Enabled || st.ExpireAt != 1793145600 {
		t.Fatalf("%+v %v", st, err)
	}
	if _, err := c.GetDebugState(context.Background(), "d1"); err != nil {
		t.Fatal(err)
	}
	rows, total, err := c.ListDebugLogs(context.Background(), "d1", 0, 2)
	if err != nil || total != 3 || len(rows) != 2 || rows[0].Action != "publish" || rows[1].Error != "bad" {
		t.Fatalf("%+v %d %v", rows, total, err)
	}
	if !strings.Contains(got, "/debug/logs?limit=2&offset=0") {
		t.Fatal(got)
	}
	bad := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"enabled":true}}`))
	})
	if _, err := bad.GetDebugState(context.Background(), "d1"); err == nil {
		t.Fatal("status without expire_at must be rejected")
	}
}

func TestExpectedCommands(t *testing.T) {
	var gotBody, gotQuery string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			b := make([]byte, 512)
			n, _ := r.Body.Read(b)
			gotBody = string(b[:n])
			_, _ = w.Write([]byte(`{"code":200,"data":{"id":"e1","status":"pending"}}`))
			return
		}
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"code":200,"data":{"total":1,"list":[{"id":"e1","status":"sent"}]}}`))
	})
	exp := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	label := "night"
	rec, err := c.CreateExpectedCommand(context.Background(), "d1", "reboot", `{"a":1}`, &exp, &label)
	if err != nil || rec.ID != "e1" || rec.Status != "pending" {
		t.Fatalf("%+v %v", rec, err)
	}
	for _, want := range []string{`"send_type":"command"`, `"identify":"reboot"`, `"payload":"{\"a\":1}"`, `"expiry":"2026-10-29T00:00:00Z"`, `"label":"night"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body %s lacks %s", gotBody, want)
		}
	}
	rows, err := c.ListExpectedCommands(context.Background(), "d1", 1, 100)
	if err != nil || len(rows) != 1 || rows[0].Status != "sent" || !strings.Contains(gotQuery, "send_type=command") {
		t.Fatalf("%+v %v %s", rows, err, gotQuery)
	}
}

func TestDeleteExpected(t *testing.T) {
	var method, path string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	})
	if err := c.DeleteExpected(context.Background(), "e 1"); err != nil || method != http.MethodDelete || path != "/api/v1/expected/data/e 1" {
		t.Fatalf("%s %s %v", method, path, err)
	}
	failed := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":500,"data":null}`))
	})
	if err := failed.DeleteExpected(context.Background(), "e1"); err == nil {
		t.Fatal("a non-200 envelope must be an error")
	}
}

func TestSubmitCommandRejectionIsDefinite(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		rejected bool
	}{
		{"client error", http.StatusBadRequest, `{"code":400}`, true},
		{"envelope refusal", http.StatusOK, `{"code":400,"message":"no"}`, true},
		{"server error is unclear", http.StatusInternalServerError, `oops`, false},
		{"request timeout is unclear", http.StatusRequestTimeout, `slow`, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.SubmitCommand(context.Background(), "d1", "reboot", "", "k")
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrRejected); got != tc.rejected {
				t.Fatalf("errors.Is(ErrRejected)=%v want %v (%v)", got, tc.rejected, err)
			}
		})
	}
}

func TestListTelemetryHistoryPagesAndSlicesUnpagedBackends(t *testing.T) {
	var query map[string]string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = map[string]string{}
		for k := range r.URL.Query() {
			query[k] = r.URL.Query().Get(k)
		}
		_, _ = w.Write([]byte(`{"code":200,"data":{"total":3,"list":[
			{"ts":1760000003000,"key":"temp","value":3},
			{"ts":1760000002000,"key":"temp","value":2.5},
			{"ts":1760000001000,"key":"temp","value":"x"}]}}`))
	})
	rows, total, err := c.ListTelemetryHistory(context.Background(), "d1", "temp", 1000, 2000, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	// The backend ignored paging (3 rows for pageSize 2), so page 2 is the tail.
	if total != 3 || len(rows) != 1 || string(rows[0].Value) != `"x"` || !rows[0].Ts.Equal(time.UnixMilli(1760000001000)) {
		t.Fatalf("total=%d rows=%+v", total, rows)
	}
	if query["start_time"] != "1000" || query["end_time"] != "2000" || query["key"] != "temp" || query["device_id"] != "d1" {
		t.Fatalf("query=%v", query)
	}
	bad := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"total":1,"list":[{"key":"temp","value":1}]}}`))
	})
	if _, _, err := bad.ListTelemetryHistory(context.Background(), "d1", "temp", 1, 2, 1, 10); err == nil {
		t.Fatal("a row without a timestamp must be rejected")
	}
}

func TestDispatcherHelpersSendCommunityRequestsAndSeparateRejection(t *testing.T) {
	var method, path, body string
	respond := `{"code":200,"data":{}}`
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(raw)
		_, _ = w.Write([]byte(respond))
	})
	ctx := context.Background()
	if err := c.SetDeviceNumber(ctx, "d1", "N-2"); err != nil || method != "PUT" || path != "/api/v1/device" ||
		!strings.Contains(body, `"device_number":"N-2"`) || !strings.Contains(body, `"id":"d1"`) {
		t.Fatalf("renumber: %v %s %s %s", err, method, path, body)
	}
	if err := c.BindSubdevice(ctx, "d1", "g1", "A1"); err != nil ||
		!strings.Contains(body, `"parent_id":"g1"`) || !strings.Contains(body, `"sub_device_addr":"A1"`) {
		t.Fatalf("bind: %v %s", err, body)
	}
	if err := c.RemoveDevice(ctx, "d1"); err != nil || method != "DELETE" || path != "/api/v1/device/d1" {
		t.Fatalf("remove: %v %s %s", err, method, path)
	}
	respond = `{"code":500,"data":null}`
	if err := c.SetDeviceNumber(ctx, "d1", "N-3"); !errors.Is(err, ErrRejected) {
		t.Fatalf("a business-code failure must be a rejection, got %v", err)
	}
}

func TestDeviceGoneOnlyTrueWhenCommunityDeniesTheDevice(t *testing.T) {
	respond := `{"code":200,"data":{"id":"d1"}}`
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(respond)) })
	if gone, err := c.DeviceGone(context.Background(), "d1"); err != nil || gone {
		t.Fatalf("existing device: gone=%v err=%v", gone, err)
	}
	respond = `{"code":204003,"data":null}`
	if gone, err := c.DeviceGone(context.Background(), "d1"); err != nil || !gone {
		t.Fatalf("missing device: gone=%v err=%v", gone, err)
	}
	down := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if gone, err := down.DeviceGone(context.Background(), "d1"); err == nil || gone {
		t.Fatalf("an unreachable runtime must stay unknown: gone=%v err=%v", gone, err)
	}
}

func TestSimulationSendNeverPassesBrokerTarget(t *testing.T) {
	var path, body string
	c := runtimeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		path, body = r.URL.Path, string(raw)
		_, _ = w.Write([]byte(`{"code":200,"data":null}`))
	})
	if err := c.SimulationSend(context.Background(), "d1", `{"temp":1}`); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/telemetry/datas/simulation/send" || !strings.Contains(body, `"device_id":"d1"`) {
		t.Fatalf("path=%s body=%s", path, body)
	}
	for _, forbidden := range []string{"server", "port", "topic"} {
		if strings.Contains(body, `"`+forbidden+`"`) {
			t.Fatalf("request must not carry %q: %s", forbidden, body)
		}
	}
}
