package mqttadapter

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"project/internal/brokermapper"
)

func TestMapDownlinkUsesOneSnapshotWireTopicAndParams(t *testing.T) {
	snapshot, err := brokermapper.Compile([]brokermapper.Rule{{
		ID: 1, Direction: "down", SourceTopic: "wire/{device_number}/control",
		TargetTopic: "devices/command/{device_number}/{message_id}", DataIdentifier: "set-mode",
	}})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"method":"set-mode","params":{"mode":"eco"}}`)
	topic, out, err := mapDownlinkWithSnapshot(snapshot, "dev-a", "devices/command/dev-a/msg-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "wire/dev-a/control" || !bytes.Equal(out, []byte(`{"mode":"eco"}`)) {
		t.Fatalf("mapped output = %q %s", topic, out)
	}
}

func TestStandardDownlinkAllowsOwnedResponseTopics(t *testing.T) {
	snapshot := brokermapper.Standard()
	for _, topic := range []string{
		"devices/attributes/response/dev-a/msg-1",
		"devices/event/response/dev-a/msg-2",
		"gateway/attributes/response/gw-1/msg-3",
		"gateway/event/response/gw-1/msg-4",
	} {
		got, payload, err := mapDownlinkWithSnapshot(snapshot, topicDevice(topic), topic, []byte(`{"ok":true}`))
		if err != nil || got != topic || !bytes.Equal(payload, []byte(`{"ok":true}`)) {
			t.Fatalf("standard fallback %q: got=%q err=%v", topic, got, err)
		}
	}
}

func topicDevice(topic string) string {
	parts := bytes.Split([]byte(topic), []byte("/"))
	if len(parts) > 3 {
		return string(parts[3])
	}
	return ""
}

func TestCustomDownlinkDoesNotFallbackForUnmappedCanonicalTopic(t *testing.T) {
	snapshot, err := brokermapper.Compile([]brokermapper.Rule{{
		ID: 1, Direction: "down", SourceTopic: "wire/{device_number}/control",
		TargetTopic: "devices/command/{device_number}/{message_id}", DataIdentifier: "known-method",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := mapDownlinkWithSnapshot(snapshot, "dev-a", "devices/command/dev-a/msg-1", []byte(`{"method":"other","params":{}}`)); err == nil {
		t.Fatal("unsupported method unexpectedly produced a wire publication")
	}
}

func TestRememberedMappingHasHardCapacityAndExplicitInvalidation(t *testing.T) {
	a := &Adapter{mappingVersions: make(map[string]string, MaxRememberedMappings)}
	for i := 0; i < MaxRememberedMappings; i++ {
		a.mappingVersions[fmt.Sprintf("d-%d", i)] = "standard-v1"
	}
	a.mappingMu.Lock()
	err := a.rememberMappingLocked("new-device", "mapping-new")
	a.mappingMu.Unlock()
	if !errors.Is(err, ErrMappingCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	// Updating an existing device is allowed at capacity and never evicts another.
	a.mappingMu.Lock()
	err = a.rememberMappingLocked("d-0", "mapping-updated")
	a.mappingMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := a.rememberedMapping("d-1"); !ok || got != "standard-v1" {
		t.Fatal("capacity handling evicted an in-flight mapping")
	}
	a.InvalidateMapping("d-0")
	if _, ok := a.rememberedMapping("d-0"); ok {
		t.Fatal("invalidation retained device mapping")
	}
	a.mappingMu.Lock()
	err = a.rememberMappingLocked("new-device", "mapping-new")
	a.mappingMu.Unlock()
	if err != nil {
		t.Fatalf("capacity was not released by invalidation: %v", err)
	}
}
