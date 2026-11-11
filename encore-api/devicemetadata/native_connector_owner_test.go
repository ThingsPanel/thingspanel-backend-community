package devicemetadata

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.app/l3-core/connectorcatalog"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
	"github.com/google/uuid"
)

func TestNativeConnectorValidationCallsCatalogWithAuthenticatedTenant(t *testing.T) {
	ctx := context.Background()
	provider, tenant, user := uuid.NewString(), uuid.NewString(), uuid.NewString()
	defer et.OverrideAuthInfo("", nil)
	et.OverrideAuthInfo(auth.UID(user), &authn.Data{UserID: user, Roles: []string{"SYS_ADMIN"}})
	manifest, err := json.Marshal(map[string]any{
		"formatVersion": 1, "providerId": provider, "name": "Native validation fixture", "version": "1.0.0",
		"artifactDigest": "sha256:" + strings.Repeat("a", 64), "protocols": []string{"modbus"}, "deviceKinds": []string{"standalone"},
		"configurationSchema":   json.RawMessage(`{"type":"object","additionalProperties":false}`),
		"deviceParameterSchema": json.RawMessage(`{"type":"object","properties":{"unit":{"type":"integer","minimum":1,"maximum":247}},"required":["unit"],"additionalProperties":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	release, err := connectorcatalog.PublishNativeProviderRelease(ctx, &connectorcatalog.PublishNativeProviderReleaseRequest{Manifest: manifest})
	if err != nil {
		t.Fatal(err)
	}
	et.OverrideAuthInfo(auth.UID(user), &authn.Data{UserID: user, TenantID: tenant, Roles: []string{"TENANT_ADMIN"}})
	access := strings.Replace(builtInMQTTAccess, `"mode":"direct","protocol":"mqtt"`, `"mode":"protocol_connector","protocol":"modbus"`, 1)
	access = strings.Replace(access, `"providerId":null`, `"providerId":"`+provider+`","providerVersion":"1.0.0","schemaDigest":"`+release.SchemaDigest+`"`, 1)
	if err := validateNativeDeviceConfiguration(ctx, tenant, "standalone", json.RawMessage(access), json.RawMessage(`{"unit":1}`)); err != nil {
		t.Fatalf("device owner validation: %v", err)
	}
	if err := validateNativeTemplateAccessConfiguration(ctx, tenant, "standalone", json.RawMessage(access), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("template owner validation: %v", err)
	}
	if err := validateNativeDeviceConfiguration(ctx, tenant, "standalone", json.RawMessage(access), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing device parameters accepted")
	}
	if err := validateNativeDeviceConfiguration(ctx, uuid.NewString(), "standalone", json.RawMessage(access), json.RawMessage(`{"unit":1}`)); errs.Code(err) != errs.PermissionDenied {
		t.Fatalf("foreign validation tenant: %v", err)
	}
	et.OverrideAuthInfo("", nil)
	if err := validateNativeDeviceConfiguration(ctx, tenant, "standalone", json.RawMessage(access), json.RawMessage(`{"unit":1}`)); err == nil {
		t.Fatal("anonymous connector validation accepted")
	}
}
