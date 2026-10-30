package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.dev/beta/errs"
	"github.com/google/uuid"
)

func TestDecodeNativeTemplateCopy(t *testing.T) {
	if req, err := decodeNativeTemplateCopy([]byte(`{"name":"副本"}`)); err != nil || req.Name != "副本" {
		t.Fatalf("valid: %+v %v", req, err)
	}
	for _, bad := range []string{`{}`, `{"name":""}`, `{"name":" x"}`, `{"name":"x","extra":1}`, `{"name":"x"} {}`, `[]`, `not json`, `{"name":"` + strings.Repeat("a", 129) + `"}`} {
		if _, err := decodeNativeTemplateCopy([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestNativeTemplateCopyKeepsConfigurationAndIsIdempotent(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID := testTenantID()
	identity := nativeTemplateWriteTestIdentity(tenantID)
	sourceName := "source-" + uuid.NewString()
	body, _ := json.Marshal(map[string]any{"name": sourceName, "description": "origin", "brand": "acme", "tags": []string{"a", "b"}, "configuration": json.RawMessage(nativeTemplateMQTTConfiguration)})
	created, err := createNativeTemplate(ctx, identity, "template-copy-src-"+uuid.NewString(), body, "copy-src-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := nativeTemplateWriteTestResult(t, created)

	copyName := "copy-" + uuid.NewString()
	key := "template-copy-" + uuid.NewString()
	payload := []byte(`{"name":"` + copyName + `"}`)
	copied, err := copyNativeTemplate(ctx, identity, source.ID, key, payload, "copy-request", validateNativeDeviceConfiguration, nil)
	if err != nil {
		t.Fatal(err)
	}
	clone := nativeTemplateWriteTestResult(t, copied)
	if copied.Status != 201 || copied.Location != "/api/v2/device-access-templates/"+clone.ID || clone.ID == source.ID || clone.Name != copyName || clone.ResourceVersion != 1 {
		t.Fatalf("copy response: %+v %+v", copied, clone)
	}
	if clone.Description == nil || *clone.Description != "origin" || clone.Brand == nil || *clone.Brand != "acme" || len(clone.Tags) != 2 {
		t.Fatalf("descriptive fields not kept: %+v", clone)
	}
	var a, b map[string]any
	if json.Unmarshal(created.Body, &a) != nil || json.Unmarshal(copied.Body, &b) != nil {
		t.Fatal("unreadable bodies")
	}
	cfgA, _ := json.Marshal(a["data"].(map[string]any)["configuration"])
	cfgB, _ := json.Marshal(b["data"].(map[string]any)["configuration"])
	if string(cfgA) != string(cfgB) {
		t.Fatalf("configuration changed by copy:\n%s\n%s", cfgA, cfgB)
	}

	replay, err := copyNativeTemplate(ctx, identity, source.ID, key, payload, "other-request", validateNativeDeviceConfiguration, nil)
	if err != nil || !replay.Replayed || string(replay.Body) != string(copied.Body) {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if _, err := copyNativeTemplate(ctx, identity, source.ID, "template-copy-dup-"+uuid.NewString(), payload, "dup-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.AlreadyExists {
		t.Fatalf("duplicate name must conflict: %v", err)
	}
	if _, err := copyNativeTemplate(ctx, nativeTemplateWriteTestIdentity(testTenantID()), source.ID, "template-copy-x-"+uuid.NewString(), []byte(`{"name":"other-`+uuid.NewString()+`"}`), "cross-request", validateNativeDeviceConfiguration, nil); errs.Code(err) != errs.NotFound {
		t.Fatalf("cross-tenant: %v", err)
	}
	after, err := readNativeTemplate(ctx, nativeTemplateReadIdentity{tenantID: tenantID}, source.ID)
	if err != nil || after.ResourceVersion != source.ResourceVersion {
		t.Fatalf("source must be untouched: %+v %v", after, err)
	}
}
