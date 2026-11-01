package devicemetadata

import (
	"net/url"
	"testing"
)

func TestResolveRuntimeDeviceID(t *testing.T) {
	const nativeID = "11111111-1111-4111-8111-111111111111"
	t.Setenv(runtimeNamespaceEnv, "")
	t.Setenv(runtimeIdentityModeEnv, "")

	if _, err := resolveRuntimeDeviceID(nil, nativeID); err == nil {
		t.Fatal("a device without a Community identity must be refused by default")
	}
	other := &RuntimeRef{ProviderID: "p", Namespace: "mqtt-broker", ExternalID: "x"}
	if _, err := resolveRuntimeDeviceID(other, nativeID); err == nil {
		t.Fatal("a runtimeRef in another namespace must not be used as the Community ID")
	}
	ref := &RuntimeRef{ProviderID: "p", Namespace: "community", ExternalID: "legacy-42"}
	if got, err := resolveRuntimeDeviceID(ref, nativeID); err != nil || got != "legacy-42" {
		t.Fatalf("got %q err %v", got, err)
	}
	t.Setenv(runtimeIdentityModeEnv, "identity")
	if got, err := resolveRuntimeDeviceID(nil, nativeID); err != nil || got != nativeID {
		t.Fatalf("identity mode: got %q err %v", got, err)
	}
	if got, _ := resolveRuntimeDeviceID(ref, nativeID); got != "legacy-42" {
		t.Fatal("runtimeRef must win over identity mode")
	}
	t.Setenv(runtimeNamespaceEnv, "tp-community")
	custom := &RuntimeRef{ProviderID: "p", Namespace: "tp-community", ExternalID: "legacy-7"}
	if got, _ := resolveRuntimeDeviceID(custom, nativeID); got != "legacy-7" {
		t.Fatal("configured namespace must be honoured")
	}
}

func TestDecodeNativeReadingQuery(t *testing.T) {
	parse := func(raw string) (nativeReadingQuery, error) {
		values, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		return decodeNativeReadingQuery(values)
	}
	if q, err := parse("latestOnly=true"); err != nil || q.kind != "telemetry" || !q.latestOnly {
		t.Fatalf("latest telemetry: %+v %v", q, err)
	}
	if q, err := parse("kind=attribute&latestOnly=true"); err != nil || q.kind != "attribute" {
		t.Fatalf("latest attribute: %+v %v", q, err)
	}
	if q, err := parse("kind=event&identifier=overheat&page=2&pageSize=10"); err != nil || q.kind != "event" || q.page != 2 {
		t.Fatalf("events: %+v %v", q, err)
	}
	q, err := parse("identifier=temp&from=2026-01-01T00:00:00Z&to=2026-01-01T06:00:00.500Z")
	if err != nil || q.from == nil || q.to == nil || q.to.Sub(*q.from).Milliseconds() != 6*3600*1000+500 {
		t.Fatalf("raw history: %+v %v", q, err)
	}
	if _, err := parse("identifier=temp&aggregation=raw&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z"); err != nil {
		t.Fatalf("exactly 24h raw history must be accepted: %v", err)
	}
	agg, err := parse("identifier=temp&aggregation=avg&window=1h&from=2026-01-01T00:00:00Z&to=2026-12-31T00:00:00Z")
	if err != nil || agg.aggregation != "avg" || agg.window != "1h" || agg.from == nil {
		t.Fatalf("aggregated history over a long range: %+v %v", agg, err)
	}
	for _, bad := range []string{
		"identifier=temp&from=2026-01-01T00:00:00Z",
		"identifier=temp&from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z",
		"identifier=temp&from=2026-01-01T00:00:00Z&to=2026-01-01T00:00:00Z",
		"identifier=temp&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:01Z",
		"from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&from=yesterday&to=2026-01-01T01:00:00Z",
		"kind=attribute&identifier=a&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&aggregation=avg&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&aggregation=raw&window=1h&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&aggregation=median&window=1h&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&aggregation=avg&window=45m&from=2026-01-01T00:00:00Z&to=2026-01-01T01:00:00Z",
		"identifier=temp&aggregation=avg&window=1h&from=2026-01-01T00:00:00Z&to=2027-01-03T00:00:00Z",
		"kind=attribute&identifier=a&aggregation=avg&window=1h&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z",
		"identifier=temp&aggregation=avg&window=1h&latestOnly=true&from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z",
	} {
		if _, err := parse(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	for _, bad := range []string{"", "kind=attribute", "kind=bogus", "latestOnly=true&aggregation=avg", "latestOnly=true&from=2026-01-01T00:00:00Z", "kind=event&latestOnly=maybe", "identifier=&latestOnly=true"} {
		if _, err := parse(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}
