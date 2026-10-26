package brokerstate

import (
	"context"
	"testing"
)

func TestPostgresConnectingBindingAcceptsObservationButBlocksProjection(t *testing.T) {
	s := testStore(t)
	if err := s.db.Exec("UPDATE broker_transport_bindings SET phase='connecting' WHERE tenant_id=? AND device_id=?", "ta", "da").Error; err != nil {
		t.Fatal(err)
	}
	o := observation("connecting-connection", 1, true)
	d, err := s.Apply(context.Background(), o)
	if err != nil || d.Status != "online" || !d.Changed {
		t.Fatalf("connecting observation was not retained: %+v err=%v", d, err)
	}
	var count int64
	if err := s.db.Table("broker_connection_observations").Where("tenant_id=? AND device_id=?", "ta", "da").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("observation count=%d err=%v", count, err)
	}
	if _, err := s.Project(context.Background(), "ta", "da"); err != ErrFenced {
		t.Fatalf("connecting status projection err=%v, want ErrFenced", err)
	}
	var pending int64
	if err := s.db.Table("broker_state_projection_queue").Where("tenant_id=? AND device_id=?", "ta", "da").Count(&pending).Error; err != nil || pending != 1 {
		t.Fatalf("projection queue was lost while binding connecting: %d %v", pending, err)
	}
	if err := s.db.Exec("UPDATE broker_transport_bindings SET phase='active' WHERE tenant_id=? AND device_id=?", "ta", "da").Error; err != nil {
		t.Fatal(err)
	}
	projected, err := s.Project(context.Background(), "ta", "da")
	if err != nil || projected.Status != "online" || !projected.Changed {
		t.Fatalf("active binding did not project retained observation: %+v err=%v", projected, err)
	}
}
