package uplink

import (
	"encoding/json"
	"errors"
)

// The published model permits unbounded INT. Community's existing converters
// and number_v storage use float64. A semantically valid but unrepresentable
// number is a distinct storage-capacity rejection, never a schema violation
// and never an excuse to wrap a native message as _raw.
type storageRepresentationRejection struct{}

func (storageRepresentationRejection) Error() string {
	return "native uplink number exceeds current storage range"
}
func (storageRepresentationRejection) Permanent() bool { return true }
func validateNativeStorageRepresentation(family string, raw []byte) error {
	if family == "event" {
		var e struct {
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		raw = e.Params
	}
	var values map[string]interface{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return storageRepresentationRejection{}
	}
	return nil
}
func preparationFailureReason(err error) string {
	var capacity storageRepresentationRejection
	if errors.As(err, &capacity) {
		return "数值超出当前存储范围，数据未写入"
	}
	return "上报数据校验或处理失败"
}
