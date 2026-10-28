package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("../../../../specs/notification/fixtures", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestNotificationListIncludesBlockedWithoutTargets(t *testing.T) {
	var envelope Envelope[NotificationListData]
	if err := DecodeStrictJSON(fixture(t, "notification-list-blocked.json"), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Total != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].IntakeStatus != "blocked" || envelope.Data.Items[0].Deliveries == nil || len(envelope.Data.Items[0].Deliveries) != 0 {
		t.Fatal("blocked request disappeared from task list")
	}
}

func TestGroupUpdateAndReadFixtureFields(t *testing.T) {
	var update GroupUpdate
	if err := DecodeStrictJSON(fixture(t, "group-update.json"), &update); err != nil {
		t.Fatal(err)
	}
	if update.ExpectedVersion != 1 || update.Name != "Fixture group" || len(update.Bindings) != 1 {
		t.Fatal("group update lost inherited fields or expectedVersion")
	}
	var view GroupView
	if err := DecodeStrictJSON(fixture(t, "group-response.json"), &view); err != nil {
		t.Fatal(err)
	}
	if view.ID != "group-fixture" || view.Revision != 1 || view.Version != 1 || len(view.Bindings) != 1 {
		t.Fatal("group read lost resource fields")
	}
	if err := DecodeStrictJSON([]byte(`{"name":"group","enabled":false,"bindings":[],"expectedVersion":1,"id":"forbidden"}`), &update); err == nil {
		t.Fatal("group update accepted a read-only id")
	}
}

func TestPluginGrantFixturesUseExplicitTenantAndVersionedSafeView(t *testing.T) {
	var update PluginGrantUpdate
	if err := DecodeStrictJSON(fixture(t, "plugin-grant-update.json"), &update); err != nil {
		t.Fatal(err)
	}
	if update.PluginRegistrationID != "plugin-registration-fixture" || update.TenantID != "tenant-opaque-fixture" || !update.Enabled || update.ExpectedVersion != 0 {
		t.Fatalf("decoded grant update mismatch: %+v", update)
	}
	var view PluginGrantView
	if err := DecodeStrictJSON(fixture(t, "plugin-grant-view.json"), &view); err != nil {
		t.Fatal(err)
	}
	if view.PluginRegistrationID != update.PluginRegistrationID || view.TenantID != update.TenantID || !view.Enabled || view.Version != 1 || view.CreatedAt == "" || view.UpdatedAt == "" {
		t.Fatalf("decoded grant view mismatch: %+v", view)
	}
	var page Envelope[PluginGrantPage]
	if err := DecodeStrictJSON(fixture(t, "plugin-grant-list.json"), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data.Items) != 1 || page.Data.Items[0].Enabled || page.Data.Items[0].Version != 2 || page.Data.Total != 1 {
		t.Fatalf("disabled grant missing from global list fixture: %+v", page.Data)
	}
	for _, invalid := range []string{
		string(fixture(t, "plugin-grant-update-null-enabled.raw")),
		`{"pluginRegistrationId":"p","tenantId":"t","enabled":true,"expectedVersion":0,"authSecret":"must-not-cross-wire"}`,
		`{"pluginRegistrationId":"p","tenantId":"t","enabled":true,"enabled":false,"expectedVersion":0}`,
		`{"pluginRegistrationId":"p","tenantId":"t","enabled":true,"expectedVersion":0,"secret":"must-not-cross-wire"}`,
	} {
		var rejected PluginGrantUpdate
		if err := DecodeStrictJSON([]byte(invalid), &rejected); err == nil {
			t.Errorf("invalid grant update accepted: %s", invalid)
		}
	}
}

func TestBusinessSubmitFixtureRoundTripsWithCamelCase(t *testing.T) {
	data := fixture(t, "submit-request.json")
	var got NotificationSubmit
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal submit fixture: %v", err)
	}
	if got.Source.ID != "alarm-fixture-001" || len(got.Deliveries) != 1 || got.Deliveries[0].InstanceID != "sms-fixture-001" {
		t.Fatalf("decoded wrong business identifiers: %#v", got)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal submit DTO: %v", err)
	}
	var wantValue, gotValue any
	if err := json.Unmarshal(data, &wantValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wire, &gotValue); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(wantValue)
	gotJSON, _ := json.Marshal(gotValue)
	if !bytes.Equal(wantJSON, gotJSON) {
		t.Fatalf("wire mismatch\nwant %s\n got %s", wantJSON, gotJSON)
	}
}

func TestSourceEventFixtureUsesCamelCase(t *testing.T) {
	data := fixture(t, "source-event.json")
	var got SourceEventV1
	if err := DecodeStrictJSON(data, &got); err != nil {
		t.Fatalf("unmarshal source event: %v", err)
	}
	if got.SourceEventID != "alarm-fixture-001" || got.TenantID != "tenant-fixture" || got.GroupRevision != 1 {
		t.Fatalf("decoded wrong source event: %#v", got)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(wire, &value); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schemaVersion", "sourceDeploymentId", "sourceEventId", "tenantId", "groupRevision"} {
		if _, ok := value[key]; !ok {
			t.Errorf("missing camelCase key %q in %s", key, wire)
		}
	}
	var legacy struct {
		Device struct {
			Description json.RawMessage `json:"description"`
			Metadata    json.RawMessage `json:"metadata"`
		} `json:"device"`
	}
	if err := json.Unmarshal(got.Payload.LegacyAlertJSON["device"], &legacy.Device); err != nil {
		t.Fatal(err)
	}
	if string(legacy.Device.Description) != "null" {
		t.Fatalf("legacy alert nested null was not preserved: %#v", legacy.Device)
	}
	var samples []json.RawMessage
	if err := json.Unmarshal(got.Payload.LegacyAlertJSON["samples"], &samples); err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 || string(samples[0]) != "null" {
		t.Fatalf("legacy alert array null was not preserved: %s", got.Payload.LegacyAlertJSON["samples"])
	}
	var roundTrip SourceEventV1
	if err := DecodeStrictJSON(wire, &roundTrip); err != nil {
		t.Fatalf("strict decoder rejected source-event round trip: %v", err)
	}
	if string(roundTrip.Payload.Variables["device.extra"]) != `{"nullable":null,"labels":["cold",null]}` {
		t.Fatalf("dynamic variable nulls were not preserved: %s", roundTrip.Payload.Variables["device.extra"])
	}
	if string(roundTrip.Payload.LegacyAlertJSON["device"]) != `{"description":null,"metadata":{"owner":null}}` {
		t.Fatalf("legacy alert JSON nulls were not preserved: %s", roundTrip.Payload.LegacyAlertJSON["device"])
	}
}

func TestPluginV1KeepsSnakeCaseAndReceiptAckIsRaw(t *testing.T) {
	manifest := PluginManifestV1{
		APIVersion: "1.0", PluginID: "example.mock", PluginVersion: "0.1.0",
		IdentityFields: []string{"account_id", "region"},
		Channels:       []Channel{ChannelWebhook}, ContentModes: []string{"text"},
		ConfigSchema:    json.RawMessage(`{"type":"object"}`),
		RecipientSchema: json.RawMessage(`{"type":"object"}`),
		SecretFields:    []string{"token"},
		Capabilities:    PluginCapabilitiesV1{DeliveryReceipts: false},
	}
	wire, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"api_version", "plugin_id", "plugin_version", "identity_fields", "config_schema", "recipient_schema", "secret_fields"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing plugin snake_case field %q in %s", key, wire)
		}
	}
	if _, ok := fields["identityFields"]; ok {
		t.Fatalf("plugin manifest must retain snake_case identity_fields: %s", wire)
	}
	ack, err := json.Marshal(ReceiptAck{Accepted: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(ack) != `{"accepted":true}` {
		t.Fatalf("receipt ack must stay raw protocol JSON, got %s", ack)
	}
}

func TestPluginIdentityFieldsRemainOptionalAndSummaryUsesBusinessCase(t *testing.T) {
	var legacy PluginManifestV1
	if err := DecodeStrictJSON(fixture(t, "plugin-manifest-legacy.json"), &legacy); err != nil {
		t.Fatalf("legacy manifest without identity_fields must decode: %v", err)
	}
	if legacy.IdentityFields != nil {
		t.Fatalf("missing identity_fields must remain absent, got %#v", legacy.IdentityFields)
	}

	var current PluginManifestV1
	if err := DecodeStrictJSON(fixture(t, "plugin-manifest-identity-fields.json"), &current); err != nil {
		t.Fatalf("manifest identity_fields must decode: %v", err)
	}
	if len(current.IdentityFields) != 2 || current.IdentityFields[0] != "account_id" || current.IdentityFields[1] != "region" {
		t.Fatalf("unexpected identity_fields: %#v", current.IdentityFields)
	}
	var empty PluginManifestV1
	if err := DecodeStrictJSON([]byte(`{"api_version":"1.0","identity_fields":[]}`), &empty); err != nil {
		t.Fatalf("empty identity_fields must survive structural decode for schema rejection: %v", err)
	}
	if empty.IdentityFields == nil || len(empty.IdentityFields) != 0 {
		t.Fatalf("present empty identity_fields must be distinguishable from absence: %#v", empty.IdentityFields)
	}
	if err := DecodeStrictJSON([]byte(`{"api_version":"1.0","identity_fields":null}`), &empty); err == nil {
		t.Fatal("explicit null identity_fields must be rejected; omission is the legacy form")
	}

	manifestSummary := PluginManifestSummary{IdentityFields: []string{"account_id", "region"}}
	wire, err := json.Marshal(manifestSummary)
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]json.RawMessage
	if err := json.Unmarshal(wire, &summary); err != nil {
		t.Fatal(err)
	}
	if _, ok := summary["identityFields"]; !ok {
		t.Fatalf("business safe summary must use identityFields: %s", wire)
	}
	if _, ok := summary["identity_fields"]; ok {
		t.Fatalf("business safe summary must not use plugin wire snake_case: %s", wire)
	}
	legacySummary, err := json.Marshal(PluginManifestSummary{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacySummary), `"identityFields"`) {
		t.Fatalf("legacy summary must omit absent identityFields: %s", legacySummary)
	}
}

func TestConsumerWireDriftRejected(t *testing.T) {
	var submit NotificationSubmit
	if err := DecodeStrictJSON(fixture(t, "submit-request.json"), &submit); err != nil {
		t.Fatalf("business golden fixture must decode: %v", err)
	}
	var source SourceEventV1
	sourceFixture := fixture(t, "source-event.json")
	if err := DecodeStrictJSON(sourceFixture, &source); err != nil {
		t.Fatalf("source-event golden fixture must decode: %v", err)
	}
	var plugin PluginValidateRequestV1
	pluginFixture := fixture(t, "plugin-validate-request.json")
	if err := DecodeStrictJSON(pluginFixture, &plugin); err != nil {
		t.Fatalf("plugin snake_case golden fixture must decode: %v", err)
	}
	var ack ReceiptAck
	var receiptGolden struct {
		HTTPStatus int             `json:"httpStatus"`
		Body       json.RawMessage `json:"body"`
	}
	if err := json.Unmarshal(fixture(t, "receipt-ack.json"), &receiptGolden); err != nil {
		t.Fatal(err)
	}
	if err := DecodeStrictJSON(receiptGolden.Body, &ack); receiptGolden.HTTPStatus != 200 || err != nil || !ack.Accepted {
		t.Fatalf("raw receipt ACK golden fixture must decode without business envelope: ack=%#v err=%v", ack, err)
	}
	var acceptedGolden struct {
		HTTPStatus int                    `json:"httpStatus"`
		Body       Envelope[AcceptedData] `json:"body"`
	}
	if err := json.Unmarshal(fixture(t, "accepted-business-response.json"), &acceptedGolden); err != nil {
		t.Fatal(err)
	}
	if acceptedGolden.HTTPStatus != 202 || acceptedGolden.Body.Code != 200 || !acceptedGolden.Body.Data.Accepted {
		t.Fatalf("accepted business golden fixture lost HTTP/envelope distinction: %#v", acceptedGolden)
	}

	mutations := []struct {
		name string
		wire []byte
		dst  any
	}{
		{
			name: "plugin snake_case changed to camelCase",
			wire: bytes.Replace(pluginFixture, []byte(`"api_version"`), []byte(`"apiVersion"`), 1),
			dst:  &PluginValidateRequestV1{},
		},
		{
			name: "plugin extra field",
			wire: appendJSONField(pluginFixture, `"unexpected":true`),
			dst:  &PluginValidateRequestV1{},
		},
		{
			name: "business camelCase changed to snake_case",
			wire: bytes.Replace(sourceFixture, []byte(`"sourceDeploymentId"`), []byte(`"source_deployment_id"`), 1),
			dst:  &SourceEventV1{},
		},
		{
			name: "business extra field",
			wire: appendJSONField(fixture(t, "submit-request.json"), `"unexpected":true`),
			dst:  &NotificationSubmit{},
		},
	}
	for _, testCase := range mutations {
		if err := DecodeStrictJSON(testCase.wire, testCase.dst); err == nil {
			t.Errorf("consumer accepted wire drift %q: %s", testCase.name, testCase.wire)
		}
	}
}

func appendJSONField(object []byte, field string) []byte {
	trimmed := bytes.TrimSpace(object)
	if len(trimmed) == 0 || trimmed[len(trimmed)-1] != '}' {
		return append(append([]byte(nil), object...), []byte(field)...)
	}
	return append(append(bytes.TrimSuffix(trimmed, []byte("}")), []byte(",")...), append([]byte(field), '}')...)
}

func TestInstanceUpdateNegativeFixturesArePreserved(t *testing.T) {
	for _, name := range []string{
		"instance-update-explicit-null.raw",
		"instance-update-enabled-null.raw",
		"instance-update-enabled-case-mismatch.raw",
		"instance-update-version-case-mismatch.raw",
		"instance-update-duplicate-key.raw",
	} {
		if err := DecodeStrictJSON(fixture(t, name), &InstanceUpdate{}); err == nil {
			t.Errorf("strict decoder accepted negative fixture %s", name)
		}
	}
}

func TestStrictDecoderAllowsNullInsideDynamicTemplateParams(t *testing.T) {
	input := []byte(`{"recipient":{"kind":"phone","address":"+15550000000"},"content":{"kind":"template","template":{"id":"tpl-fixture","params":{"optional":null,"nested":{"value":null},"items":[null]}}},"expiresAt":"2030-01-01T00:00:00Z"}`)
	var got TestSendRequest
	if err := DecodeStrictJSON(input, &got); err != nil {
		t.Fatalf("template params schema allows arbitrary JSON null values: %v", err)
	}
	if string(got.Content.Template.Params["optional"]) != "null" || string(got.Content.Template.Params["nested"]) != `{"value":null}` {
		t.Fatalf("dynamic template nulls were not preserved: %#v", got.Content.Template.Params)
	}
}

func TestStrictDecoderKeepsPluginDynamicConfigNullCompatible(t *testing.T) {
	input := []byte(`{"api_version":"1.0","tenant_id":"tenant-fixture","instance_id":"instance-fixture","config_version":1,"config":{"optional":null,"nested":{"field":null}}}`)
	var got PluginValidateRequestV1
	if err := DecodeStrictJSON(input, &got); err != nil {
		t.Fatalf("plugin config schema permits arbitrary JSON values: %v", err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(got.Config, &config); err != nil {
		t.Fatal(err)
	}
	if string(config["optional"]) != "null" || string(config["nested"]) != `{"field":null}` {
		t.Fatalf("plugin config nulls were not preserved: %s", got.Config)
	}
}

func TestInstanceUpdateFixturesPreserveOmittedSetAndClear(t *testing.T) {
	var omitted, set, clear InstanceUpdate
	if err := DecodeStrictJSON(fixture(t, "instance-update-omitted.json"), &omitted); err != nil {
		t.Fatal(err)
	}
	if omitted.Name == nil || *omitted.Name != "Primary SMS" || omitted.Secrets != nil || omitted.Enabled != nil {
		t.Fatalf("omitted update fields must remain absent: %#v", omitted)
	}
	if err := DecodeStrictJSON(fixture(t, "instance-update-set-secret.json"), &set); err != nil {
		t.Fatal(err)
	}
	if set.Secrets == nil || set.Secrets.Set["accessToken"] != "FAKE_SECRET_SENTINEL_ROTATE" || len(set.Secrets.Clear) != 0 {
		t.Fatalf("set-secret fixture decoded incorrectly: %#v", set)
	}
	if err := DecodeStrictJSON(fixture(t, "instance-update-clear-secret.json"), &clear); err != nil {
		t.Fatal(err)
	}
	if clear.Secrets == nil || len(clear.Secrets.Clear) != 1 || clear.Secrets.Clear[0] != "accessToken" || len(clear.Secrets.Set) != 0 {
		t.Fatalf("clear-secret fixture decoded incorrectly: %#v", clear)
	}
}

func TestInstanceUpdateReplayFixtureExercisesDecisionOrder(t *testing.T) {
	var scenario struct {
		InitialVersion int64 `json:"initialVersion"`
		FirstResponse  struct {
			RequestID  string `json:"requestId"`
			ResourceID string `json:"resourceId"`
			Version    int64  `json:"version"`
		} `json:"firstResponse"`
		SameKeyReplay struct {
			RequestID         string `json:"requestId"`
			ResponseRequestID string `json:"responseRequestId"`
			ResourceID        string `json:"resourceId"`
			Version           int64  `json:"version"`
		} `json:"sameKeyReplay"`
		Rules struct {
			CheckKeyFirst bool `json:"sameKeySameDigestChecksIdempotencyBeforeExpectedVersion"`
			CurrentID     bool `json:"replayRequestIdIsCurrentHttpRequest"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(fixture(t, "instance-update-replay.json"), &scenario); err != nil {
		t.Fatal(err)
	}
	if !scenario.Rules.CheckKeyFirst || !scenario.Rules.CurrentID {
		t.Fatalf("fixture must require key-first replay and current requestId: %#v", scenario.Rules)
	}
	stored := &UpdateReplayRecord{RequestDigest: "digest-v1", ResourceID: scenario.FirstResponse.ResourceID, Version: scenario.FirstResponse.Version}
	if got := DecideUpdate(stored, "digest-v1", scenario.InitialVersion, scenario.FirstResponse.Version); got != UpdateReplay {
		t.Fatalf("same key and digest must replay before stale expectedVersion: got %v", got)
	}
	if scenario.SameKeyReplay.ResourceID != stored.ResourceID || scenario.SameKeyReplay.Version != stored.Version {
		t.Fatalf("replay must preserve business identity/version: %#v vs %#v", scenario.SameKeyReplay, stored)
	}
	if scenario.SameKeyReplay.RequestID != scenario.SameKeyReplay.ResponseRequestID || scenario.SameKeyReplay.ResponseRequestID == scenario.FirstResponse.RequestID || scenario.SameKeyReplay.ResponseRequestID == "" {
		t.Fatalf("replay HTTP response must carry the current requestId, fixture=%#v", scenario.SameKeyReplay)
	}
	if got := DecideUpdate(stored, "digest-v2", scenario.InitialVersion, scenario.FirstResponse.Version); got != UpdateIdempotencyConflict {
		t.Fatalf("same key with changed content must conflict before version check: got %v", got)
	}
	if got := DecideUpdate(nil, "digest-v1", scenario.InitialVersion, scenario.FirstResponse.Version); got != UpdateVersionConflict {
		t.Fatalf("new key with stale expectedVersion must conflict: got %v", got)
	}
}

func TestGroupProjectionFixtureUsesBoundSourceIdentity(t *testing.T) {
	var got SourceGroupProjectionRequest
	if err := json.Unmarshal(fixture(t, "source-group-projection-request.json"), &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceDeploymentID != "community-fixture-a" || got.TenantID != "tenant-fixture" || got.LegacyGroupID != "legacy-group-fixture" || got.NotificationGroupID != "group-fixture-001" || got.GroupRevision != 3 {
		t.Fatalf("projection request lost identity/version fields: %#v", got)
	}
	if err := DecodeStrictJSON(fixture(t, "group-create-source-mapping.raw"), &GroupCreate{}); err == nil {
		t.Fatal("ordinary user group create must reject caller-supplied sourceMapping")
	}
}

func TestGroupProjectionLifecycleFixturePreservesRevisionHistory(t *testing.T) {
	var scenario struct {
		SourceTuple struct {
			NotificationGroupID string `json:"notificationGroupId"`
		} `json:"sourceTuple"`
		InitialRevisions []int64 `json:"initialRevisions"`
		Cases            []struct {
			Name                string `json:"name"`
			GroupRevision       int64  `json:"groupRevision"`
			NotificationGroupID string `json:"notificationGroupId"`
			Decision            string `json:"decision"`
			LatestRevision      int64  `json:"latestRevision"`
		} `json:"cases"`
		HistoryLimit int `json:"historyLimit"`
		AtLimit      struct {
			ExistingRevisions      int    `json:"existingRevisions"`
			NewRevisionDecision    string `json:"newRevisionDecision"`
			ExistingRevisionReplay string `json:"existingRevisionReplayDecision"`
		} `json:"atLimit"`
		LegacySwitchRules struct {
			CAS         bool `json:"usesExpectedOldRevisionCompareAndSwap"`
			NoDowngrade bool `json:"rejectsDowngrade"`
			Transaction bool `json:"switchOccursInLegacyDatabaseTransaction"`
		} `json:"legacySwitchRules"`
	}
	if err := json.Unmarshal(fixture(t, "source-group-projection-lifecycle.json"), &scenario); err != nil {
		t.Fatal(err)
	}
	if scenario.HistoryLimit != MaxSourceGroupProjectionRevisions || len(scenario.InitialRevisions) != 1 {
		t.Fatalf("fixture and contract history limit disagree: limit=%d revisions=%v", scenario.HistoryLimit, scenario.InitialRevisions)
	}
	index := &SourceProjectionIndex{NotificationGroupID: scenario.SourceTuple.NotificationGroupID, Revisions: map[int64]SourceGroupProjection{}}
	for _, revision := range scenario.InitialRevisions {
		index.Revisions[revision] = SourceGroupProjection{NotificationGroupID: index.NotificationGroupID, GroupRevision: revision}
	}
	decisionNames := map[SourceProjectionDecision]string{
		SourceProjectionCreate: "create", SourceProjectionReplay: "replay",
		SourceProjectionConflict: "conflict", SourceProjectionHistoryLimit: "history_limit",
	}
	for _, testCase := range scenario.Cases {
		request := SourceGroupProjectionRequest{NotificationGroupID: testCase.NotificationGroupID, GroupRevision: testCase.GroupRevision}
		got := DecideSourceProjection(index, request)
		if decisionNames[got] != testCase.Decision {
			t.Errorf("%s: decision=%s, want %s", testCase.Name, decisionNames[got], testCase.Decision)
		}
		if got == SourceProjectionCreate {
			index.Revisions[testCase.GroupRevision] = SourceGroupProjection{NotificationGroupID: testCase.NotificationGroupID, GroupRevision: testCase.GroupRevision}
		}
		if latest := LatestSourceProjectionRevision(index); latest != testCase.LatestRevision {
			t.Errorf("%s: latest revision=%d, want %d", testCase.Name, latest, testCase.LatestRevision)
		}
	}
	full := &SourceProjectionIndex{NotificationGroupID: index.NotificationGroupID, Revisions: make(map[int64]SourceGroupProjection, scenario.AtLimit.ExistingRevisions)}
	for revision := 1; revision <= scenario.AtLimit.ExistingRevisions; revision++ {
		full.Revisions[int64(revision)] = SourceGroupProjection{NotificationGroupID: full.NotificationGroupID, GroupRevision: int64(revision)}
	}
	if got := decisionNames[DecideSourceProjection(full, SourceGroupProjectionRequest{NotificationGroupID: full.NotificationGroupID, GroupRevision: int64(scenario.AtLimit.ExistingRevisions + 1)})]; got != scenario.AtLimit.NewRevisionDecision {
		t.Errorf("new projection over history cap: got %s want %s", got, scenario.AtLimit.NewRevisionDecision)
	}
	if got := decisionNames[DecideSourceProjection(full, SourceGroupProjectionRequest{NotificationGroupID: full.NotificationGroupID, GroupRevision: 1})]; got != scenario.AtLimit.ExistingRevisionReplay {
		t.Errorf("old projection replay at history cap: got %s want %s", got, scenario.AtLimit.ExistingRevisionReplay)
	}
	if !scenario.LegacySwitchRules.CAS || !scenario.LegacySwitchRules.NoDowngrade || !scenario.LegacySwitchRules.Transaction {
		t.Errorf("legacy projection switch must use transactional compare-and-swap and reject downgrade: %#v", scenario.LegacySwitchRules)
	}
}

func TestDecodeStrictJSONRejectsTrailingAndExcessiveNesting(t *testing.T) {
	for name, input := range map[string]string{
		"trailing": `{ "expectedVersion": 2 } { "expectedVersion": 3 }`,
		"deep":     `{"expectedVersion":2,"secrets":{"set":{"token":{"a":{"b":{"c":true}}}}}}`,
	} {
		if name == "deep" {
			// The bounded depth limit is 64; construct a reliably over-limit body.
			input = `{"value":` + strings.Repeat("[", 66) + `0` + strings.Repeat("]", 66) + `}`
		}
		if err := DecodeStrictJSON([]byte(input), &map[string]any{}); err == nil {
			t.Errorf("strict decoder accepted %s input", name)
		}
	}
}

func TestDecodeStrictJSONRejectsInvalidUTF8(t *testing.T) {
	if err := DecodeStrictJSON([]byte{0xff}, &map[string]any{}); err == nil {
		t.Fatal("strict decoder accepted invalid UTF-8")
	}
}
