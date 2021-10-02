package automation

import "github.com/google/uuid"

type PropertyResponseBody []Property

// Property : automation property
type Property struct {
	// AdditionalInfo : additional information about the property
	AdditionalInfo *string `json:"additional_info"`
	// BusinessID : business id number
	BusinessID uuid.UUID `json:"business_id"`
	// CustomerID : customer id number
	CustomerID *uuid.UUID `json:"customer_id"`
	// ID : property id number
	ID uuid.UUID `json:"id"`
	// Name : name
	Name string `json:"name"`
	// ParentID : property parent id
	ParentID string `json:"parent_id"`
	// SearchText : search text
	SearchText *string `json:"search_text"`
	// Tier : tier
	Tier int `json:"tier"`
	// Type: type
	Type *string `json:"type"`
}
