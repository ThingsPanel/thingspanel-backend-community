package brokermapper

import (
	"bytes"
	"testing"
)

func TestImmutableMappingAndScope(t *testing.T) {
	rules := []Rule{{ID: 1, Direction: "up", SourceTopic: "sensor/{device_number}/+", TargetTopic: "devices/event/{message_id}", Priority: 1}, {ID: 2, Direction: "down", SourceTopic: "sensor/{device_number}/control", TargetTopic: "devices/command/{device_number}/+", DataIdentifier: "set", Priority: 1}}
	s, e := Compile(rules)
	if e != nil {
		t.Fatal(e)
	}
	rules[0].SourceTopic = "forged"
	if got, e := s.Up("sensor/dev-a/id1", "dev-a"); e != nil || got != "devices/event/id1" {
		t.Fatalf("up=%s %v", got, e)
	}
	if _, e := s.Up("sensor/dev-b/id1", "dev-a"); e == nil {
		t.Fatal("cross device mapping")
	}
	body := []byte(`{"method":"set","params":{"value":5}}`)
	topic, out, e := s.Down("devices/command/dev-a/c1", "dev-a", body)
	if e != nil || topic != "sensor/dev-a/control" || !bytes.Equal(out, []byte(`{"value":5}`)) {
		t.Fatalf("down %s %s %v", topic, out, e)
	}
	if _, _, e := s.Down("devices/command/dev-b/c1", "dev-a", body); e == nil {
		t.Fatal("cross device downlink")
	}
}
func TestRejectUnexpressibleAndInternalTargets(t *testing.T) {
	for _, r := range []Rule{{ID: 1, Direction: "up", SourceTopic: "s/+", TargetTopic: "$tp/state/v1/a"}, {ID: 1, Direction: "up", SourceTopic: "s/#", TargetTopic: "devices/telemetry"}, {ID: 1, Direction: "up", SourceTopic: "s/+", TargetTopic: "devices/command/a/b"}, {ID: 1, Direction: "up", SourceTopic: "s/{unknown}", TargetTopic: "devices/telemetry"}} {
		if _, e := Compile([]Rule{r}); e == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}
func TestLiteralTopicCharactersAndVersion(t *testing.T) {
	r := Rule{ID: 1, Direction: "up", SourceTopic: "a.b/{device_number}", TargetTopic: "devices/telemetry"}
	s, _ := Compile([]Rule{r})
	if _, e := s.Up("axb/dev", "dev"); e == nil {
		t.Fatal("literal dot treated as regex")
	}
	s2, _ := Compile([]Rule{r})
	if s.Version != s2.Version {
		t.Fatal("unstable snapshot")
	}
	r.SourceTopic = "other/{device_number}"
	s3, _ := Compile([]Rule{r})
	if s.Version == s3.Version {
		t.Fatal("mapping mutation reused version")
	}
}
