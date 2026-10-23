package devicemetadata

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Version numbers are allocation metadata. Compare the persisted semantic
// payload without that number so an unchanged save cannot create a new version.
func samePublicationContent(a, b json.RawMessage) (bool, error) {
	normalize := func(raw json.RawMessage) ([]byte, error) {
		if !json.Valid(raw) {
			return nil, fmt.Errorf("invalid snapshot JSON")
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var object map[string]any
		if err := dec.Decode(&object); err != nil || object == nil {
			return nil, fmt.Errorf("invalid snapshot object")
		}
		delete(object, "version")
		return json.Marshal(object)
	}
	left, err := normalize(a)
	if err != nil {
		return false, err
	}
	right, err := normalize(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(left, right), nil
}
