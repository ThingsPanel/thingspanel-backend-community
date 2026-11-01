package devicemetadata

import (
	"strings"
	"testing"
)

const validNativeDirectAccess = `{"access":{"mode":"direct","protocol":"mqtt","providerId":null,"providerVersion":null,"schemaDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parameters":{"threshold":9007199254740993},"credentialPolicy":{"kind":"none","autoRegister":false},"onlinePolicy":{"mode":"transport","timeoutSeconds":30},"connectionDirection":"device_to_platform","serviceInstanceId":null},"externalDeviceId":null}`

func TestDecodeNativeCreateDeviceDefaultsAndRawPrecision(t *testing.T) {
	got, err := DecodeNativeCreateDevice([]byte(`{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","deviceParameters":{"threshold":12.500000000000000001},"customFields":{"reading":9007199254740993},"initialGroupIds":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.InitialAdministrativeState != "enabled" || string(got.Labels) != `{}` || string(got.CustomFields) != `{"reading":9007199254740993}` || string(got.DeviceParameters) != `{"threshold":12.500000000000000001}` {
		t.Fatalf("defaults or raw precision changed: %#v", got)
	}
	if !got.InitialGroupIDs.Present || got.InitialGroupIDs.Value == nil || len(got.InitialGroupIDs.Value) != 0 {
		t.Fatalf("explicit empty initialGroupIds was not preserved: %#v", got.InitialGroupIDs)
	}
	withoutGroups, err := DecodeNativeCreateDevice([]byte(`{"name":"sensor","deviceNumber":"SN-2","deviceKind":"standalone"}`))
	if err != nil {
		t.Fatal(err)
	}
	if withoutGroups.InitialGroupIDs.Present {
		t.Fatal("omitted initialGroupIds must remain distinguishable from []")
	}
}

func TestDecodeNativeCreateDeviceAllowsOmittedNumberButRejectsExplicitInvalidValues(t *testing.T) {
	got, err := DecodeNativeCreateDevice([]byte(`{"name":"sensor","deviceKind":"standalone"}`))
	if err != nil {
		t.Fatalf("omitted deviceNumber should be defaulted by the create owner: %v", err)
	}
	if got.DeviceNumberPresent || got.DeviceNumber != "" {
		t.Fatalf("omitted deviceNumber presence was not preserved: %#v", got)
	}
	for _, input := range []string{
		`{"name":"sensor","deviceNumber":null,"deviceKind":"standalone"}`,
		`{"name":"sensor","deviceNumber":"","deviceKind":"standalone"}`,
		`{"name":"sensor","deviceNumber":" bad ","deviceKind":"standalone"}`,
	} {
		if _, err := DecodeNativeCreateDevice([]byte(input)); err == nil {
			t.Errorf("explicit invalid deviceNumber was accepted: %s", input)
		}
	}
	provided, err := DecodeNativeCreateDevice([]byte(`{"name":"sensor","deviceNumber":"SN-7","deviceKind":"standalone"}`))
	if err != nil || !provided.DeviceNumberPresent || provided.DeviceNumber != "SN-7" {
		t.Fatalf("provided deviceNumber was not preserved: input=%#v err=%v", provided, err)
	}
}

func TestDecodeNativeUpdateDeviceTriStateAndReplacement(t *testing.T) {
	got, err := DecodeNativeUpdateDevice([]byte(`{"description":null,"labels":{"zone":"west"},"location":null,"tags":[],"customFields":{"precision":1.000000000000000001},"productId":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Name.Present || !got.Description.Present || !got.Description.Null || !got.Location.Present || !got.Location.Null || !got.ProductID.Present || !got.ProductID.Null {
		t.Fatalf("omitted/null state not preserved: %#v", got)
	}
	if !got.Labels.Present || got.Labels.Null || string(got.Labels.Value) != `{"zone":"west"}` || !got.Tags.Present || got.Tags.Value == nil || len(got.Tags.Value) != 0 || !strings.Contains(string(got.CustomFields.Value), "1.000000000000000001") {
		t.Fatalf("replacement value not preserved: %#v", got)
	}
}

func TestDecodeNativeUpdateDescriptionBoundsAndPresence(t *testing.T) {
	for _, input := range []string{`{"description":"short"}`, `{"description":""}`, `{"description":null}`, `{}`} {
		if _, err := DecodeNativeUpdateDevice([]byte(input)); err != nil {
			t.Errorf("valid description update %s rejected: %v", input, err)
		}
	}
	tooLong := `{"description":"` + strings.Repeat("x", 2001) + `"}`
	if _, err := DecodeNativeUpdateDevice([]byte(tooLong)); err == nil {
		t.Fatal("description longer than 2000 characters must be rejected")
	}
}

func TestDecodeNativeLocationRejectsQuotedAndRoundedOutOfRangeNumbers(t *testing.T) {
	for _, coordinate := range []string{`"1"`, `"90"`, `90.000000000000000001`, `-90.000000000000000001`, `1e400`} {
		input := `{"location":{"latitude":` + coordinate + `,"longitude":0,"address":null}}`
		if _, err := DecodeNativeUpdateDevice([]byte(input)); err == nil {
			t.Errorf("invalid latitude accepted: %s", coordinate)
		}
	}
	for _, coordinate := range []string{`180.000000000000000001`, `-180.000000000000000001`} {
		input := `{"location":{"latitude":0,"longitude":` + coordinate + `,"address":null}}`
		if _, err := DecodeNativeUpdateDevice([]byte(input)); err == nil {
			t.Errorf("invalid longitude accepted: %s", coordinate)
		}
	}
	for _, coordinate := range []string{`90`, `-90`, `9e1`, `89.999999999999999999`} {
		input := `{"location":{"latitude":` + coordinate + `,"longitude":180,"address":null}}`
		if _, err := DecodeNativeUpdateDevice([]byte(input)); err != nil {
			t.Errorf("valid coordinate %s rejected: %v", coordinate, err)
		}
	}
}

func TestDecodeNativeDeviceRejectsInvalidLocalShape(t *testing.T) {
	valid := `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone"}`
	cases := []struct{ name, input string }{
		{"missing required name", `{"deviceNumber":"SN-1","deviceKind":"standalone"}`},
		{"unknown readonly", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","activationState":"active"}`},
		{"unknown model binding", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","modelBinding":{}}`},
		{"invalid kind", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"hub"}`},
		{"trimmed number", `{"name":"sensor","deviceNumber":" SN-1","deviceKind":"standalone"}`},
		{"C1 control number", `{"name":"sensor","deviceNumber":"SN-\u0085-1","deviceKind":"standalone"}`},
		{"unicode whitespace number", `{"name":"sensor","deviceNumber":"\u00a0SN-1","deviceKind":"standalone"}`},
		{"duplicate property", `{"name":"sensor","name":"other","deviceNumber":"SN-1","deviceKind":"standalone"}`},
		{"trailing value", valid + ` {}`},
		{"template direct conflict", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","templateId":"123e4567-e89b-12d3-a456-426614174000","directAccess":` + validNativeDirectAccess + `}`},
		{"gateway pair mismatch", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"subdevice","gatewayId":"123e4567-e89b-12d3-a456-426614174000"}`},
		{"subdevice address whitespace", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"subdevice","gatewayId":"123e4567-e89b-12d3-a456-426614174000","subdeviceAddress":" addr "}`},
		{"subdevice address C1 control", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"subdevice","gatewayId":"123e4567-e89b-12d3-a456-426614174000","subdeviceAddress":"addr\u0085"}`},
		{"bad labels", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","labels":{"bad key":"v"}}`},
		{"null label value", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","labels":{"valid":null}}`},
		{"nested custom field", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","customFields":{"x":{"y":1}}}`},
		{"duplicate tags", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","tags":["a","a"]}`},
		{"tags null", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","tags":null}`},
		{"invalid coordinate pair", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","location":{"latitude":1,"longitude":null,"address":null}}`},
		{"direct access null boolean", `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","directAccess":{"access":{"mode":"direct","protocol":"mqtt","providerId":null,"providerVersion":null,"schemaDigest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parameters":{},"credentialPolicy":{"kind":"none","autoRegister":null},"onlinePolicy":{"mode":"transport","timeoutSeconds":30},"connectionDirection":"device_to_platform","serviceInstanceId":null},"externalDeviceId":null}}`},
		{"unknown update property", `{"runtimeState":"active"}`},
		{"update nonnullable null", `{"name":null}`},
		{"update tags null", `{"tags":null}`},
		{"update invalid product uuid", `{"productId":"not-a-uuid"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if strings.HasPrefix(tc.name, "update") || tc.name == "unknown update property" {
				if _, err := DecodeNativeUpdateDevice([]byte(tc.input)); err == nil {
					t.Fatal("expected local shape rejection")
				}
				return
			}
			if _, err := DecodeNativeCreateDevice([]byte(tc.input)); err == nil {
				t.Fatal("expected local shape rejection")
			}
		})
	}
}

func TestDecodeNativeUpdateAllowsEmptyObjectAndCreateValidLocation(t *testing.T) {
	if _, err := DecodeNativeUpdateDevice([]byte(`{}`)); err != nil {
		t.Fatalf("empty partial update shape should be locally valid: %v", err)
	}
	input := `{"name":"sensor","deviceNumber":"SN-1","deviceKind":"standalone","location":{"latitude":null,"longitude":null,"address":"rack 1"},"gatewayId":null,"subdeviceAddress":null}`
	if _, err := DecodeNativeCreateDevice([]byte(input)); err != nil {
		t.Fatalf("valid location/null pair rejected: %v", err)
	}
	direct := `{"name":"sensor","deviceNumber":"SN-2","deviceKind":"standalone","directAccess":` + validNativeDirectAccess + `}`
	if got, err := DecodeNativeCreateDevice([]byte(direct)); err != nil || !got.DirectAccess.Present || got.DirectAccess.Null {
		t.Fatalf("valid direct access shape rejected: input=%#v err=%v", got, err)
	}
}
