package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.dev/beta/errs"
)

const builtInMQTTAccess = `{"mode":"direct","protocol":"mqtt","providerId":null,"parameters":{},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":30},"connectionDirection":"device_to_platform","serviceInstanceId":null}`

const nativeAccessTenant = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

func TestValidateNativeDeviceConfigurationAcceptsBuiltinMQTTWithoutProviderCatalog(t *testing.T) {
	for _, access := range []string{
		builtInMQTTAccess,
		strings.Replace(builtInMQTTAccess, `"providerId":null`, `"providerId":null,"providerVersion":null`, 1),
		strings.Replace(builtInMQTTAccess, `"providerId":null`, `"providerId":null,"schemaDigest":"`+strings.Repeat("a", 64)+`"`, 1),
	} {
		if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(access), json.RawMessage(`{}`)); err != nil {
			t.Fatalf("built-in MQTT rejected: %v; access=%s", err, access)
		}
	}
}

func TestValidateNativeDeviceConfigurationRequiresEmptyParametersWithoutSchema(t *testing.T) {
	if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "gateway", json.RawMessage(builtInMQTTAccess), json.RawMessage(`{"interval":30}`)); err == nil {
		t.Fatal("device parameters without a provider schema accepted")
	}
	access := strings.Replace(builtInMQTTAccess, `"parameters":{}`, `"parameters":{"interval":30}`, 1)
	if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(access), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("ordinary access parameters rejected by local shape validation: %v", err)
	}
	secretAccess := strings.Replace(builtInMQTTAccess, `"parameters":{}`, `"parameters":{"password":"sentinel"}`, 1)
	if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(secretAccess), json.RawMessage(`{}`)); err == nil || strings.Contains(err.Error(), "sentinel") {
		t.Fatalf("credential access parameter accepted or leaked: %v", err)
	}
}

func TestValidateNativeDeviceConfigurationFailsClosedForConnectors(t *testing.T) {
	connector := strings.Replace(builtInMQTTAccess, `"mode":"direct","protocol":"mqtt"`, `"mode":"protocol_connector","protocol":"modbus"`, 1)
	err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(connector), json.RawMessage(`{}`))
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) || apiErr.Code != errs.Unavailable {
		t.Fatalf("connector error = %#v, want unavailable", err)
	}
}

func TestRejectSecretParameterKeysUsesExactNamesAndSafeError(t *testing.T) {
	for _, raw := range []string{`{"password":"sentinel"}`, `{"nested":[{"private_key":"sentinel"}]}`, `{"client-secret":"sentinel"}`, `{"credentials":{"user":"x"}}`} {
		err := rejectSecretParameterKeys([]byte(raw), "deviceParameters")
		if err == nil || strings.Contains(err.Error(), "sentinel") {
			t.Fatalf("secret parameter %s error=%v", raw, err)
		}
	}
	if err := rejectSecretParameterKeys([]byte(`{"tokenBucket":10,"secretaryMode":"safe"}`), "deviceParameters"); err != nil {
		t.Fatalf("ordinary parameter name falsely classified as secret: %v", err)
	}
}

func TestValidateNativeDeviceConfigurationRejectsNoncanonicalTenantAndMalformedInput(t *testing.T) {
	if err := validateNativeDeviceConfiguration(context.Background(), strings.ToUpper(nativeAccessTenant), "standalone", json.RawMessage(builtInMQTTAccess), json.RawMessage(`{}`)); err == nil {
		t.Fatal("noncanonical tenant accepted")
	}
	if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(`{"mode":"direct","mode":"direct"}`), json.RawMessage(`{}`)); err == nil {
		t.Fatal("duplicate access property accepted")
	}
	if err := validateNativeDeviceConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(builtInMQTTAccess), json.RawMessage(`null`)); err == nil {
		t.Fatal("non-object deviceParameters accepted")
	}
}
