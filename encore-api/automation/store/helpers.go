package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"encore.app/automation/domain"
)

func domainSignal(s string) domain.SignalKind { return domain.SignalKind(s) }

func domainQuality(q string) domain.Quality { return domain.Quality(q) }

// paramsJSON turns a parameter map into a value for a jsonb column. An empty map
// becomes NULL so "no binding" and "a binding with no overrides" stay
// distinguishable in the row itself.
func paramsJSON(m map[string]string) any {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}

// paramsScanner reads a jsonb column into a parameter map.
type paramsScanner map[string]string

func (p *paramsScanner) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*p = map[string]string{}
		return nil
	case []byte:
		if len(v) == 0 {
			*p = map[string]string{}
			return nil
		}
		var m map[string]string
		if err := json.Unmarshal(v, &m); err != nil {
			return fmt.Errorf("decode binding params: %w", err)
		}
		*p = m
		return nil
	case string:
		return p.Scan([]byte(v))
	default:
		return fmt.Errorf("unexpected binding params type %T", src)
	}
}

func (p paramsScanner) Value() (driver.Value, error) {
	if len(p) == 0 {
		return nil, nil
	}
	return json.Marshal(map[string]string(p))
}
