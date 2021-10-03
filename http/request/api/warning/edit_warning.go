package warning

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

type EditWarning struct {
	request.Request
	// BusinessID : business id
	BusinessID uuid.UUID `json:"bid"`
	// Config: conditions for the warning
	Config []Config `json:"config"`
	// Description : description
	Description string `json:"describe"`
	// ID : warning id
	ID uuid.UUID `json:"id"`
	// WID : wid
	WID uuid.UUID `json:"wid"`
	// Name : name
	Name string `json:"name"`
	// SensorID : sensor id
	SensorID uuid.UUID `json:"sensor"`
	// Message: message to be sent
	Message string `json:"message"`
}

// Encode : serialize the edit warning request
func (c *EditWarning) Encode() ([]byte, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize edit warning request
func (c *EditWarning) Decode(in []byte) error {
	err := json.Unmarshal(in, c)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
