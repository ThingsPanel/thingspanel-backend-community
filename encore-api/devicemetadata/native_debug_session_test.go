package devicemetadata

import (
	"strings"
	"testing"
	"time"

	"encore.app/internal/legacy"
)

func TestDebugSessionID(t *testing.T) {
	a := debugSessionID("d1", 100)
	if a != debugSessionID("d1", 100) || a == debugSessionID("d1", 101) || a == debugSessionID("d2", 100) {
		t.Fatal("session id must be stable and depend on device and expiry")
	}
}

func TestDecodeNativeDebugSessionRequest(t *testing.T) {
	if s, err := decodeNativeDebugSessionRequest([]byte(`{"level":"info"}`)); err != nil || s != 300 {
		t.Fatalf("%d %v", s, err)
	}
	if s, err := decodeNativeDebugSessionRequest([]byte(`{"level":"info","durationSeconds":60}`)); err != nil || s != 60 {
		t.Fatalf("%d %v", s, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"level":"debug"}`, `{"level":"trace"}`, `{"level":"info","durationSeconds":0}`, `{"level":"info","durationSeconds":604801}`, `{"level":"info","x":1}`} {
		if _, err := decodeNativeDebugSessionRequest([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestDebugLogFromEntry(t *testing.T) {
	at := time.Date(2026, 10, 28, 1, 2, 3, 0, time.UTC)
	ok := debugLogFromEntry("d1", legacy.DebugEntry{Raw: `{"a":1}`, Time: at, Direction: "uplink", Action: "publish", Outcome: "ok"})
	if ok.Level != "info" || ok.Direction != "uplink" || ok.Message != "publish ok" || !strings.HasPrefix(ok.ID, "legacy-debug:") {
		t.Fatalf("%+v", ok)
	}
	bad := debugLogFromEntry("d1", legacy.DebugEntry{Raw: `{"a":2}`, Time: at, Direction: "x", Action: "auth", Outcome: "error", Error: "bad"})
	if bad.Level != "error" || bad.Direction != "internal" || bad.Message != "auth error: bad" || bad.ID == ok.ID {
		t.Fatalf("%+v", bad)
	}
	if got := debugLogFromEntry("d1", legacy.DebugEntry{Raw: "z", Time: at, Outcome: "drop"}); got.Level != "warning" || got.Message != "drop" {
		t.Fatalf("%+v", got)
	}
	if got := debugLogFromEntry("d1", legacy.DebugEntry{Raw: "e", Time: at}); got.Message != "(no detail)" {
		t.Fatalf("%+v", got)
	}
}
