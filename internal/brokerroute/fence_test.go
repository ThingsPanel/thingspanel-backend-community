package brokerroute

import (
	"math"
	"testing"
)

func TestSnapshotRequiresIndependentCoherentEpochs(t *testing.T) {
	good := Snapshot{TenantID: "t", TargetID: "child", TargetEpoch: 3, TransportID: "root", TransportEpoch: 9, Cell: "a"}
	if good.Validate() != nil {
		t.Fatal("independent target/root epochs rejected")
	}
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.TargetEpoch = 0 }, func(s *Snapshot) { s.TransportEpoch = 0 },
		func(s *Snapshot) { s.TargetEpoch = math.MaxUint64 }, func(s *Snapshot) { s.TransportID = s.TargetID },
		func(s *Snapshot) { s.TenantID = "" }, func(s *Snapshot) { s.Cell = "" },
	} {
		bad := good
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatalf("invalid snapshot accepted: %+v", bad)
		}
	}
	good.TransportID = good.TargetID
	good.TransportEpoch = good.TargetEpoch
	if good.Validate() != nil {
		t.Fatal("self transport rejected")
	}
}
