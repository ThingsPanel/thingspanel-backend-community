package asset

import "github.com/google/uuid"

type EditAssetRequest struct {
	ID uuid.UUID `json:"id"`
}

type Asset struct {
	// ID : entry id
	ID uuid.UUID `json:"id"`
	// Name : name
	Name string `json:"name"`
	// BusinessID : business identification number
	BusinessID uuid.UUID `json:"business_id"`
	// Device : devices bound by the asset
	Device []Device `json:"device"`
	// Two : two
	Two []interface{} `json:"two"` // todo- define two structure
}

// Device : edit asset device object
type Device struct {
	// ID : device id in uuid format
	ID uuid.UUID `json:"id"`
	// AssetID : asset id in uuid format
	AssetID string `json:"asset_id"`
	// Type : type of the device
	Type string `json:"type"`
	// Name : name of the device
	Name string `json:"name"`
	// Disabled : signifies whether the device is disabled or not
	Disabled bool
	// DM : dm name
	DM string `json:"dm"`
	// State: state
	State string `json:"state"`
	// Dash: dashboard entries
	Dash []Dash `json:"dash"`
	// Mapping : mapping
	Mapping []interface{} `json:"mapping"` // todo - mapping body
}

// Dash : edit asset dashboard object
type Dash struct {
	// Name: name of the entry
	Name string `json:"name"`
	// Description : description of the dashboard entry
	Description string `json:"description"`
	// Class : example : "App\\ Extensions\\ Gps\\ Actions\\ Marker"
	Class string `json:"class"`
	// Thumbnail : reference to the thumbnail
	Thumbnail string `json:"thumbnail"`
	// Template : template name
	Template string `json:"template"`
	// Fields: dashboard entry additional attributes
	Fields map[string]string `json:"fields"`
}

// Field : edit asset dashboard entry field
type Field struct {
	Temperature string `json:"temperature"`
	Longitude   string `json:"longitude"`
	Latitude    string `json:"latitude"`
}
