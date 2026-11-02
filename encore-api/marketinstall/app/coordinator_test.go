package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type testLedger struct {
	work                    *Work
	dispatchErr, confirmErr error
	dispatched, confirmed   int
	heldState, heldCode     string
}

func (*testLedger) Create(context.Context, Operation) (Operation, error) {
	return Operation{}, ErrInvalid
}
func (*testLedger) Get(context.Context, string, string, string) (Operation, error) {
	return Operation{}, ErrNotFound
}
func (s *testLedger) Claim(context.Context, int) (*Work, error) { return s.work, nil }
func (s *testLedger) Dispatch(context.Context, Work) error      { s.dispatched++; return s.dispatchErr }
func (s *testLedger) Confirm(context.Context, Work, Receipt) error {
	s.confirmed++
	return s.confirmErr
}
func (s *testLedger) Hold(ctx context.Context, w Work, state, code string) error {
	s.heldState = state
	s.heldCode = code
	return nil
}
func (*testLedger) Resume(context.Context, string, string, string) error { return nil }

type testProvider struct {
	observation        Observation
	queryErr           error
	executeCalls       int
	executeObservation Observation
}

func (*testProvider) Prepare(context.Context, PrepareRequest) (Plan, error) {
	return Plan{}, ErrInvalid
}
func (p *testProvider) Query(context.Context, StepRequest) (Observation, error) {
	return p.observation, p.queryErr
}
func (p *testProvider) Execute(context.Context, StepRequest) (Observation, error) {
	p.executeCalls++
	return p.executeObservation, nil
}
func sampleWork() *Work {
	step := Step{ID: "model:create", Owner: "thingmodel", Action: "create", ResourceKey: "model", Payload: json.RawMessage(`{"model":{"value":9007199254740993}}`)}
	hash, _ := StepHash(step)
	return &Work{Operation: Operation{ID: "operation", TenantID: "tenant", SubjectID: "subject", Generation: 1}, Step: step, IdempotencyKey: StableStepKey("operation", step), PayloadHash: hash}
}
func TestCoordinatorDoesNotExecuteWithoutCommittedDispatch(t *testing.T) {
	injected := errors.New("local intent commit failed")
	ledger := &testLedger{work: sampleWork(), dispatchErr: injected}
	provider := &testProvider{observation: Observation{State: NotObserved}}
	coordinator := Coordinator{Store: ledger, Provider: provider}
	if _, err := coordinator.RunOne(context.Background()); !errors.Is(err, injected) {
		t.Fatalf("dispatch error lost: %v", err)
	}
	if provider.executeCalls != 0 || ledger.confirmed != 0 {
		t.Fatal("business action ran without durable dispatch intent")
	}
}
func TestCoordinatorUnknownAndUntrustedReceiptsPause(t *testing.T) {
	for _, state := range []string{Unknown, Expired, "", Confirmed, NotObserved} {
		t.Run(state, func(t *testing.T) {
			work := sampleWork()
			ledger := &testLedger{work: work}
			provider := &testProvider{observation: Observation{State: state}, executeObservation: Observation{State: NotObserved}}
			coordinator := Coordinator{Store: ledger, Provider: provider}
			if _, err := coordinator.RunOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if ledger.heldState != Paused || ledger.confirmed != 0 {
				t.Fatalf("unconfirmed outcome advanced: %#v", ledger)
			}
		})
	}
}
func TestCoordinatorOwnerReceiptCannotSubstituteIdentity(t *testing.T) {
	for _, field := range []string{"owner", "hash", "resource", "data", "null"} {
		t.Run(field, func(t *testing.T) {
			work := sampleWork()
			receipt := Receipt{Owner: work.Step.Owner, PayloadHash: work.PayloadHash, ResourceID: "original", Data: json.RawMessage(`{}`)}
			switch field {
			case "owner":
				receipt.Owner = "other"
			case "hash":
				receipt.PayloadHash = strings.Repeat("0", 64)
			case "resource":
				receipt.ResourceID = ""
			case "data":
				receipt.Data = json.RawMessage(`invalid`)
			case "null":
				receipt.Data = json.RawMessage(`null`)
			}
			ledger := &testLedger{work: work}
			provider := &testProvider{observation: Observation{State: Confirmed, Receipt: &receipt}}
			coordinator := Coordinator{Store: ledger, Provider: provider}
			if _, err := coordinator.RunOne(context.Background()); err != nil {
				t.Fatal(err)
			}
			if ledger.heldCode != "INVALID_OWNER_RECEIPT" || ledger.confirmed != 0 || provider.executeCalls != 0 {
				t.Fatal("untrusted owner result advanced")
			}
		})
	}
}
func TestCoordinatorConfirmCommitFailureRemainsError(t *testing.T) {
	work := sampleWork()
	injected := errors.New("receipt commit failed")
	ledger := &testLedger{work: work, confirmErr: injected}
	provider := &testProvider{observation: Observation{State: Confirmed, Receipt: &Receipt{Owner: work.Step.Owner, ResourceID: "original", PayloadHash: work.PayloadHash, Data: json.RawMessage(`{}`)}}}
	coordinator := Coordinator{Store: ledger, Provider: provider}
	if _, err := coordinator.RunOne(context.Background()); !errors.Is(err, injected) {
		t.Fatalf("commit error became successful completion: %v", err)
	}
	if provider.executeCalls != 0 {
		t.Fatal("known owner result reexecuted")
	}
}
func TestCoordinatorStepDigestAndPlanValidation(t *testing.T) {
	work := sampleWork()
	reordered := work.Step
	reordered.Payload = json.RawMessage(` {"model" : {"value":9007199254740993}}`)
	hash, err := StepHash(reordered)
	if err != nil || hash != work.PayloadHash {
		t.Fatal("whitespace changed step identity")
	}
	changed := work.Step
	changed.Payload = json.RawMessage(`{"model":{"value":9007199254740992}}`)
	hash, err = StepHash(changed)
	if err != nil || hash == work.PayloadHash {
		t.Fatal("large integer precision lost in step digest")
	}
	if StableStepKey("another-instance", work.Step) == work.IdempotencyKey {
		t.Fatal("distinct instances share a step identity")
	}
	plan := Plan{ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Steps: []Step{work.Step}}
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	plan.Steps[0].Action = "activate"
	if err := ValidatePlan(plan); !errors.Is(err, ErrInvalid) {
		t.Fatal("unimplemented activation admitted")
	}
}
