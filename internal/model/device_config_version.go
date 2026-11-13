package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var ErrDeviceConfigVersionConflict = errors.New("template metadata version changed")
var ErrInvalidDeviceConfigCondition = errors.New("invalid template If-Match condition")

// DeviceConfigCondition is independent of the explicit native binding revision.
// A nil condition preserves the legacy unconditional caller contract.
type DeviceConfigCondition struct {
	Any  bool
	Tags []string
}

func ParseDeviceConfigCondition(value string) (*DeviceConfigCondition, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > 4096 {
		return nil, ErrInvalidDeviceConfigCondition
	}
	s := strings.TrimSpace(value)
	if s == "*" {
		return &DeviceConfigCondition{Any: true}, nil
	}
	c := &DeviceConfigCondition{}
	for len(s) > 0 {
		weak := strings.HasPrefix(s, "W/")
		if weak {
			s = s[2:]
		}
		if len(s) < 2 || s[0] != '"' {
			return nil, ErrInvalidDeviceConfigCondition
		}
		i := 1
		for i < len(s) && s[i] != '"' {
			if s[i] < 0x21 || s[i] == 0x7f {
				return nil, ErrInvalidDeviceConfigCondition
			}
			i++
		}
		if i == len(s) || len(c.Tags) >= 32 {
			return nil, ErrInvalidDeviceConfigCondition
		}
		tag := s[:i+1]
		if weak {
			tag = "W/" + tag
		}
		c.Tags = append(c.Tags, tag)
		s = strings.TrimSpace(s[i+1:])
		if s == "" {
			break
		}
		if s[0] != ',' {
			return nil, ErrInvalidDeviceConfigCondition
		}
		s = strings.TrimSpace(s[1:])
		if s == "" {
			return nil, ErrInvalidDeviceConfigCondition
		}
	}
	if len(c.Tags) == 0 {
		return nil, ErrInvalidDeviceConfigCondition
	}
	return c, nil
}

// DeviceConfigETag covers the persisted entity, including fields whose existing
// internal writers do not touch updated_at. Never log the serialized entity.
func DeviceConfigETag(config *DeviceConfig) (string, error) {
	if config == nil {
		return "", ErrInvalidDeviceConfigCondition
	}
	canonical := *config
	canonical.CreatedAt = canonical.CreatedAt.UTC()
	canonical.UpdatedAt = canonical.UpdatedAt.UTC()
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	return "\"dc1-" + hex.EncodeToString(h[:]) + "\"", nil
}

func (condition *DeviceConfigCondition) Check(config *DeviceConfig) error {
	if condition == nil || condition.Any {
		return nil
	}
	current, err := DeviceConfigETag(config)
	if err != nil {
		return err
	}
	for _, tag := range condition.Tags {
		if !strings.HasPrefix(tag, "W/") && tag == current {
			return nil
		}
	}
	return ErrDeviceConfigVersionConflict
}
