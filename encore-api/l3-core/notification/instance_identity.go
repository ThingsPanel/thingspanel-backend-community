package notification

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"encore.app/l3-core/notification/contract"
)

var identityFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validIdentityFieldDeclarations(fields []string) bool {
	if fields == nil {
		return true // legacy manifest with no identity_fields declaration
	}
	if len(fields) == 0 || len(fields) > 64 {
		return false
	}
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if len(field) == 0 || len(field) > 128 || !identityFieldPattern.MatchString(field) {
			return false
		}
		if _, duplicate := seen[field]; duplicate {
			return false
		}
		seen[field] = struct{}{}
	}
	return true
}

func validateIdentityFieldsPayload(raw []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return false
	}
	encoded, exists := object["identity_fields"]
	if !exists {
		return true
	}
	var fields []string
	if json.Unmarshal(encoded, &fields) != nil || fields == nil || !validIdentityFieldDeclarations(fields) {
		return false
	}
	return true
}

// identityFieldsChanged compares the declared identity fields in complete
// config snapshots, including values that are normally stored as encrypted
// secret slots. Older registrations have no declaration, so their complete
// effective config is conservatively treated as identity.
func identityFieldsChanged(identityFields []string, before, after map[string]json.RawMessage) (bool, error) {
	fields := identityFields
	if len(fields) == 0 {
		seen := make(map[string]struct{}, len(before)+len(after))
		fields = make([]string, 0, len(before)+len(after))
		for field := range before {
			seen[field] = struct{}{}
			fields = append(fields, field)
		}
		for field := range after {
			if _, ok := seen[field]; !ok {
				fields = append(fields, field)
			}
		}
	}
	for _, field := range fields {
		beforeValue, beforePresent := before[field]
		afterValue, afterPresent := after[field]
		if beforePresent != afterPresent {
			return true, nil
		}
		if !beforePresent {
			continue
		}
		beforeCanonical, err := canonicalInstanceJSONValue(beforeValue)
		if err != nil {
			return false, err
		}
		afterCanonical, err := canonicalInstanceJSONValue(afterValue)
		if err != nil {
			return false, err
		}
		if !bytes.Equal(beforeCanonical, afterCanonical) {
			return true, nil
		}
	}
	return false, nil
}

func canonicalInstanceJSONValue(raw json.RawMessage) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	return json.Marshal(value)
}

func decodeCompleteInstanceConfig(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var config map[string]json.RawMessage
	if contract.DecodeStrictJSON(raw, &config) != nil || config == nil {
		return nil, ErrManagementInvalid
	}
	return config, nil
}

func completeInstanceConfig(config map[string]json.RawMessage, secrets map[string]string) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(config)+len(secrets))
	for field, value := range config {
		result[field] = append(json.RawMessage(nil), value...)
	}
	for field, value := range secrets {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		result[field] = encoded
	}
	return result, nil
}

func applyInstanceConfigPatch(current json.RawMessage, update contract.InstanceUpdate) (map[string]json.RawMessage, error) {
	config, err := decodeCompleteInstanceConfig(current)
	if err != nil {
		return nil, err
	}
	if len(update.ConfigPatch) > 0 {
		config, err = mergeConfigPatchFields(config, update.ConfigPatch)
		if err != nil {
			return nil, ErrManagementInvalid
		}
	}
	if update.Secrets != nil {
		for field, value := range update.Secrets.Set {
			if containsString(update.Secrets.Clear, field) {
				return nil, ErrManagementInvalid
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, ErrManagementInvalid
			}
			config[field] = encoded
		}
		for _, field := range update.Secrets.Clear {
			delete(config, field)
		}
	}
	return config, nil
}

func instanceIdentityUpdateChanges(identityFields []string, current json.RawMessage, update contract.InstanceUpdate) (bool, error) {
	before, err := decodeCompleteInstanceConfig(current)
	if err != nil {
		return false, err
	}
	after, err := applyInstanceConfigPatch(current, update)
	if err != nil {
		return false, err
	}
	return identityFieldsChanged(identityFields, before, after)
}
