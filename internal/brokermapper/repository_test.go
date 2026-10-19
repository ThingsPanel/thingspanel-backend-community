package brokermapper

import "testing"

func TestSnapshotCacheIsBoundedAndRulesAreImmutable(t *testing.T) {
	r := NewRepository(nil)
	r.MaxSnapshots = 2
	r.MaxCacheBytes = 1 << 20
	first, err := Compile([]Rule{{ID: 1, Direction: "up", SourceTopic: "sensor/{device_number}/{message_id}", TargetTopic: "devices/event/{message_id}"}})
	if err != nil {
		t.Fatal(err)
	}
	copyRules := first.Rules()
	copyRules[0].SourceTopic = "forged/{device_number}/{message_id}"
	if first.Rules()[0].SourceTopic == copyRules[0].SourceTopic {
		t.Fatal("caller mutated immutable rules")
	}
	r.cacheSnapshot(first, 100)
	if got := r.cached(first.Version); got != first {
		t.Fatal("cache did not return snapshot")
	}
	for i := int64(2); i <= 3; i++ {
		s, err := Compile([]Rule{{ID: i, Direction: "up", SourceTopic: "sensor/{device_number}/{message_id}", TargetTopic: "devices/event/{message_id}"}})
		if err != nil {
			t.Fatal(err)
		}
		r.cacheSnapshot(s, 100)
	}
	if len(r.cache) != 2 || r.cacheBytes > r.MaxCacheBytes {
		t.Fatalf("cache exceeded bounds: count=%d bytes=%d", len(r.cache), r.cacheBytes)
	}
	if r.cached(first.Version) != nil {
		t.Fatal("least-recent snapshot was not evicted")
	}
}
