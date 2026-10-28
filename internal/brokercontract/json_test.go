package brokercontract

import (
	"strings"
	"testing"
)

func TestUniqueJSONHandlesArraysEscapesAndDepthBound(t *testing.T) {
	for _, body := range []string{`null`, `[1,true,{"text":"你好","items":[{}]}]`, `{"a":{"x":1},"b":{"x":2}}`} {
		if err := ValidateUniqueJSON([]byte(body)); err != nil {
			t.Fatalf("valid JSON rejected: %v", err)
		}
	}
	for _, body := range []string{`{"a":1,"\u0061":2}`, `[1,{"id":"a","id":"b"}]`, `{} []`, strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		if ValidateUniqueJSON([]byte(body)) == nil {
			t.Fatal("ambiguous or excessive JSON accepted")
		}
	}
}
