package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// nativeJSONFile rejects ambiguous duplicate keys and preserves large integer
// revisions rather than rounding them through float64.
func nativeJSONFile(path string) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("需要 --file")
	}
	raw, err := readFileOrStdin(path)
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := validateUniqueJSON(d); err != nil {
		return nil, fmt.Errorf("JSON 无效: %w", err)
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("JSON 必须只有一个值")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var body map[string]any
	if err := d.Decode(&body); err != nil || body == nil {
		return nil, fmt.Errorf("需要 JSON 对象")
	}
	return body, nil
}

func validateUniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("对象键无效")
			}
			if seen[name] {
				return fmt.Errorf("重复字段 %q", name)
			}
			seen[name] = true
			if err := validateUniqueJSON(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := validateUniqueJSON(d); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("JSON 结构无效")
	}
	_, err = d.Token()
	return err
}
