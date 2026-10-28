package brokerstate

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestCommandContextIgnoresHistoricalTombstonesButBoundsFreshConnections(t *testing.T) {
	s := activityStore(t)
	configureActivity(t, s, "da", "ta", 60, 0)
	setActivity(t, s, "ta", "da", "da", "c", 1, time.Now().UTC())
	before, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) SELECT 'ta','da','c','i','boot','old-'||n,1,2,false,now()-interval '2 minutes',now()-interval '2 minutes' FROM generate_series(1,300) n`).Error; err != nil {
		t.Fatal(err)
	}
	after, err := lockedCommandContext(s, "ta", "da", "da")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("historical tombstones changed current fingerprint: %v", err)
	}
	if err := s.db.Exec(`INSERT INTO broker_connection_observations(tenant_id,device_id,broker_cell,broker_instance,node_boot_id,connection_id,route_epoch,event_seq,online,observed_at,received_at) SELECT 'ta','da','c','i','boot','fresh-'||n,1,1,true,now(),now() FROM generate_series(1,257) n`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := lockedCommandContext(s, "ta", "da", "da"); !errors.Is(err, ErrFenced) {
		t.Fatalf("fresh connection capacity: %v", err)
	}
}
