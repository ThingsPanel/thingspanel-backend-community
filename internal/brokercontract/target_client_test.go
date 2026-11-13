package brokercontract

import (
	"strings"
	"testing"
)

func TestTargetPublisherClientIDCanonicalAndBounded(t *testing.T) {
	id, err := TargetPublisherClientID("platform", "target-1")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParseTargetPublisherClientID("platform", id)
	if !ok || got != "target-1" {
		t.Fatal("roundtrip failed")
	}
	for _, bad := range []string{"", "#", "other/target", " target", "target\n", strings.Repeat("x", 256)} {
		if _, err := TargetPublisherClientID("platform", bad); err == nil {
			t.Fatalf("invalid target %q accepted", bad)
		}
	}
	for _, bad := range []string{id + "=", id + "!", "other" + id, id + "--target-v1--YQ"} {
		if _, ok := ParseTargetPublisherClientID("platform", bad); ok {
			t.Fatal("noncanonical client id accepted")
		}
	}
	if _, ok := ParseTargetPublisherClientID("other", id); ok {
		t.Fatal("different base accepted")
	}
}
