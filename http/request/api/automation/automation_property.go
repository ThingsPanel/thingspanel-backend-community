package automation

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

// Property : automation property
type Property struct {
	request.Request
	// BusinessID : business id
	BusinessID uuid.UUID `json:"business_id"`
}

// Encode : serialize the property request
func (l *Property) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize property request
func (l *Property) Decode(in []byte) error {
	err := json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
