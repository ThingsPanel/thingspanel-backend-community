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
	"strconv"
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

// sensitiveEncodedKey handles malformed/truncated bodies where a key cannot
// be decoded by the normal JSON or query parser. Ambiguous escapes are masked.
func sensitiveEncodedKey(key string) bool {
	for i := 0; i < 8; i++ {
		if IsSensitiveKey(key) {
			return true
		}
		next, err := url.QueryUnescape(key)
		if err != nil {
			return true
		}
		if strings.Contains(next, `\`) {
			decoded, err := strconv.Unquote(`"` + next + `"`)
			if err != nil {
				return true
			}
			next = decoded
		}
		if next == key {
			return false
		}
		key = next
	}
	return strings.ContainsAny(key, `%\`)
}

var (
	// Accept JSON, form, query and common error-log assignment syntax. Values
	// are consumed by a scanner so quotes and suffixes cannot leave fragments.
	assignmentRe = regexp.MustCompile(`(^|[?&;,\s{[(])(["']?)([^:=?&;,\s"'{}\[\]]+)(["']?\s*[:=]\s*)(["']?)`)
	userinfoRe   = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/?#\s"'<>]*@`)
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
	return redactLooseText(s)
}

func redactLooseText(s string) string {
	return redactAssignments(redactURLUserinfo(s))
}

func redactURLUserinfo(s string) string {
	return userinfoRe.ReplaceAllString(s, `${1}***@`)
}

func redactAssignments(s string) string {
	var out strings.Builder
	written, searchFrom := 0, 0
	for searchFrom <= len(s) {
		indices := assignmentRe.FindStringSubmatchIndex(s[searchFrom:])
		if indices == nil {
			break
		}
		start, end := searchFrom+indices[0], searchFrom+indices[1]
		group := func(n int) string {
			lo, hi := indices[2*n], indices[2*n+1]
			if lo < 0 || hi < 0 {
				return ""
			}
			return s[searchFrom+lo : searchFrom+hi]
		}
		sensitive := sensitiveEncodedKey(group(3))
		if !sensitive {
			searchFrom = end
			continue
		}

		out.WriteString(s[written:start])
		out.WriteString(s[start:end])
		valueStart := end
		if quote := group(5); quote != "" {
			if closeAt := quotedValueEnd(s, valueStart, quote[0]); closeAt >= 0 {
				out.WriteString(Mask)
				out.WriteByte(quote[0])
				// Treat any unseparated suffix as part of the credential too.
				fieldEnd := scalarValueEnd(s, closeAt+1)
				written, searchFrom = fieldEnd, fieldEnd
				continue
			}
			out.WriteString(Mask)
			return out.String()
		}
		if valueStart < len(s) && (s[valueStart] == '{' || s[valueStart] == '[') {
			if closeAt := balancedValueEnd(s, valueStart); closeAt >= 0 {
				out.WriteString(Mask)
				fieldEnd := scalarValueEnd(s, closeAt)
				written, searchFrom = fieldEnd, fieldEnd
				continue
			}
			out.WriteString(Mask)
			return out.String()
		}
		endValue := scalarValueEnd(s, valueStart)
		out.WriteString(Mask)
		written, searchFrom = endValue, endValue
	}
	out.WriteString(s[written:])
	return out.String()
}

func quotedValueEnd(s string, start int, quote byte) int {
	for i := start; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == quote {
			return i
		}
	}
	return -1
}

func balancedValueEnd(s string, start int) int {
	stack := make([]byte, 0, 4)
	var quote byte
	for i := start; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '"', '\'':
			quote = ch
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) == 0 || (ch == '}' && stack[len(stack)-1] != '{') || (ch == ']' && stack[len(stack)-1] != '[') {
				return -1
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func scalarValueEnd(s string, start int) int {
	for i := start; i < len(s); i++ {
		if s[i] == '&' {
			return i
		}
	}
	return len(s)
}

func maskValue(v interface{}) interface{} {
	switch t := v.(type) {
	case string:
		return redactLooseText(t)
	case map[string]interface{}:
		for k, val := range t {
			if sensitiveEncodedKey(k) {
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
		return Text(stripURLUserinfo(raw))
	}
	// Userinfo is sensitive even when a URL has no query string.
	u.User = nil
	if u.RawQuery == "" {
		return Text(u.String())
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		// Invalid escaping can hide a sensitive query key from ParseQuery.
		return Text(stripURLUserinfo(raw))
	}
	for k := range q {
		if sensitiveEncodedKey(k) {
			q.Set(k, Mask)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func stripURLUserinfo(raw string) string {
	schemeEnd := strings.Index(raw, "://")
	if schemeEnd <= 0 {
		return raw
	}
	authorityStart := schemeEnd + len("://")
	authorityEnd := len(raw)
	if offset := strings.IndexAny(raw[authorityStart:], "/?#"); offset >= 0 {
		authorityEnd = authorityStart + offset
	}
	if at := strings.LastIndex(raw[authorityStart:authorityEnd], "@"); at >= 0 {
		return raw[:authorityStart] + raw[authorityStart+at+1:]
	}
	return raw
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
