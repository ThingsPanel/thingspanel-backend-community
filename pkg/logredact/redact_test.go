package logredact

import (
	"strings"
	"testing"
)

func TestTextMasksSensitiveFields(t *testing.T) {
	const tok = "fake-ha-token-123"
	cases := []string{
		`{"name":"a","voucher":"{\"url\":\"http://ha\",\"token\":\"` + tok + `\"}"}`,
		`{"voucher":{"url":"x","access_token":"` + tok + `"}}`,
		`{"a":[{"Password":"` + tok + `"}],"client_secret":"` + tok + `"}`,
		`{"name":"a","voucher":"{\"token\":\"` + tok, // truncated
		`voucher=` + tok + `&page=1`,
		`{"voucher":12345` + tok + `}`,
	}
	for _, in := range cases {
		out := Text(in)
		if strings.Contains(out, tok) {
			t.Errorf("leaked: in=%s out=%s", in, out)
		}
	}
	if got := Text(`{"name":"keep","page":2}`); got != `{"name":"keep","page":2}` {
		t.Errorf("non-sensitive altered: %s", got)
	}
}

func TestURLAndError(t *testing.T) {
	out := URL("http://h/api?voucher=%7B%22t%22%3A%22secretX%22%7D&page=1")
	if strings.Contains(out, "secretX") || !strings.Contains(out, "page=1") {
		t.Errorf("bad url: %s", out)
	}
	if e := Error(nil); e != nil {
		t.Error("nil")
	}
}
