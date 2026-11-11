package devicemetadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"encore.app/l3-core/connectorcatalog"
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
	var inputErr *NativeDeviceInputError
	if !errors.As(err, &inputErr) {
		t.Fatalf("connector without immutable release error = %#v, want invalid input", err)
	}
}

func TestNativeConnectorUsesPinnedReleaseAndSeparateSchemas(t *testing.T) {
	provider := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	digest := strings.Repeat("a", 64)
	access := strings.Replace(builtInMQTTAccess, `"mode":"direct","protocol":"mqtt"`, `"mode":"protocol_connector","protocol":"modbus"`, 1)
	access = strings.Replace(access, `"providerId":null`, `"providerId":"`+provider+`","providerVersion":"1.0.0","schemaDigest":"`+digest+`"`, 1)
	access = strings.Replace(access, `"parameters":{}`, `"parameters":{"port":502}`, 1)
	schema := json.RawMessage(`{"type":"object","properties":{"port":{"type":"integer","minimum":1,"maximum":65535}},"required":["port"],"additionalProperties":false}`)
	deviceSchema := json.RawMessage(`{"type":"object","properties":{"unit":{"type":"integer","minimum":1,"maximum":247}},"required":["unit"],"additionalProperties":false}`)
	for _, tc := range []struct {
		name, access, parameters, change string
		wantError                        bool
	}{
		{"valid", access, `{"unit":1}`, "", false},
		{"template", access, `{}`, "", false},
		{"device required parameters", access, `{}`, "", true},
		{"access schema", strings.Replace(access, `"port":502`, `"port":0`, 1), `{"unit":1}`, "", true},
		{"device schema", access, `{"unit":248}`, "", true},
		{"different release", access, `{"unit":1}`, "release", true},
		{"wrong protocol", access, `{"unit":1}`, "protocol", true},
		{"wrong device kind", access, `{"unit":1}`, "kind", true},
		{"invalid schema", access, `{"unit":1}`, "schema", true},
		{"owner unavailable", access, `{"unit":1}`, "unavailable", true},
		{"secret rejected", access, `{"unit":1,"password":"sentinel"}`, "", true},
		{"instance unverified", strings.Replace(access, `"serviceInstanceId":null`, `"serviceInstanceId":"instance-1"`, 1), `{"unit":1}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := func(_ context.Context, tenant string, request *connectorcatalog.ResolveNativeProviderReleaseRequest) (*connectorcatalog.ResolveNativeProviderReleaseResponse, error) {
				if tenant != nativeAccessTenant || request.ProviderID != provider || request.Version != "1.0.0" || request.SchemaDigest != digest {
					t.Fatal("lost trusted tenant or pinned release")
				}
				r := &connectorcatalog.ResolveNativeProviderReleaseResponse{ProviderID: provider, Version: "1.0.0", SchemaDigest: digest, Protocols: []string{"modbus"}, DeviceKinds: []string{"standalone"}, ConfigurationSchema: schema, DeviceParameterSchema: deviceSchema}
				switch tc.change {
				case "release":
					r.Version = "2.0.0"
				case "protocol":
					r.Protocols = []string{"http"}
				case "kind":
					r.DeviceKinds = []string{"gateway"}
				case "schema":
					r.ConfigurationSchema = json.RawMessage(`{"type":"nonsense"}`)
				case "unavailable":
					return nil, &errs.Error{Code: errs.Unavailable, Message: "catalog unavailable"}
				}
				return r, nil
			}
			err := validateNativeAccessConfiguration(context.Background(), nativeAccessTenant, "standalone", json.RawMessage(tc.access), json.RawMessage(tc.parameters), resolver, tc.name != "template")
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v", err)
			}
			if err != nil && strings.Contains(err.Error(), "sentinel") {
				t.Fatal("secret leaked")
			}
		})
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
