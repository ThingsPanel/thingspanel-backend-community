package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxContractJSONBytes = 1 << 20
	maxContractJSONDepth = 64
)

// DecodeStrictJSON validates the raw representation before decoding a DTO.
// encoding/json otherwise accepts duplicate object keys and accepts null into
// pointer/map fields where the contract means omission. Structural DTO fields
// reject explicit null, while fields tagged strictjson:"dynamic-object" or
// strictjson:"dynamic-values" preserve arbitrary JSON values including null.
func DecodeStrictJSON(data []byte, dst any) error {
	if len(data) == 0 || len(data) > maxContractJSONBytes {
		return fmt.Errorf("notification JSON body size must be between 1 and %d bytes", maxContractJSONBytes)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("notification JSON body must be valid UTF-8")
	}

	check := json.NewDecoder(bytes.NewReader(data))
	check.UseNumber()
	if err := scanStrictValue(check, 0); err != nil {
		return err
	}
	if _, err := check.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	if err := validateStrictNulls(data, dst); err != nil {
		return err
	}

	decode := json.NewDecoder(bytes.NewReader(data))
	decode.DisallowUnknownFields()
	if err := decode.Decode(dst); err != nil {
		return err
	}
	if err := decode.Decode(new(any)); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func scanStrictValue(decoder *json.Decoder, depth int) error {
	if depth > maxContractJSONDepth {
		return fmt.Errorf("notification JSON nesting exceeds %d levels", maxContractJSONDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return fmt.Errorf("invalid object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("object key must be a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON object key %q", key)
				}
				seen[key] = struct{}{}
				if err := scanStrictValue(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("unterminated JSON object")
			}
		case '[':
			for decoder.More() {
				if err := scanStrictValue(decoder, depth+1); err != nil {
					return err
				}
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("unterminated JSON array")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
	}
	return nil
}

func validateStrictNulls(data []byte, dst any) error {
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return fmt.Errorf("notification JSON destination must be a non-nil pointer")
	}
	return validateStrictValue(data, t.Elem(), "$", "")
}

func validateStrictValue(data []byte, typ reflect.Type, path, mode string) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("%s: missing JSON value", path)
	}
	if bytes.Equal(trimmed, []byte("null")) {
		return fmt.Errorf("%s: explicit null is not supported for structural fields", path)
	}
	if mode == "dynamic-object" {
		if trimmed[0] != '{' {
			return fmt.Errorf("%s: dynamic JSON field must be an object", path)
		}
		return nil
	}
	if typ == reflect.TypeOf(json.RawMessage{}) {
		return rejectNestedNulls(trimmed, path)
	}
	if typ.Kind() == reflect.Pointer {
		return validateStrictValue(trimmed, typ.Elem(), path, "")
	}
	switch typ.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &fields); err != nil {
			return nil // The ordinary DTO decode below reports the type error.
		}
		fieldTypes := jsonFieldTypes(typ)
		for name, raw := range fields {
			field, ok := fieldTypes[name]
			if !ok {
				return fmt.Errorf("%s.%s: unknown field", path, name)
			}
			fieldMode := field.Tag.Get("strictjson")
			if err := validateStrictValue(raw, field.Type, path+"."+name, fieldMode); err != nil {
				return err
			}
		}
	case reflect.Map:
		var values map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return nil // The ordinary DTO decode below reports the type error.
		}
		if mode == "dynamic-values" {
			return nil
		}
		for key, raw := range values {
			if err := validateStrictValue(raw, typ.Elem(), path+"["+key+"]", ""); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		var values []json.RawMessage
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return nil // The ordinary DTO decode below reports the type error.
		}
		for i, raw := range values {
			if err := validateStrictValue(raw, typ.Elem(), path+"["+strconv.Itoa(i)+"]", ""); err != nil {
				return err
			}
		}
	case reflect.Interface:
		return rejectNestedNulls(trimmed, path)
	}
	return nil
}

func jsonFieldTypes(typ reflect.Type) map[string]reflect.StructField {
	fields := make(map[string]reflect.StructField)
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			for name, embedded := range jsonFieldTypes(field.Type) {
				fields[name] = embedded
			}
			continue
		}
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field
	}
	return fields
}

func rejectNestedNulls(data []byte, path string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return scanStrictNulls(decoder, 0, path)
}

func scanStrictNulls(decoder *json.Decoder, depth int, path string) error {
	if depth > maxContractJSONDepth {
		return fmt.Errorf("%s: notification JSON nesting exceeds %d levels", path, maxContractJSONDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if token == nil {
		return fmt.Errorf("%s: explicit null is not supported for structural fields", path)
	}
	switch delimiter := token.(type) {
	case json.Delim:
		switch delimiter {
		case '{':
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, _ := keyToken.(string)
				if err := scanStrictNulls(decoder, depth+1, path+"."+key); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		case '[':
			for i := 0; decoder.More(); i++ {
				if err := scanStrictNulls(decoder, depth+1, path+"["+strconv.Itoa(i)+"]"); err != nil {
					return err
				}
			}
			_, err := decoder.Token()
			return err
		}
	}
	return nil
}
