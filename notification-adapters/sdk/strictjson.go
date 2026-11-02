package sdk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	DefaultMaxRequestBytes = 1 << 20
	maxJSONDepth           = 32
)

var ErrInvalidJSON = errors.New("invalid JSON request")

// decodeStrict rejects duplicate keys, null at any depth, trailing values and
// oversized bodies before decoding the wire struct. The second decode disables
// unknown struct fields; explicit key allowlists below also prevent encoding/json
// from accepting case-folded names such as Delivery_ID.
func decodeStrict(body io.Reader, limit int64, target any) (map[string]any, error) {
	if limit <= 0 {
		limit = DefaultMaxRequestBytes
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, ErrInvalidJSON
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseValue(dec, 0)
	if err != nil {
		return nil, ErrInvalidJSON
	}
	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidJSON
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, ErrInvalidJSON
	}
	canonical, err := json.Marshal(root)
	if err != nil {
		return nil, ErrInvalidJSON
	}
	strict := json.NewDecoder(bytes.NewReader(canonical))
	strict.DisallowUnknownFields()
	if err := strict.Decode(target); err != nil {
		return nil, ErrInvalidJSON
	}
	if err := ensureEOF(strict); err != nil {
		return nil, ErrInvalidJSON
	}
	return root, nil
}

func parseValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, ErrInvalidJSON
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		if tok == nil {
			return nil, ErrInvalidJSON
		}
		return tok, nil
	}
	switch delim {
	case '{':
		obj := make(map[string]any)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok || strings.ContainsRune(key, '\x00') {
				return nil, ErrInvalidJSON
			}
			if _, exists := obj[key]; exists {
				return nil, ErrInvalidJSON
			}
			value, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj[key] = value
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') {
			return nil, ErrInvalidJSON
		}
		return obj, nil
	case '[':
		items := make([]any, 0)
		for dec.More() {
			value, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, value)
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') {
			return nil, ErrInvalidJSON
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%w", ErrInvalidJSON)
	}
}

func ensureEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return ErrInvalidJSON
	}
	return nil
}

func exactKeys(obj map[string]any, required []string, optional ...string) bool {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, exists := obj[key]; !exists {
			return false
		}
	}
	for _, key := range optional {
		allowed[key] = struct{}{}
	}
	for key := range obj {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func validateValidateKeys(obj map[string]any) bool {
	config, ok := obj["config"].(map[string]any)
	return exactKeys(obj, []string{"api_version", "tenant_id", "instance_id", "config_version", "config"}) && ok && config != nil
}

func validateSendKeys(obj map[string]any) bool {
	if !exactKeys(obj, []string{"api_version", "delivery_id", "attempt_id", "tenant_id", "instance_id", "config_version", "channel", "recipient", "content", "config", "expires_at"}) {
		return false
	}
	recipient, ok := obj["recipient"].(map[string]any)
	if !ok || !exactKeys(recipient, []string{"kind", "address"}) {
		return false
	}
	config, ok := obj["config"].(map[string]any)
	if !ok || config == nil {
		return false
	}
	content, ok := obj["content"].(map[string]any)
	if !ok {
		return false
	}
	kind, _ := content["kind"].(string)
	switch kind {
	case "text":
		if !exactKeys(content, []string{"kind", "text"}, "title") {
			return false
		}
	case "template":
		if !exactKeys(content, []string{"kind", "template"}, "title") {
			return false
		}
		tpl, ok := content["template"].(map[string]any)
		if !ok || !exactKeys(tpl, []string{"id", "params"}, "locale") {
			return false
		}
		if _, ok := tpl["params"].(map[string]any); !ok {
			return false
		}
	default:
		return false
	}
	return true
}
