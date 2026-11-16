package devicemetadata

import (
	"context"
	"encore.app/internal/communityclient"
	"errors"
	"net/url"
	"testing"
	"time"
)

type summaryFixture struct {
	runtime []communityclient.RuntimeSummary
	alarms  []communityclient.AlarmSummary
	err     error
}

func (f summaryFixture) GetRuntimeSummaries(context.Context, []string) ([]communityclient.RuntimeSummary, error) {
	return f.runtime, f.err
}
func (f summaryFixture) GetAlarmSummaries(context.Context, string, []string) ([]communityclient.AlarmSummary, error) {
	return f.alarms, f.err
}
func TestNativeDeviceSummaryUnknownAndOwnerIntegrity(t *testing.T) {
	now := time.Now().UTC()
	n := int64(1)
	q := nativeDeviceListQuery{values: url.Values{}}
	makeItems := func() []NativeDevice {
		return []NativeDevice{{ID: "native-a", RuntimeRef: &RuntimeRef{Namespace: "community", ExternalID: "runtime-a"}}}
	}
	f := summaryFixture{runtime: []communityclient.RuntimeSummary{{DeviceID: "runtime-a", Connectivity: "online", ObservedAt: &now, LastTelemetryAt: &now, Available: true}}, alarms: []communityclient.AlarmSummary{{DeviceID: "native-a", State: "active", ActiveCount: &n, ObservedAt: &now, Available: true}}}
	items := makeItems()
	if err := attachDeviceSummariesWithReader(context.Background(), items, q, f, "http://owner"); err != nil {
		t.Fatal(err)
	}
	if items[0].RuntimeSummary.Connectivity != "online" || items[0].AlarmSummary.State != "active" {
		t.Fatalf("summary=%+v", items[0])
	}
	for _, bad := range []summaryFixture{{err: errors.New("unavailable")}, {runtime: []communityclient.RuntimeSummary{{DeviceID: "foreign", Connectivity: "offline", ObservedAt: &now, Available: true}}}} {
		items = makeItems()
		if err := attachDeviceSummariesWithReader(context.Background(), items, q, bad, "http://owner"); err != nil {
			t.Fatal(err)
		}
		if items[0].RuntimeSummary.Connectivity != "unknown" || items[0].RuntimeSummary.Available || items[0].AlarmSummary.ActiveCount != nil {
			t.Fatal("owner failure invented known state")
		}
		qf := nativeDeviceListQuery{values: url.Values{"connectivity": {"offline"}}}
		if err := attachDeviceSummariesWithReader(context.Background(), makeItems(), qf, bad, "http://owner"); err == nil {
			t.Fatal("filter silently accepted failed owner")
		}
	}
}
