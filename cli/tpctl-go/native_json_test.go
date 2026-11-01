package main

import (
	"encoding/json"
	"testing"
)

func TestNativeJSONPreservesRevisionAndRejectsDuplicateKeys(t *testing.T) {
	p := writeTemp(t, `{"expectedVersion":9007199254740993}`)
	v, err := nativeJSONFile(p)
	if err != nil || v["expectedVersion"] != json.Number("9007199254740993") {
		t.Fatalf("value=%v err=%v", v, err)
	}
	p = writeTemp(t, `{"data":{"x":1,"x":2}}`)
	if _, err := nativeJSONFile(p); err == nil {
		t.Fatal("duplicate key accepted")
	}
}
