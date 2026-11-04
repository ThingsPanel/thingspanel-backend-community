// Package logredact masks credential-bearing fields before text reaches logs,
// operation-log rows or error messages. A field is sensitive when its name
// contains voucher, token, password or secret (case-insensitive), so
// access_token, client_secret, newPassword etc. are covered too.
package logredact

import (
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Mask replaces every redacted value.
const Mask = "***"

const (
	maxTextBytes          = 1 << 20
	maxRedactionWorkBytes = 4 << 20
	maxRedactionFields    = 1 << 14
	maxNestedDepth        = 8
	maxNestedDecodeLayers = 4
)

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
	assignmentRe = regexp.MustCompile(`(^|[?&;,\s{[(])(["']?)([^:=?&;,\s"'{}]+)(["']?\s*[:=]\s*)(["']?)`)
	userinfoRe   = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/?#\s"'<>]*@`)
)

// Text masks sensitive fields in a JSON document, form/query encoded text or
// loose log text. Valid JSON is parsed and masked structurally (nested objects
// and arrays included); anything else, including truncated JSON, falls back to
// pattern masking so a cut-off body can never leak a value.
func Text(s string) string {
	budget := &redactionBudget{remaining: maxRedactionWorkBytes}
	return textWithBudget(s, 0, budget)
}

type redactionBudget struct{ remaining int }

func textWithBudget(s string, depth int, budget *redactionBudget) string {
	if s == "" {
		return s
	}
	if depth > maxNestedDepth || len(s) > maxTextBytes || len(s) > budget.remaining {
		return Mask
	}
	budget.remaining -= len(s)
	var v interface{}
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	if err := dec.Decode(&v); err == nil {
		var trailing interface{}
		if err := dec.Decode(&trailing); err != io.EOF {
			return redactLooseText(s, depth, budget)
		}
		switch v.(type) {
		case map[string]interface{}, []interface{}:
			out, err := json.Marshal(maskValue(v, depth, budget))
			if err == nil && len(out) <= maxTextBytes {
				return string(out)
			}
		}
	}
	safe := redactLooseText(s, depth, budget)
	if len(safe) > maxTextBytes {
		return Mask
	}
	return safe
}

func redactLooseText(s string, depth int, budget *redactionBudget) string {
	return redactAssignments(redactURLUserinfo(s), depth, budget)
}

func redactURLUserinfo(s string) string {
	if !strings.Contains(s, "://") || !strings.Contains(s, "@") {
		return s
	}
	return userinfoRe.ReplaceAllString(s, `${1}***@`)
}

func redactAssignments(s string, depth int, budget *redactionBudget) string {
	if !strings.Contains(s, "%") && !containsSensitiveMarker(s) {
		return s
	}
	var out strings.Builder
	written, searchFrom, fields := 0, 0, 0
	for searchFrom <= len(s) {
		indices := assignmentRe.FindStringSubmatchIndex(s[searchFrom:])
		if indices == nil {
			break
		}
		start, end := searchFrom+indices[0], searchFrom+indices[1]
		fields++
		if fields > maxRedactionFields {
			out.WriteString(s[written:start])
			out.WriteString(Mask)
			return out.String()
		}
		group := func(n int) string {
			lo, hi := indices[2*n], indices[2*n+1]
			if lo < 0 || hi < 0 {
				return ""
			}
			return s[searchFrom+lo : searchFrom+hi]
		}
		sensitive := sensitiveEncodedKey(group(3))
		valueStart := end
		if strings.Contains(group(4), ":") && strings.HasPrefix(s[valueStart:], "//") {
			// A URL scheme delimiter is not a key/value assignment.
			searchFrom = end
			continue
		}
		quote := group(5)
		fieldEnd := len(s)
		valueEnd := len(s)
		if quote != "" {
			if closeAt := quotedValueEnd(s, valueStart+1, quote[0]); closeAt >= 0 {
				valueEnd = closeAt + 1
				fieldEnd = scalarValueEnd(s, valueEnd)
			} else {
				valueEnd = len(s)
			}
		} else if valueStart < len(s) && (s[valueStart] == '{' || s[valueStart] == '[') {
			if closeAt := balancedValueEnd(s, valueStart); closeAt >= 0 {
				valueEnd = closeAt
				fieldEnd = scalarValueEnd(s, closeAt)
			}
		} else {
			valueEnd = scalarValueEnd(s, valueStart)
			fieldEnd = valueEnd
		}

		out.WriteString(s[written:valueStart])
		if sensitive {
			out.WriteString(Mask)
			if quote != "" {
				out.WriteByte(quote[0])
			}
		} else if quote != "" {
			if valueEnd == len(s) && (len(s) == 0 || s[valueEnd-1] != quote[0]) {
				out.WriteByte(quote[0])
				out.WriteString(redactNestedValue(s[valueStart+1:], depth+1, budget))
			} else {
				out.WriteString(redactQuotedValue(s[valueStart:valueEnd], quote[0], depth+1, budget))
				out.WriteString(redactNestedValue(s[valueEnd:fieldEnd], depth+1, budget))
			}
		} else {
			out.WriteString(redactNestedValue(s[valueStart:valueEnd], depth+1, budget))
		}
		written, searchFrom = fieldEnd, fieldEnd
	}
	out.WriteString(s[written:])
	return out.String()
}

func redactQuotedValue(raw string, quote byte, depth int, budget *redactionBudget) string {
	if quote == '"' {
		if decoded, err := strconv.Unquote(raw); err == nil {
			safe := redactNestedValue(decoded, depth, budget)
			if safe != decoded {
				return strconv.Quote(safe)
			}
			return raw
		}
	}
	if len(raw) < 2 {
		return raw
	}
	inner := raw[1 : len(raw)-1]
	safe := redactNestedValue(inner, depth, budget)
	if safe == inner {
		return raw
	}
	return string(quote) + safe + string(quote)
}

func redactNestedValue(raw string, depth int, budget *redactionBudget) string {
	if raw == "" {
		return raw
	}
	if !strings.ContainsAny(raw, `%{}[]=?:'"`) && !strings.Contains(raw, "://") {
		return raw
	}
	if depth > maxNestedDepth {
		return Mask
	}
	decoded := raw
	layers := 0
	for strings.Contains(decoded, "%") {
		next, err := url.QueryUnescape(decoded)
		if err != nil {
			return Mask
		}
		if next == decoded {
			break
		}
		if layers == maxNestedDecodeLayers {
			return Mask
		}
		decoded = next
		layers++
	}
	safe := textWithBudget(decoded, depth, budget)
	if safe == decoded {
		return raw
	}
	for i := 0; i < layers; i++ {
		safe = url.QueryEscape(safe)
		if len(safe) > maxTextBytes {
			return Mask
		}
	}
	return safe
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

func maskValue(v interface{}, depth int, budget *redactionBudget) interface{} {
	if depth > maxNestedDepth {
		return Mask
	}
	switch t := v.(type) {
	case string:
		return textWithBudget(t, depth+1, budget)
	case map[string]interface{}:
		for k, val := range t {
			if sensitiveEncodedKey(k) {
				t[k] = Mask
			} else {
				t[k] = maskValue(val, depth+1, budget)
			}
		}
		return t
	case []interface{}:
		for i := range t {
			t[i] = maskValue(t[i], depth+1, budget)
		}
		return t
	}
	return v
}

// URL masks the values of sensitive query parameters in a URL string.
func URL(raw string) string {
	budget := &redactionBudget{remaining: maxRedactionWorkBytes}
	return urlWithBudget(raw, 0, budget)
}

func urlWithBudget(raw string, depth int, budget *redactionBudget) string {
	if len(raw) > maxTextBytes || len(raw) > budget.remaining {
		return Mask
	}
	budget.remaining -= len(raw)
	u, err := url.Parse(raw)
	if err != nil {
		if containsEncodedAssignmentDelimiter(raw) && containsSensitiveMarker(raw) {
			return Mask
		}
		return textWithBudget(stripURLUserinfo(raw), depth, budget)
	}
	// Userinfo is sensitive even when a URL has no query string.
	u.User = nil
	if _, overLimit := redactURLPath(u, depth+1, budget); overLimit {
		return Mask
	}
	if u.RawQuery == "" {
		safe := u.String()
		if len(safe) > maxTextBytes {
			return Mask
		}
		return safe
	}
	if strings.Count(u.RawQuery, "&")+1 > maxRedactionFields {
		return Mask
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		// Invalid escaping can hide a sensitive query key from ParseQuery.
		return textWithBudget(stripURLUserinfo(raw), depth, budget)
	}
	for k, values := range q {
		if sensitiveEncodedKey(k) {
			q.Set(k, Mask)
			continue
		}
		for i, value := range values {
			values[i] = redactNestedValue(value, depth+1, budget)
		}
		q[k] = values
	}
	u.RawQuery = q.Encode()
	safe := u.String()
	if len(safe) > maxTextBytes {
		return Mask
	}
	return safe
}

func redactURLPath(u *url.URL, depth int, budget *redactionBudget) (changed bool, overLimit bool) {
	decoded := u.EscapedPath()
	layers := 0
	for {
		safe := textWithBudget(decoded, depth, budget)
		if safe != decoded {
			u.Path = safe
			u.RawPath = ""
			return true, false
		}
		if !strings.Contains(decoded, "%") {
			return false, false
		}
		next, err := url.PathUnescape(decoded)
		if err != nil {
			return false, true
		}
		if next == decoded {
			return false, false
		}
		if layers == maxNestedDecodeLayers {
			return false, true
		}
		decoded = next
		layers++
	}
}

func containsEncodedAssignmentDelimiter(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "%3f") || strings.Contains(lower, "%3d")
}

func containsSensitiveMarker(s string) bool {
	lower := strings.ToLower(s)
	for _, word := range sensitiveWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
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
