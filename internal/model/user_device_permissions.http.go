package model

// UserDevicePermissionAssignment is the only writable part of a user's
// device scope. Tenant and user are taken from the authenticated route and
// are never accepted from the request body.
type UserDevicePermissionAssignment struct {
	DeviceID    string `json:"device_id" validate:"required,max=36"`
	AccessLevel string `json:"access_level" validate:"required,oneof=read manage"`
}

type UpdateUserDevicePermissionsReq struct {
	Assignments []UserDevicePermissionAssignment `json:"assignments" validate:"omitempty,dive"`
}

type UserDevicePermissionItem struct {
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	DeviceNumber string `json:"device_number"`
	AccessLevel  string `json:"access_level"`
}

type UserDevicePermissionsRsp struct {
	UserID  string                     `json:"user_id"`
	Devices []UserDevicePermissionItem `json:"devices"`
}
