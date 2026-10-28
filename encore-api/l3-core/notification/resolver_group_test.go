package notification

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"encore.app/l3-core/notification/contract"
)

type resolverFixture struct {
	group       contract.GroupView
	groupErr    error
	projections []contract.SourceGroupProjection
}

func (f resolverFixture) ResolvePublished(context.Context, string, string, int64) (contract.GroupView, error) {
	return f.group, f.groupErr
}
func (f resolverFixture) ListGroupProjections(context.Context, string, string) ([]contract.SourceGroupProjection, error) {
	return f.projections, nil
}

type missingMember struct{}

func (missingMember) ResolveMember(context.Context, string, string, string) ([]contract.Recipient, error) {
	return nil, errors.New("contact-missing-private-data")
}

func sourceResolverFixture() (*SourceResolver, contract.SourceEventV1) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	event := contract.SourceEventV1{SchemaVersion: "1.0", TenantID: "tenant-fixture", SourceDeploymentID: "fixture", SourceEventID: "event", ActionID: "action", NotificationGroupID: "group", GroupRevision: 1, OccurredAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339), Payload: contract.SourceEventPayload{Subject: "alarm", Text: "fixture", Variables: map[string]json.RawMessage{"device.name": json.RawMessage(`"cold-room"`)}}}
	group := contract.GroupView{ID: "group", Revision: 1, Enabled: true, Bindings: []contract.GroupBinding{{BindingID: "binding", InstanceID: "instance", RecipientSource: json.RawMessage(`{"kind":"literal","recipient":{"kind":"email","address":"fixture@example.invalid"}}`), ContentBinding: json.RawMessage(`{"kind":"text","title":"Alarm","text":"Device {{device.name}}: {{subject}}"}`)}}}
	f := resolverFixture{group: group, projections: []contract.SourceGroupProjection{{TenantID: event.TenantID, SourceDeploymentID: event.SourceDeploymentID, NotificationGroupID: group.ID, GroupRevision: 1}}}
	return &SourceResolver{Groups: f, Projections: f, Members: missingMember{}, Now: func() time.Time { return now }, ValidateTarget: func(context.Context, string, contract.SubmitDelivery) error { return nil }}, event
}

func TestSourceResolutionIsAtomicAndKeepsDistinctIntents(t *testing.T) {
	r, event := sourceResolverFixture()
	f := r.Groups.(resolverFixture)
	duplicate := f.group.Bindings[0]
	duplicate.BindingID = "duplicate"
	distinct := f.group.Bindings[0]
	distinct.BindingID = "different"
	distinct.ContentBinding = json.RawMessage(`{"kind":"text","title":"Alarm","text":"Different message"}`)
	f.group.Bindings = append(f.group.Bindings, duplicate, distinct)
	r.Groups = f
	plan, err := r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "" || len(plan.Deliveries) != 2 {
		t.Fatalf("bad all-target plan: %v %+v", err, plan)
	}
	if *plan.Deliveries[0].Content.Text != "Device cold-room: alarm" {
		t.Fatal("variable resolution changed content")
	}
	member := contract.GroupBinding{BindingID: "missing", InstanceID: "instance", RecipientSource: json.RawMessage(`{"kind":"member","userId":"member-fixture","contactField":"email"}`), ContentBinding: duplicate.ContentBinding}
	f.group.Bindings = append(f.group.Bindings, member)
	r.Groups = f
	plan, err = r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "member_contact_unavailable" || len(plan.Deliveries) != 0 {
		t.Fatal("missing last member allowed partial fanout")
	}
}

func TestSourceResolutionDistinguishesUnavailableStorageAndMissingRevision(t *testing.T) {
	r, event := sourceResolverFixture()
	f := r.Groups.(resolverFixture)
	f.groupErr = storeErr(StoreErrorNotFound)
	r.Groups = f
	plan, err := r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "group_revision_unavailable" {
		t.Fatal("legal missing revision was not preserved as blocked")
	}
	f.groupErr = storeErr(StoreErrorUnavailable)
	r.Groups = f
	if _, err := r.PlanSourceEvent(context.Background(), event.TenantID, event); err == nil {
		t.Fatal("DB outage acknowledged as blocked business state")
	}
	r, event = sourceResolverFixture()
	event.ExpiresAt = "2026-10-08T00:00:00Z"
	plan, err = r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "blocked_expired" || len(plan.Deliveries) != 0 {
		t.Fatal("expired event has send targets")
	}
}

func TestSourceProjectionAndAppNeverSilentlyRoute(t *testing.T) {
	r, event := sourceResolverFixture()
	f := r.Projections.(resolverFixture)
	f.projections[0].SourceDeploymentID = "other-deployment"
	r.Projections = f
	plan, err := r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "source_projection_unavailable" || len(plan.Deliveries) != 0 {
		t.Fatal("wrong deployment projection used")
	}
	r, event = sourceResolverFixture()
	f = r.Groups.(resolverFixture)
	f.group.Bindings[0].RecipientSource = json.RawMessage(`{"kind":"member","userId":"application-user","contactField":"applicationUserId"}`)
	r.Groups = f
	plan, err = r.PlanSourceEvent(context.Background(), event.TenantID, event)
	if err != nil || plan.BlockedReason != "legacy_app_not_migratable" || len(plan.Deliveries) != 0 {
		t.Fatal("application ID silently used as provider ID")
	}
}

func TestRestrictedVariablesRemainLiteralAndPreserveJSONValues(t *testing.T) {
	_, event := sourceResolverFixture()
	payload := event.Payload
	payload.Variables["device.name"] = json.RawMessage(`"{{never_evaluate_this}}"`)
	text, err := renderNotificationText("Device {{device.name}}", payload)
	if err != nil || text != "Device {{never_evaluate_this}}" {
		t.Fatal("variable was recursively evaluated")
	}
	for _, template := range []string{"{{missing}}", "{{eval(1)}}", "{{device.name[0]}}"} {
		if _, err := renderNotificationText(template, payload); err == nil {
			t.Fatal("unsupported expression accepted")
		}
	}
	params := map[string]string{"nullable": "device.null", "object": "$.variables.device.object"}
	payload.Variables["device.null"] = json.RawMessage(`null`)
	payload.Variables["device.object"] = json.RawMessage(`{"nested":null}`)
	id, locale := "template-fixture", "zh-CN"
	content, err := resolveBindingContent(contentBinding{Kind: "template", TemplateID: &id, Locale: &locale, ParamsMapping: &params}, payload)
	if err != nil || string(content.Template.Params["nullable"]) != "null" || string(content.Template.Params["object"]) != `{"nested":null}` {
		t.Fatal("template JSON values were lost")
	}
}
