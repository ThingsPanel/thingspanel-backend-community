package devicemetadata

import "testing"

const lifecycleReplacementID = "22222222-2222-2222-2222-222222222222"

func TestDecodeNativeRotateCredential(t *testing.T) {
	ok, err := decodeNativeRotateCredential([]byte(`{"replacementCredentialId":"` + lifecycleReplacementID + `","overlapSeconds":60,"expectedDeviceVersion":3}`))
	if err != nil || ok.ReplacementCredentialID != lifecycleReplacementID || ok.OverlapSeconds != 60 || ok.ExpectedDeviceVersion != 3 {
		t.Fatalf("%v %+v", err, ok)
	}
	bad := []string{
		`[]`, `{}`,
		`{"overlapSeconds":0,"expectedDeviceVersion":1}`,
		`{"replacementCredentialId":"x","overlapSeconds":0,"expectedDeviceVersion":1}`,
		`{"replacementCredentialId":"` + lifecycleReplacementID + `","overlapSeconds":3601,"expectedDeviceVersion":1}`,
		`{"replacementCredentialId":"` + lifecycleReplacementID + `","overlapSeconds":-1,"expectedDeviceVersion":1}`,
		`{"replacementCredentialId":"` + lifecycleReplacementID + `","overlapSeconds":0,"expectedDeviceVersion":0}`,
		`{"replacementCredentialId":"` + lifecycleReplacementID + `","overlapSeconds":0,"expectedDeviceVersion":1,"secret":"s"}`,
	}
	for _, body := range bad {
		if _, err := decodeNativeRotateCredential([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestDecodeNativeRevokeCredential(t *testing.T) {
	ok, err := decodeNativeRevokeCredential([]byte(`{"expectedDeviceVersion":2,"reason":"leaked"}`))
	if err != nil || ok.ExpectedDeviceVersion != 2 || ok.Reason != "leaked" {
		t.Fatalf("%v %+v", err, ok)
	}
	bad := []string{`{}`, `{"expectedDeviceVersion":2}`, `{"expectedDeviceVersion":2,"reason":""}`,
		`{"expectedDeviceVersion":0,"reason":"x"}`, `{"expectedDeviceVersion":2,"reason":"x","extra":1}`}
	for _, body := range bad {
		if _, err := decodeNativeRevokeCredential([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestDecodeNativeRenumberDevice(t *testing.T) {
	ok, err := decodeNativeRenumberDevice([]byte(`{"deviceNumber":"SN-002","expectedDeviceVersion":5,"reason":"relabel"}`))
	if err != nil || ok.DeviceNumber != "SN-002" || ok.ExpectedDeviceVersion != 5 || ok.Reason != "relabel" {
		t.Fatalf("%v %+v", err, ok)
	}
	bad := []string{`{}`, `{"deviceNumber":"SN-002","expectedDeviceVersion":5}`,
		`{"deviceNumber":"","expectedDeviceVersion":5,"reason":"x"}`,
		`{"deviceNumber":"SN-002","expectedDeviceVersion":0,"reason":"x"}`,
		`{"deviceNumber":"SN-002","expectedDeviceVersion":5,"reason":""}`,
		`{"deviceNumber":"SN-002","expectedDeviceVersion":5,"reason":"x","extra":true}`}
	for _, body := range bad {
		if _, err := decodeNativeRenumberDevice([]byte(body)); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
