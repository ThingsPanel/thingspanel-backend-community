package automation

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

type Edit struct {
	request.Request
	// BusinessID: business id
	BusinessID uuid.UUID `json:"business_id"`
	// Config : automation configurations
	Config []Config `json:"config"`
	// Description : nominal description
	Description string `json:"describe"`
	// Issued : 1 if true
	Issued string `json:"issued"`
	// Name : name string
	Name string `json:"name"`
	// Sort : set to "1" if sorting is required
	Sort string `json:"sort"`
	// Status : status code , 1 if active
	Status int `json:"status"`
	// Type : type code
	Type string `json:"type"`
}

// Encode : serialize edit automation request
func (l *Edit) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize edit automation request
func (l *Edit) Decode(in []byte) error {
	err := json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
