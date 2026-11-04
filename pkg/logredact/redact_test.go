package logredact

import (
	"errors"
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
		`voucher="` + tok + `"`,
		`voucher="` + tok + `"?suffix&next=ok`,
		`voucher='` + tok + `'`,
		`{"error":"GET https://operator:` + tok + `@host.test/path%ZZ"}`,
		`voucher=` + tok + `?suffix&next=ok`,
		`v%6fucher=` + tok + `&page=1`,
		`voucher%5Btoken%5D=` + tok + `&page=1`,
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
	if got := Text(`v%6fice=keep&page=1`); got != `v%6fice=keep&page=1` {
		t.Errorf("non-sensitive encoded form field altered: %s", got)
	}
	if got := Text(`page=1?next=ok`); got != `page=1?next=ok` {
		t.Errorf("ordinary query suffix altered: %s", got)
	}
	if got := Text(`page=1?token=` + tok + `&next=ok`); strings.Contains(got, tok) || !strings.Contains(got, `page=1?token=***&next=ok`) {
		t.Errorf("sensitive nested query was not redacted or normal parameters were lost: %s", got)
	}
	if got := Text(`voucher="` + tok + `"`); strings.Contains(got, tok) || !strings.Contains(got, `voucher="***"`) {
		t.Errorf("quoted voucher was not redacted: %s", got)
	}
	if got := Text(`voucher="` + tok + `"?suffix&next=ok`); strings.Contains(got, "suffix") || !strings.Contains(got, `&next=ok`) {
		t.Errorf("quoted voucher suffix was not masked or following parameter was lost: %s", got)
	}
}

func TestURLWithoutQueryStripsUserinfo(t *testing.T) {
	const secret = "fake-userinfo-password"
	out := URL("https://operator:" + secret + "@ha.example.test/api/devices")
	if strings.Contains(out, secret) || strings.Contains(out, "operator") {
		t.Fatalf("userinfo leaked from URL without query: %s", out)
	}
	if out != "https://ha.example.test/api/devices" {
		t.Fatalf("unexpected sanitized URL: %s", out)
	}
}

func TestURLParseFailureStillStripsUserinfo(t *testing.T) {
	const secret = "fake-url-password"
	out := URL("https://operator:" + secret + "@host.test/path%ZZ")
	if strings.Contains(out, secret) || strings.Contains(out, "operator") {
		t.Fatalf("userinfo leaked from malformed URL: %s", out)
	}
	if out != "https://host.test/path%ZZ" {
		t.Fatalf("unexpected sanitized malformed URL: %s", out)
	}
}

func TestURLMalformedQueryFailsClosed(t *testing.T) {
	const userinfoSecret = "fake-query-userinfo-password"
	const querySecret = "fake-malformed-query-secret"
	out := URL("https://operator:" + userinfoSecret + "@host.test/path?voucher%ZZ=" + querySecret + "&next=ok")
	for _, secret := range []string{userinfoSecret, "operator", querySecret} {
		if strings.Contains(out, secret) {
			t.Fatalf("credential %q leaked from malformed query: %s", secret, out)
		}
	}
}

func TestErrorMasksQuotedCredentialAndMalformedURL(t *testing.T) {
	const userinfoSecret = "fake-error-password"
	const voucherSecret = "fake-error-voucher"
	input := `GET "https://operator:` + userinfoSecret + `@host.test/path%ZZ" failed: voucher="` + voucherSecret + `"`
	got := Error(errors.New(input)).Error()
	for _, secret := range []string{userinfoSecret, "operator", voucherSecret} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential %q leaked from error: %s", secret, got)
		}
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

func TestEncodedAndTruncatedSensitiveKeys(t *testing.T) {
	for _, input := range []string{
		`%2576oucher=fake-secret`,
		`{"\u0076oucher":"fake-secret"`,
		`{"%2576oucher":"fake-secret"}`,
		`https://example.test/?%2576oucher=fake-secret`,
	} {
		if got := Text(input); strings.Contains(got, "fake-secret") {
			t.Errorf("Text leaked credential: %s", got)
		}
		if got := URL(input); strings.Contains(got, "fake-secret") {
			t.Errorf("URL leaked credential: %s", got)
		}
	}
}
