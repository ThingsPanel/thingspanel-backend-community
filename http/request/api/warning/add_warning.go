package warning

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
)

// AddWarning : add a new warning alert
type AddWarning struct {
	request.Request
	// ID : warning id
	ID string `json:"wid"`
	// Name : name string
	Name string `json:"name"`
	// Description : description string
	Description string `json:"describe"`
	// BusinessID : business id
	BusinessID string `json:"bid"`
	// Sensor : sensor
	Sensor string `json:"sensor"`
	// Message: message
	Message string `json:"message"`
	// Config : warning configurations
	Config []Config `json:"config"`
}

// Config : warning configuration entry
type Config struct {
	// Field : field of concern
	Field string `json:"field"`
	// Condition : boundary condition
	Condition string `json:"condition"`
	// Value : boundary value
	Value string `json:"value"`
}

// Encode : serialize the add warning request
func (c *Config) Encode() ([]byte, error) {
	byt, err := json.Marshal(c)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize add warning request
func (c *Config) Decode(in []byte) error {
	err := json.Unmarshal(in, c)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
