package warning

import (
	"github.com/ThingsPanel/ThingsPanel-Go/http/response/pagination"
	"github.com/google/uuid"
)

// ShowWarningResponseBody : show warning response body
type ShowWarningResponseBody struct {
	pagination.Info
	Data []Warning `json:"data"`
}

// Warning : warning
type Warning struct {
	// BusinessID : business id
	BusinessID uuid.UUID `json:"business_id"`
	// Config : warning conditions
	Config []Config `json:"config"`
	// CustomerID : customer id number
	CustomerID *uuid.UUID `json:"customer_id"`
	// Description: description, ex: "oxygen data"
	Description string `json:"describe"`
	// ID : unique id to address the warning
	ID uuid.UUID `json:"id"`
	// Name : name
	Name string `json:"name"`
	// Sensor: sensor id
	SensorID uuid.UUID `json:"sensor"`
	// WorkID : work id
	WorkID uuid.UUID `json:"wid"`
}

// Config : configuration entry for the warning
type Config struct {
	// Condition : boolean condition , ex: "<"
	Condition string `json:"condition"`
	// Field : field to be bounded , ex: "nai"
	Field string `json:"field"`
	// Value : bounded value , ex: "7"
	Value string `json:"value"`
}
