package asset

import (
	"github.com/ThingsPanel/ThingsPanel-Go/http/response/pagination"
	"github.com/google/uuid"
)

// WorkIndexBody : response body for list work request
type WorkIndexBody struct {
	pagination.Info
	Data []WorkIndexRecord
}

// WorkIndexRecord : list entry for work index
type WorkIndexRecord struct {
	// AppID : application id
	AppID string `json:"app_id"`
	// AppSecret : Application secret
	AppSecret string `json:"app_secret"`
	// AppType : type of the application
	AppType string `json:"app_type"`
	// CreatedAt : time of creation in RFC 3339 format
	CreatedAt string `json:"created_at"`
	// ID : unique identification number for the entry
	ID uuid.UUID `json:"id"`
	// IsDevice : signifies whether the application  is a device
	IsDevice bool `json:"is_device"`
	// Name : name of the application
	Name string `json:"name"`
}
