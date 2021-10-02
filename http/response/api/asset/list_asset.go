package asset

import "github.com/google/uuid"

type ListAssetResponseBody []Asset

type Asset struct {
	// BusinessID : business id
	BusinessID uuid.UUID `json:"business_id"`
	Device     []Device  `json:"device"`
	// ID : asset id
	ID string `json:"id"`
	// Name : asset name
	Name string        `json:"name"`
	Two  []interface{} `json:"two"` // todo- two body
}

// Device : list asset response device
type Device struct {
	// AssetID : asset id
	AssetID uuid.UUID `json:"asset_id"`
	// Dash: dashboard
	Dash []Dash `json:"dash"`
	// Disabled : whether the device is active
	Disabled bool `json:"disabled"`
	// DM : dm label
	DM string `json:"dm"`
	// ID : device id
	ID uuid.UUID `json:"id"`
	// Mapping : device mapping
	Mapping []interface{} `json:"mapping"` // todo - mapping body
	// Name : name
	Name string `json:"name"`
	// State : state
	State string `json:"state"`
	// Type : type of the device ex: gps
	Type string `json:"type"`
}

// Dash : dashboard entry
type Dash struct {
	// Class : example : App\Extensions\WeatherStations\Actions\AirQuality
	Class string `json:"class"`
	// Description : description
	Description string `json:"description"`
	// Fields : dash fields
	Fields map[string]string `json:"fields,omitempty"`
	// Name: dashboard entry name
	Name string `json:"name"`
	// Template: template
	Template string `json:"template"`
	// Thumbnail : reference string to the thumbnail
	Thumbnail string `json:"thumbnail"`
}
