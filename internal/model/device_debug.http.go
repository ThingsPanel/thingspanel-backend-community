package model

// DeviceDebugConfig mirrors the gmqtt-side debug config stored in Redis.
type DeviceDebugConfig struct {
	Enabled         bool  `json:"enabled"`
	ExpireAt        int64 `json:"expire_at"`
	MaxItems        int   `json:"max_items"`
	PayloadMaxBytes int   `json:"payload_max_bytes"`
}

// SetDeviceDebugReq enables/disables debug and updates config.
// If both Duration and ExpireAt are omitted, Duration defaults to 30 minutes.
// If Enabled is explicitly false, config will be removed (debug off).
type SetDeviceDebugReq struct {
	Enabled         *bool  `json:"enabled" validate:"omitempty"`
	Duration        *int64 `json:"duration" validate:"omitempty,gte=0,lte=604800"` // seconds, up to 7 days
	ExpireAt        *int64 `json:"expire_at" validate:"omitempty,gte=0"`
	MaxItems        *int   `json:"max_items" validate:"omitempty,gte=1,lte=5000"`
	PayloadMaxBytes *int   `json:"payload_max_bytes" validate:"omitempty,gte=0,lte=65536"`
}

type GetDeviceDebugLogsReq struct {
	Offset int64 `json:"offset" form:"offset" validate:"omitempty,gte=0"`
	Limit  int64 `json:"limit" form:"limit" validate:"omitempty,gte=1,lte=500"`
}

// DeviceDebugLogEntry is stored as JSON strings in Redis list.
type DeviceDebugLogEntry struct {
	Ts       string `json:"ts"`
	DeviceID string `json:"device_id"`

	Protocol  string `json:"protocol,omitempty"`
	Direction string `json:"direction"`

	// Current fields (protocol-agnostic)
	Action  string                 `json:"action,omitempty"`
	Outcome string                 `json:"outcome,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`

	Error   string `json:"error,omitempty"`
	Payload string `json:"payload,omitempty"`

	// Legacy fields (backward compatibility for previously written logs)
	Event  string                 `json:"event,omitempty"`
	Result string                 `json:"result,omitempty"`
	Extra  map[string]interface{} `json:"extra,omitempty"`
}
