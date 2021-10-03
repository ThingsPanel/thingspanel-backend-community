package automation

import (
	"encoding/json"
	"github.com/ThingsPanel/ThingsPanel-Go/domain/exception"
	"github.com/ThingsPanel/ThingsPanel-Go/http/request/request"
	"github.com/google/uuid"
)

type Add struct {
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

// Config : automation rule configuration
type Config struct {
	// Apply : apply
	Apply []interface{} `json:"apply"` // todo - apply object body
	// Rules : config  rules
	Rules []Rule `json:"rules"`
}

// Rule : automation rule
type Rule struct {
	// AssetID : asset id
	AssetID uuid.UUID `json:"asset_id"`
	// DeviceID : device id
	DeviceID uuid.UUID `json:"device_id"`
	// Duration : duration in seconds
	Duration int64 `json:"duration"`
	// Field : field related to the constraint
	Field string `json:"field"`
}

// Encode : serialize add automation request
func (l *Add) Encode() ([]byte, error) {
	byt, err := json.Marshal(l)
	if err != nil {
		return nil, exception.NewEncodingError(err)
	}

	return byt, nil
}

// Decode : deserialize add automation request
func (l *Add) Decode(in []byte) error {
	err := json.Unmarshal(in, l)
	if err != nil {
		return exception.NewDecodingError(err)
	}

	return nil
}
