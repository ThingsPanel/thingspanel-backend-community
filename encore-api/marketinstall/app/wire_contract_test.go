package app

import (
	"encoding/json"
	"testing"
)

// Fixed coordinator-provider-v1 vector shared by both owners. Keeping this
// independently specified wire value protects cross-module hash compatibility,
// JSON key normalization and integers larger than float64's exact range.
func TestInstallationWireHashVector(t *testing.T) {
	step := Step{ID: "native:create", Owner: "thingmodel", Action: "create", ResourceKey: "native", Payload: json.RawMessage(`{"model":{"name":"fixed"},"counter":9007199254740993}`)}
	hash, err := StepHash(step)
	if err != nil || hash != "e6e5505e56595b793204e411b708c5d6c33dc1ed04f406868370daf252f955cd" {
		t.Fatalf("wire hash=%s err=%v", hash, err)
	}
	if key := StableStepKey("operation-fixture", step); key != "1a33b35d4e99184621e154b7dacba070c2db2600933deee1bb7d35c14824b7da" {
		t.Fatalf("wire key=%s", key)
	}
}
