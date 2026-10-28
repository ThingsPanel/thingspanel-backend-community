package notification

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type coreStoreSpy struct {
	replay         bool
	replayError    error
	accepted       int
	sourceAccepted int
	request        contract.NotificationSubmit
	plan           SourceAcceptancePlan
	tenant         string
	digest         string
}

func (s *coreStoreSpy) FindSubmissionReplay(_ context.Context, tenant, _, digest string) (contract.AcceptedData, bool, error) {
	s.tenant, s.digest = tenant, digest
	return contract.AcceptedData{Accepted: true, NotificationID: "original", DeliveryIDs: []string{"original-delivery"}, IntakeStatus: "ready"}, s.replay, s.replayError
}
func (s *coreStoreSpy) FindSourceReplay(ctx context.Context, tenant, key, digest, _, _, _ string) (contract.AcceptedData, bool, error) {
	return s.FindSubmissionReplay(ctx, tenant, key, digest)
}
func (s *coreStoreSpy) AcceptSubmission(_ context.Context, tenant, _, digest string, request contract.NotificationSubmit) (contract.AcceptedData, error) {
	s.accepted++
	s.tenant, s.digest, s.request = tenant, digest, request
	return contract.AcceptedData{Accepted: true, NotificationID: "new", IntakeStatus: "ready"}, nil
}
func (s *coreStoreSpy) AcceptSourcePlan(_ context.Context, tenant, _, digest string, plan SourceAcceptancePlan) (contract.AcceptedData, error) {
	s.sourceAccepted++
	s.tenant, s.digest, s.plan = tenant, digest, plan
	return contract.AcceptedData{Accepted: true, NotificationID: "blocked", DeliveryIDs: []string{}, IntakeStatus: "blocked", BlockedReason: plan.BlockedReason}, nil
}
func (s *coreStoreSpy) GetNotification(context.Context, string, string) (contract.NotificationView, error) {
	return contract.NotificationView{}, errors.New("unexpected query")
}

type corePlannerFunc func(context.Context, string, contract.SourceEventV1) (SourceAcceptancePlan, error)

func (f corePlannerFunc) PlanSourceEvent(ctx context.Context, tenant string, event contract.SourceEventV1) (SourceAcceptancePlan, error) {
	return f(ctx, tenant, event)
}

func coreTestRequest() contract.NotificationSubmit {
	text := "temperature high"
	return contract.NotificationSubmit{Source: contract.SourceRef{Type: "alarm", ID: "event-1"}, ExpiresAt: "2030-01-02T00:00:00Z", Deliveries: []contract.SubmitDelivery{{InstanceID: "instance-1", Recipient: contract.Recipient{Kind: "phone", Address: "+8613800000000"}, Content: contract.Content{Kind: "text", Text: &text}}}}
}
func coreTestPrincipal(scopes ...string) UserPrincipal {
	return UserPrincipal{UserID: "user-1", TenantID: "tenant-1", Scopes: scopes}
}
func coreTestTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func TestCoreReplayPrecedesExpiryAndCurrentConfiguration(t *testing.T) {
	store := &coreStoreSpy{replay: true}
	validations := 0
	core := &Core{Store: store, Now: func() time.Time { return coreTestTime("2031-01-01T00:00:00Z") }, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error {
		validations++
		return errors.New("instance disabled")
	}}
	result, err := core.Submit(context.Background(), coreTestPrincipal("notification:send"), "stable-key-1", coreTestRequest())
	if err != nil || result.NotificationID != "original" || validations != 0 || store.accepted != 0 {
		t.Fatalf("replay lost original: result=%+v err=%v validations=%d accepts=%d", result, err, validations, store.accepted)
	}
	_, err = core.Submit(context.Background(), coreTestPrincipal("notification:read"), "stable-key-1", coreTestRequest())
	var failure *CoreFailure
	if !errors.As(err, &failure) || failure.Status != 403 {
		t.Fatalf("replay bypassed authorization: %v", err)
	}
}

func TestCoreValidatesEveryTargetBeforeAtomicAcceptance(t *testing.T) {
	store := &coreStoreSpy{}
	request := coreTestRequest()
	second := request.Deliveries[0]
	second.InstanceID = "instance-2"
	request.Deliveries = append(request.Deliveries, request.Deliveries[0], second)
	validations := 0
	core := &Core{Store: store, Now: func() time.Time { return coreTestTime("2030-01-01T00:00:00Z") }, ValidateTarget: func(_ context.Context, tenant string, delivery contract.SubmitDelivery) error {
		validations++
		if tenant != "tenant-1" {
			t.Fatal("tenant not propagated")
		}
		if delivery.InstanceID == "instance-2" {
			return errors.New("cross-tenant instance")
		}
		return nil
	}}
	if _, err := core.Submit(context.Background(), coreTestPrincipal("notification:send"), "stable-key-1", request); err == nil || store.accepted != 0 || validations != 2 {
		t.Fatalf("partially accepted invalid target: err=%v count=%d validations=%d", err, store.accepted, validations)
	}
	core.ValidateTarget = func(context.Context, string, contract.SubmitDelivery) error { return nil }
	if _, err := core.Submit(context.Background(), coreTestPrincipal("notification:send"), "stable-key-1", request); err != nil || store.accepted != 1 || len(store.request.Deliveries) != 2 {
		t.Fatalf("exact duplicate target not merged: %v accepts=%d targets=%d", err, store.accepted, len(store.request.Deliveries))
	}
}

func TestCoreReplayLookupFailureNeverFallsThrough(t *testing.T) {
	store := &coreStoreSpy{replayError: errors.New("database unavailable")}
	core := &Core{Store: store, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error {
		t.Fatal("validation after failed replay lookup")
		return nil
	}}
	if _, err := core.Submit(context.Background(), coreTestPrincipal("notification:send"), "stable-key-1", coreTestRequest()); err == nil || store.accepted != 0 {
		t.Fatalf("lookup failure accepted: %v", err)
	}
}

func TestCoreTestSendNeedsBothScopesAndUsesDurableSubmission(t *testing.T) {
	store := &coreStoreSpy{}
	core := &Core{Store: store, Now: func() time.Time { return coreTestTime("2030-01-01T00:00:00Z") }, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error { return nil }}
	intent := coreTestRequest()
	test := contract.TestSendRequest{ExpiresAt: intent.ExpiresAt, Recipient: intent.Deliveries[0].Recipient, Content: intent.Deliveries[0].Content}
	for _, scopes := range [][]string{{"notification:manage"}, {"notification:send_test"}, {"notification:send"}} {
		if _, err := core.TestSend(context.Background(), coreTestPrincipal(scopes...), "instance-1", "stable-test-key", test); err == nil {
			t.Fatalf("scope combination accepted: %v", scopes)
		}
	}
	if _, err := core.TestSend(context.Background(), coreTestPrincipal("notification:manage", "notification:send_test"), "instance-1", "stable-test-key", test); err != nil || store.accepted != 1 || store.request.Source.Type != "test" || store.request.Source.ID != "instance-1" {
		t.Fatalf("test did not use durable core: %v %+v", err, store.request)
	}
}

func TestCoreSourceScopeAndBlockedReplay(t *testing.T) {
	store := &coreStoreSpy{}
	event := contract.SourceEventV1{SchemaVersion: "1.0", SourceDeploymentID: "deployment-1", SourceEventID: "event-1", ActionID: "action-1", TenantID: "tenant-1", NotificationGroupID: "group-missing", GroupRevision: 1, OccurredAt: "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-02T00:00:00Z", Payload: contract.SourceEventPayload{Variables: map[string]json.RawMessage{}}}
	plans := 0
	core := &Core{Store: store, Planner: corePlannerFunc(func(_ context.Context, tenant string, received contract.SourceEventV1) (SourceAcceptancePlan, error) {
		plans++
		if tenant != "tenant-1" {
			t.Fatal("source tenant changed")
		}
		return SourceAcceptancePlan{Event: received, BlockedReason: "group_revision_unavailable"}, nil
	})}
	for _, principal := range []SourcePrincipal{{DeploymentID: "other", TenantIDs: []string{"tenant-1"}}, {DeploymentID: "deployment-1", TenantIDs: []string{"other"}}} {
		if _, err := core.ReceiveSourceEvent(context.Background(), principal, "stable-source-key", event); err == nil || plans != 0 {
			t.Fatalf("scope bypass: %v plans=%d", err, plans)
		}
	}
	principal := SourcePrincipal{DeploymentID: "deployment-1", TenantIDs: []string{"tenant-1"}}
	result, err := core.ReceiveSourceEvent(context.Background(), principal, "stable-source-key", event)
	if err != nil || result.IntakeStatus != "blocked" || store.sourceAccepted != 1 || len(store.plan.Deliveries) != 0 {
		t.Fatalf("missing group not durably blocked: %+v %v", result, err)
	}
	store.replay = true
	if _, err := core.ReceiveSourceEvent(context.Background(), principal, "stable-source-key", event); err != nil || plans != 1 || store.sourceAccepted != 1 {
		t.Fatalf("blocked retry replanned: %v plans=%d accepts=%d", err, plans, store.sourceAccepted)
	}
}
