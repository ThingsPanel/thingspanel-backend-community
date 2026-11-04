// Package logredact masks credential-bearing fields before text reaches logs,
// operation-log rows or error messages. A field is sensitive when its name
// contains voucher, token, password or secret (case-insensitive), so
// access_token, client_secret, newPassword etc. are covered too.
package logredact

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Mask replaces every redacted value.
const Mask = "***"

var sensitiveWords = []string{"voucher", "token", "password", "secret"}

// IsSensitiveKey reports whether a field/parameter name must be masked.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, w := range sensitiveWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

const keyPattern = `[A-Za-z0-9_.\-]*(?i:voucher|token|password|secret)[A-Za-z0-9_.\-]*`

var (
	// "key": "string value" — also matches truncated strings (no closing quote).
	jsonStringRe = regexp.MustCompile(`("` + keyPattern + `"\s*:\s*)"(?:\\.|[^"\\])*("|$)`)
	// "key": {...} / [...] / scalar, used only when the text is not valid JSON.
	jsonOtherRe = regexp.MustCompile(`("` + keyPattern + `"\s*:\s*)(?:[^"\s,}\]][^,}\]]*)`)
	formRe      = regexp.MustCompile(`((?:^|[?&;\s])([^=&;\s"]+)=)[^&?;\s"]*`)
)

// Text masks sensitive fields in a JSON document, form/query encoded text or
// loose log text. Valid JSON is parsed and masked structurally (nested objects
// and arrays included); anything else, including truncated JSON, falls back to
// pattern masking so a cut-off body can never leak a value.
func Text(s string) string {
	if s == "" {
		return s
	}
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil && !dec.More() {
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			out, err := json.Marshal(maskValue(v))
			if err == nil {
				return string(out)
			}
		}
	}
	s = jsonStringRe.ReplaceAllString(s, `${1}"`+Mask+`"`)
	s = jsonOtherRe.ReplaceAllString(s, `${1}"`+Mask+`"`)
	s = formRe.ReplaceAllStringFunc(s, redactFormField)
	return s
}

func redactFormField(match string) string {
	parts := formRe.FindStringSubmatch(match)
	if len(parts) != 3 {
		return match
	}
	key, err := url.QueryUnescape(parts[2])
	if err != nil {
		// Do not let malformed percent-encoding become a way to hide a sensitive key.
		if strings.Contains(parts[2], "%") {
			return parts[1] + Mask
		}
		return match
	}
	if IsSensitiveKey(key) {
		return parts[1] + Mask
	}
	return match
}

func maskValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if IsSensitiveKey(k) {
				t[k] = Mask
			} else {
				t[k] = maskValue(val)
			}
		}
		return t
	case []interface{}:
		for i := range t {
			t[i] = maskValue(t[i])
		}
		return t
	}
	return v
}

// URL masks the values of sensitive query parameters in a URL string.
func URL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return Text(raw)
	}
	// Userinfo is sensitive even when a URL has no query string.
	u.User = nil
	if u.RawQuery == "" {
		return u.String()
	}
	q := u.Query()
	for k := range q {
		if IsSensitiveKey(k) {
			q.Set(k, Mask)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Error returns an error whose message has any URL and credential text masked.
// *url.Error (what net/http returns) is rebuilt around the masked URL.
func Error(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return &url.Error{Op: ue.Op, URL: URL(ue.URL), Err: errors.New(Text(ue.Err.Error()))}
	}
	return errors.New(Text(err.Error()))
}
