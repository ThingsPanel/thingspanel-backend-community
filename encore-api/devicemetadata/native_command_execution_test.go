package devicemetadata

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"encore.app/thingmodel/core"
)

const commandTestSnapshot = "11111111-1111-1111-1111-111111111111"

func TestNativeCommandDecode(t *testing.T) {
	ok, err := decodeNativeCommandSubmission([]byte(`{"identifier":"reboot","snapshotId":"` + commandTestSnapshot + `","parameters":{"a":1},"timeoutSeconds":5,"label":"x"}`))
	if err != nil || ok.Identifier != "reboot" || ok.TimeoutSeconds != 5 || ok.Label == nil {
		t.Fatalf("%v %+v", err, ok)
	}
	d, _ := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{}}`))
	if d.TimeoutSeconds != 30 {
		t.Fatal("default timeout")
	}
	bad := []string{
		`[]`, `{}`, `{"identifier":"r","snapshotId":"` + commandTestSnapshot + `"}`,
		`{"identifier":"r","snapshotId":"x","parameters":{}}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":[]}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"timeoutSeconds":301}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"timeoutSeconds":0}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"expiresAt":"2026-01-01T00:00:00Z"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"extra":1}`,
	}
	for _, b := range bad {
		if _, err := decodeNativeCommandSubmission([]byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestNativeCommandParams(t *testing.T) {
	var f core.Field
	if err := json.Unmarshal([]byte(`{"kind":"COMMAND","identifier":"set","parameters":{"schema_version":1,"items":[{"identifier":"n","value_type":{"kind":"INT"}}]}}`), &f); err != nil {
		t.Fatal(err)
	}
	if err := validateNativeCommandParameters(f, []byte(`{"n":3}`)); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	for _, b := range []string{`{}`, `{"n":"x"}`, `{"n":3,"z":1}`, `[]`} {
		if err := validateNativeCommandParameters(f, []byte(b)); err == nil {
			t.Errorf("accepted %s", b)
		}
	}
}

func TestNativeCommandMapAndText(t *testing.T) {
	cases := map[string]string{"accepted": "accepted", "published": "delivered", "publish_failed": "failed",
		"device_failed": "failed", "device_succeeded": "acknowledged", "timeout": "timed_out", "weird": "unknown"}
	for in, want := range cases {
		if got := mapRuntimeCommandStatus(in); got != want {
			t.Errorf("%s=%s", in, got)
		}
	}
	if runtimeParametersText([]byte(`{}`)) != "" || runtimeParametersText([]byte(`{"n":1}`)) != `{"n":1}` {
		t.Fatal("params text")
	}
	if !(commandStateRank("pending") < commandStateRank("accepted") && commandStateRank("delivered") < commandStateRank("failed")) {
		t.Fatal("rank")
	}
}

func TestNativeCommandWhenOnline(t *testing.T) {
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	ok, err := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"` + future + `"}`))
	if err != nil || ok.DeliveryMode != "when_online" || ok.ExpiresAt == nil {
		t.Fatalf("%+v %v", ok, err)
	}
	if d, err := decodeNativeCommandSubmission([]byte(`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online"}`)); err != nil || d.ExpiresAt != nil {
		t.Fatalf("%+v %v", d, err)
	}
	for _, bad := range []string{
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"2020-01-01T00:00:00Z"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"deliveryMode":"when_online","expiresAt":"soon"}`,
		`{"identifier":"r","snapshotId":"` + commandTestSnapshot + `","parameters":{},"expiresAt":"` + future + `"}`,
	} {
		if _, err := decodeNativeCommandSubmission([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	for in, want := range map[string]string{"pending": "accepted", "sent": "delivered", "expired": "expired", "x": "unknown"} {
		if mapExpectedStatus(in) != want {
			t.Errorf("%s", in)
		}
	}
}

func TestNativeCancelReason(t *testing.T) {
	if r, err := decodeNativeCancelReason([]byte(`{"reason":"no longer needed"}`)); err != nil || r != "no longer needed" {
		t.Fatalf("%q %v", r, err)
	}
	for _, bad := range []string{`[]`, `{}`, `{"reason":""}`, `{"reason":"  "}`, `{"reason":1}`, `{"reason":"x","y":1}`} {
		if _, err := decodeNativeCancelReason([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestNativeCommandListDeliveryModeFilter(t *testing.T) {
	q, err := decodeNativeCommandListQuery(url.Values{"deliveryMode": {"when_online"}, "state": {"accepted"}})
	if err != nil || q.deliveryMode != "when_online" || q.state != "accepted" {
		t.Fatalf("%v %+v", err, q)
	}
	for _, bad := range []string{"", "queued", "WHEN_ONLINE"} {
		if _, err := decodeNativeCommandListQuery(url.Values{"deliveryMode": {bad}}); err == nil {
			t.Errorf("accepted deliveryMode %q", bad)
		}
	}
}
