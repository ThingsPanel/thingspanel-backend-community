package brokercontract

import (
	"testing"
	"time"
)

func TestStateBoundToBrokerAndPrincipal(t *testing.T) {
	s := StateObservation{ContractVersion: "1", MappingVersion: "standard-v1", PolicyVersion: "policy-1", Principal: Principal{TenantID: "ta", DeviceID: "da", Kind: PrincipalDevice}, BrokerCell: "c", BrokerInstance: "i", NodeBootID: "boot", ConnectionID: "conn", RouteEpoch: 1, EventSeq: 2, ObservedAt: time.Now()}
	topic, body, e := EncodeState(s)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ParseState(topic, body, TrustedMetadata{BrokerCell: "c", BrokerInstance: "i"}); e != nil {
		t.Fatal(e)
	}
	if _, e = ParseState(topic, body, TrustedMetadata{BrokerCell: "other", BrokerInstance: "i"}); e == nil {
		t.Fatal("wrong cell allowed")
	}
	if _, e = ParseState(topic+"x", body, TrustedMetadata{BrokerCell: "c", BrokerInstance: "i"}); e == nil {
		t.Fatal("wrong principal key allowed")
	}
}
