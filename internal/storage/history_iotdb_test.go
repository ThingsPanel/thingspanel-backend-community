package storage

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestIoTDBHistorySinkEnsureSchema(t *testing.T) {
	var requests []iotdbRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/table/v1/nonQuery" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "root" || password != "secret" {
			t.Fatalf("unexpected basic auth: user=%q ok=%v", user, ok)
		}
		var request iotdbRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, request)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"SUCCESS_STATUS"}`))
	}))
	defer server.Close()

	sink := &IoTDBHistorySink{BaseURL: server.URL, User: "root", Password: "secret", Database: "telemetry_test"}
	if err := sink.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Database != "" || requests[0].SQL != "CREATE DATABASE IF NOT EXISTS telemetry_test" {
		t.Fatalf("database setup request = %+v", requests)
	}
	if requests[1].Database != "telemetry_test" || !strings.Contains(requests[1].SQL, "CREATE TABLE IF NOT EXISTS telemetry") || !strings.Contains(requests[1].SQL, "time TIMESTAMP TIME") {
		t.Fatalf("table setup request = %+v", requests[1])
	}
}

func TestIoTDBHistorySinkAppendUsesTableTabletAndIsReplaySafe(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/table/v1/insertTablet" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"SUCCESS_STATUS"}`))
	}))
	defer server.Close()

	nan := math.NaN()
	truth := true
	text := "line\n'value"
	sink := &IoTDBHistorySink{BaseURL: server.URL, User: "root", Database: "history"}
	intents := []HistoryIntent{
		{ID: 1, TenantID: "tenant", DeviceID: "device", Key: "temp", TS: 1720000000123, NumberV: &nan},
		{ID: 2, TenantID: "tenant", DeviceID: "device", Key: "status", TS: 1720000000456, BoolV: &truth, StringV: &text},
	}
	if err := sink.Append(context.Background(), intents); err != nil {
		t.Fatal(err)
	}
	if payload["database"] != "history" || payload["table"] != "telemetry" {
		t.Fatalf("tablet identity = %v", payload)
	}
	if got, want := payload["column_categories"], []interface{}{"TAG", "TAG", "TAG", "FIELD", "FIELD", "FIELD"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("column categories = %v, want %v", got, want)
	}
	if got, want := payload["timestamps"], []interface{}{float64(1720000000123), float64(1720000000456)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("timestamps = %v, want %v", got, want)
	}
	values := payload["values"].([]interface{})
	first := values[0].([]interface{})
	if first[0] != "tenant" || first[1] != "device" || first[2] != "temp" || first[3] != nil {
		t.Fatalf("first row = %v", first)
	}
	second := values[1].([]interface{})
	if second[4] != true || second[5] != text {
		t.Fatalf("second row = %v", second)
	}
}

func TestIoTDBHistorySinkCoalescesDuplicateRowKeysWithinTablet(t *testing.T) {
	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"message":"SUCCESS_STATUS"}`))
	}))
	defer server.Close()

	old := 1.0
	newText := "replacement"
	sink := &IoTDBHistorySink{BaseURL: server.URL, Database: "history"}
	intents := []HistoryIntent{
		{ID: 1, TenantID: "tenant", DeviceID: "device", Key: "temp", TS: 10, NumberV: &old},
		{ID: 2, TenantID: "tenant", DeviceID: "device", Key: "temp", TS: 10, StringV: &newText},
	}
	if err := sink.Append(context.Background(), intents); err != nil {
		t.Fatal(err)
	}
	if got := len(payload["timestamps"].([]interface{})); got != 1 {
		t.Fatalf("tablet contains %d duplicate row keys", got)
	}
	row := payload["values"].([]interface{})[0].([]interface{})
	if row[3] != nil || row[5] != "replacement" {
		t.Fatalf("tablet did not keep the newest row: %v", row)
	}
}

func TestIoTDBHistorySinkQueryAndDelete(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload iotdbRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/table/v1/query" {
			if payload.Database != "history" || payload.SQL != "SELECT COUNT(*) FROM telemetry" || payload.RowLimit != iotdbQueryRowLimit {
				t.Fatalf("query request = %+v", payload)
			}
			_, _ = w.Write([]byte(`{"column_names":["count"],"data_types":["INT64"],"values":[[42]]}`))
			return
		}
		if r.URL.Path != "/rest/table/v1/nonQuery" || payload.Database != "history" || !strings.Contains(payload.SQL, "device_id='device'' OR 1=1 --'") {
			t.Fatalf("delete request = path %q payload %+v", r.URL.Path, payload)
		}
		_, _ = w.Write([]byte(`{"code":"200","message":"SUCCESS_STATUS"}`))
	}))
	defer server.Close()

	sink := &IoTDBHistorySink{BaseURL: server.URL, User: "root", Database: "history"}
	rows, err := sink.Query(context.Background(), "SELECT COUNT(*) FROM telemetry")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0][0] != json.Number("42") {
		t.Fatalf("query rows = %#v", rows)
	}
	if err := sink.DeleteDevice(context.Background(), "device' OR 1=1 --"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestIoTDBHistorySinkRejectsInvalidDatabaseBeforeNetwork(t *testing.T) {
	for _, name := range []string{"", "bad name", "x; DROP DATABASE y"} {
		sink := &IoTDBHistorySink{BaseURL: "http://127.0.0.1:1", Database: name}
		if err := sink.EnsureSchema(context.Background()); err == nil {
			t.Fatalf("database %q must be rejected", name)
		}
		if err := sink.Append(context.Background(), nil); err == nil {
			t.Fatalf("database %q must be rejected", name)
		}
	}
}
