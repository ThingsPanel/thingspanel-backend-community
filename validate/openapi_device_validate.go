package valid

type OpenapiDeviceHistoryDataValidate struct {
	DeviceId string `json:"device_id" alias:"设备id" valid:"Required;MaxSize(36)"`
	Current  int    `json:"current" alias:"条数" valid:"Max(1000000)"`
	Size     int    `json:"size" alias:"条数" valid:"Max(1000000)"`
}

type OpenapiDeviceIdValidate struct {
	DeviceId string `json:"device_id" alias:"设备id" valid:"Required;MaxSize(36)"`
}

type OpenapiDeviceEventCommandHistoryValid struct {
	DeviceId    string `json:"device_id" alias:"设备id" valid:"Required;MaxSize(36)"`
	CurrentPage int    `json:"current_page" alias:"条数" valid:"Max(1000000)"`
	PerPage     int    `json:"per_page" alias:"条数" valid:"Max(1000000)"`
}

type OpenapiCurrentKV struct {
	DeviceId string `json:"device_id" alias:"设备id" valid:"Required;MaxSize(36)"`
}
