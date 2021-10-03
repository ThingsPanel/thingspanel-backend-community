package warning

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

// DeleteWarning : delete a warning / alert
type DeleteWarning struct {
	request.Request
	// ID : warning id
	ID uuid.UUID `json:"id"`
}

// Encode : serialize delete warning request
func (c *DeleteWarning) Encode() ([]byte, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize delete warning request
func (c *DeleteWarning) Decode(in []byte) error {
	err := json.Unmarshal(in, c)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
