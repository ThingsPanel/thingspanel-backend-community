package warning

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

// ShowWarning : show the warnings / alerts
type ShowWarning struct {
	request.Request
	// WID : wid
	WID struct {
		// ID : warning id
		ID uuid.UUID `json:"id"`
	} `json:"wid"`
}

// Encode : serialize the show warnings request
func (c *ShowWarning) Encode() ([]byte, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize show warnings request
func (c *ShowWarning) Decode(in []byte) error {
	err := json.Unmarshal(in, c)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
