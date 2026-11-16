package service

import "testing"

func TestExpectedPayloadHashJSONBCompatibility(t *testing.T) {
	original := `{"method":"set","params":{"target":25}}`
	normalized := `{"method": "set", "params": {"target": 25}}`
	if !expectedPayloadMatches(normalized, expectedPayloadHash(original)) {
		t.Fatal("original compact encoding not verified")
	}
	if !expectedPayloadMatches(normalized, expectedPayloadHash(normalized)) {
		t.Fatal("persisted encoding not verified")
	}
	for _, altered := range []string{`{"method":"set","params":{"target":26}}`, `{"method":"set","params":{"target":25,"target":26}}`} {
		if expectedPayloadMatches(altered, expectedPayloadHash(original)) {
			t.Fatal("tampered payload accepted")
		}
	}
	if expectedPayloadMatches(`{"value":9007199254740993}`, expectedPayloadHash(`{"value":9007199254740992}`)) {
		t.Fatal("numeric precision lost during verification")
	}
	if expectedPayloadMatches(normalized, expectedPayloadHash(`{ "method" : "set", "params" : {"target":25} }`)) {
		t.Fatal("unprovable old arbitrary formatting accepted")
	}
}
