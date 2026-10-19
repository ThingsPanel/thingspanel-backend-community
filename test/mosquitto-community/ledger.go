package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"time"
)

var ledgerStages = []string{"planned", "sent", "broker_ack", "ingress_seen", "device_delivery", "accepted", "terminal", "sideeffects"}

type commandLedgerCase struct{ Case, Evidence string }

type ledgerEvent struct {
	RunID      string    `json:"run_id"`
	Case       string    `json:"case"`
	Stage      string    `json:"stage"`
	Status     string    `json:"status"`
	Evidence   string    `json:"evidence"`
	RecordedAt time.Time `json:"recorded_at"`
}

type ledgerAssertion struct {
	RunID   string            `json:"run_id"`
	Case    string            `json:"case"`
	Stages  map[string]string `json:"stages"`
	Verdict string            `json:"verdict"`
	Rules   []string          `json:"rules"`
}

func writeRunLedger(path, runID string, ingressCases, stateCases []string, commandCases []commandLedgerCase) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	write := func(c, stage, status, evidence string) error {
		b, err := json.Marshal(ledgerEvent{RunID: runID, Case: c, Stage: stage, Status: status, Evidence: evidence, RecordedAt: time.Now().UTC()})
		if err != nil {
			return err
		}
		_, err = f.Write(append(b, '\n'))
		return err
	}
	for _, c := range ingressCases {
		for _, row := range []struct{ stage, status, evidence string }{
			{"planned", "observed", "runner case declared before assertions"},
			{"sent", "observed", "MQTT client PUBLISH reached broker and returned without transport error"},
			{"broker_ack", "observed", "QoS 1 broker PUBACK reason success"},
			{"ingress_seen", "observed", "service MQTT subscriber received and validated ingress envelope bytes and trusted metadata"},
			{"accepted", "unobserved", "database-backed durable acceptor is not part of this fixture"},
			{"terminal", "unobserved", "no downstream terminal business result was observed"},
			{"sideeffects", "unobserved", "no business side effect was observed"},
		} {
			if err := write(c, row.stage, row.status, row.evidence); err != nil {
				return err
			}
		}
	}
	for _, c := range stateCases {
		for _, row := range []struct{ stage, status, evidence string }{
			{"planned", "observed", "runner state case declared before assertions"},
			{"sent", "not_applicable", "state event was emitted by the broker plugin callback"},
			{"broker_ack", "not_applicable", "no client-originated PUBLISH acknowledgement exists for broker-emitted state"},
			{"ingress_seen", "observed", "service state subscriber received and checked topic, principal, connection ID, sequence and online flag"},
			{"accepted", "unobserved", "application state owner/subscriber persistence is outside this fixture"},
			{"terminal", "unobserved", "no downstream terminal business result was observed"},
			{"sideeffects", "unobserved", "no business side effect was observed"},
		} {
			if err := write(c, row.stage, row.status, row.evidence); err != nil {
				return err
			}
		}
	}
	for _, c := range commandCases {
		for _, row := range []struct{ stage, status, evidence string }{
			{"planned", "observed", "runner command check declared"},
			{"sent", "observed", "service QoS1 PUBLISH reached the isolated broker"},
			{"broker_ack", "observed", "service received successful broker PUBACK; this does not assert delivery or acceptance"},
			{"ingress_seen", "not_applicable", "command delivery is a downlink path, not telemetry ingress"},
			{"device_delivery", "observed", c.Evidence},
			{"accepted", "unobserved", "no database-backed command acceptor receipt was observed"},
			{"terminal", "unobserved", "no downstream terminal command result was observed"},
			{"sideeffects", "unobserved", "no physical device side effect was observed"},
		} {
			if err := write(c.Case, row.stage, row.status, row.evidence); err != nil {
				return err
			}
		}
	}
	return f.Sync()
}

func judgeLedger(in io.Reader) ([]ledgerAssertion, error) {
	stages := map[string]bool{}
	for _, s := range ledgerStages {
		stages[s] = true
	}
	cases := map[string]map[string]ledgerEvent{}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var e ledgerEvent
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("invalid NDJSON row: %w", err)
		}
		if e.RunID == "" || e.Case == "" || !stages[e.Stage] || e.Evidence == "" || e.RecordedAt.IsZero() {
			return nil, errors.New("ledger row missing run, case, valid stage, evidence or record time")
		}
		if e.Status != "observed" && e.Status != "unobserved" && e.Status != "not_applicable" {
			return nil, fmt.Errorf("unsupported status %q", e.Status)
		}
		if cases[e.Case] == nil {
			cases[e.Case] = map[string]ledgerEvent{}
		}
		if _, exists := cases[e.Case][e.Stage]; exists {
			return nil, fmt.Errorf("duplicate stage %q for %q", e.Stage, e.Case)
		}
		cases[e.Case][e.Stage] = e
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(cases) == 0 {
		return nil, errors.New("empty ledger")
	}
	caseIDs := make([]string, 0, len(cases))
	for c := range cases {
		caseIDs = append(caseIDs, c)
	}
	sort.Strings(caseIDs)
	var out []ledgerAssertion
	for _, c := range caseIDs {
		rows := cases[c]
		var runID string
		state := make(map[string]string, len(ledgerStages))
		for _, stage := range ledgerStages {
			e, ok := rows[stage]
			if !ok {
				state[stage] = "missing"
				continue
			}
			if runID == "" {
				runID = e.RunID
			} else if runID != e.RunID {
				return nil, fmt.Errorf("mixed run IDs for case %q", c)
			}
			state[stage] = e.Status
		}
		verdict := "INCOMPLETE"
		if state["planned"] != "observed" {
			verdict = "INVALID"
		}
		// A transport acknowledgement is one layer of evidence and never proves acceptance.
		if state["accepted"] == "observed" && state["ingress_seen"] != "observed" {
			verdict = "INVALID"
		}
		out = append(out, ledgerAssertion{RunID: runID, Case: c, Stages: state, Verdict: verdict, Rules: []string{"broker_ack does not imply accepted", "accepted requires ingress_seen evidence", "unobserved stages remain incomplete"}})
	}
	return out, nil
}

func runLedgerCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: mqtt-community ledger RAW.ndjson ASSERTIONS.json")
	}
	in, err := os.Open(args[0])
	if err != nil {
		return err
	}
	defer in.Close()
	assertions, err := judgeLedger(in)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(assertions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(args[1], append(b, '\n'), 0o600)
}
