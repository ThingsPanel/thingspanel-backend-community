package main

import (
	"strings"
	"testing"
)

func TestLedgerDoesNotPromoteBrokerAckToAcceptance(t *testing.T) {
	input := `{"run_id":"r1","case":"c1","stage":"planned","status":"observed","evidence":"planned","recorded_at":"2026-10-19T00:00:00Z"}` + "\n" +
		`{"run_id":"r1","case":"c1","stage":"broker_ack","status":"observed","evidence":"PUBACK success","recorded_at":"2026-10-19T00:00:01Z"}`
	got, err := judgeLedger(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Verdict != "INCOMPLETE" || got[0].Stages["accepted"] != "missing" {
		t.Fatalf("broker acknowledgement was incorrectly promoted: %+v", got)
	}
}

func TestLedgerRejectsAcceptanceWithoutIngressEvidence(t *testing.T) {
	input := `{"run_id":"r1","case":"c1","stage":"planned","status":"observed","evidence":"planned","recorded_at":"2026-10-19T00:00:00Z"}` + "\n" +
		`{"run_id":"r1","case":"c1","stage":"accepted","status":"observed","evidence":"claimed","recorded_at":"2026-10-19T00:00:01Z"}`
	got, err := judgeLedger(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Verdict != "INVALID" {
		t.Fatalf("acceptance without ingress evidence was not rejected: %+v", got)
	}
}
