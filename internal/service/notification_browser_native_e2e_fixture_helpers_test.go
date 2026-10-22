package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// nativeBrowserE2EFixture is deliberately limited to non-secret identifiers
// created through the local browser UI. Runtime endpoints and credentials stay
// fixed in the opt-in test and cannot be supplied through this file.
type nativeBrowserE2EFixture struct {
	SchemaVersion       int    `json:"schemaVersion"`
	SourceDeploymentID  string `json:"sourceDeploymentId"`
	TenantID            string `json:"tenantId"`
	NativeInstanceID    string `json:"nativeInstanceId"`
	NativeGroupID       string `json:"nativeGroupId"`
	NativeGroupRevision int64  `json:"nativeGroupRevision"`
}

func readNativeBrowserE2EFixture(path string) (nativeBrowserE2EFixture, error) {
	var fixture nativeBrowserE2EFixture
	if !filepath.IsAbs(path) {
		return fixture, errors.New("fixture path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() < 1 || info.Size() > 4096 {
		return fixture, errors.New("fixture file unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 1 || len(raw) > 4096 {
		return fixture, errors.New("fixture file unavailable")
	}
	if first := bytes.TrimSpace(raw); len(first) == 0 || first[0] != '{' {
		return fixture, errors.New("fixture JSON invalid")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return fixture, errors.New("fixture JSON invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		return nativeBrowserE2EFixture{}, errors.New("fixture JSON invalid")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nativeBrowserE2EFixture{}, errors.New("fixture JSON invalid")
	}
	return fixture, nil
}

// rejectDuplicateJSONKeys rejects ambiguous object members before decoding the
// fixed fixture structure. The parser never includes input values in errors.
func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := readJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}

func readJSONValue(decoder *json.Decoder) (json.Token, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("object key invalid")
			}
			if _, duplicate := seen[key]; duplicate {
				return nil, errors.New("duplicate object key")
			}
			seen[key] = struct{}{}
			if _, err := readJSONValue(decoder); err != nil {
				return nil, err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errors.New("object invalid")
		}
	case '[':
		for decoder.More() {
			if _, err := readJSONValue(decoder); err != nil {
				return nil, err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errors.New("array invalid")
		}
	default:
		return nil, errors.New("JSON delimiter invalid")
	}
	return token, nil
}
