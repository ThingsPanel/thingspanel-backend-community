package valid

type TpDataTransponAddValid struct {
	Name              string                          `json:"name" valid:"Required;MaxSize(36)"`
	Desc              string                          `json:"desc,omitempty"`
	Script            string                          `json:"script,omitempty"`
	WarningStrategyId string                          `json:"warning_strategy_id,omitempty"`
	WarningSwitch     int                             `json:"warning_switch,omitempty"`
	TargetInfo        TpDataTransponTargetInfoValid   `json:"target_info,omitempty"`
	DeviceInfo        []TpDataTransponDeviceInfoValid `json:"device_info"`
}

type TpDataTransponEditValid struct {
	Id                string                          `json:"id" valid:"Required;MaxSize(36)"`
	Name              string                          `json:"name" valid:"Required;MaxSize(36)"`
	Desc              string                          `json:"desc,omitempty"`
	Script            string                          `json:"script,omitempty"`
	WarningStrategyId string                          `json:"warning_strategy_id,omitempty"`
	WarningSwitch     int                             `json:"warning_switch,omitempty"`
	TargetInfo        TpDataTransponTargetInfoValid   `json:"target_info"`
	DeviceInfo        []TpDataTransponDeviceInfoValid `json:"device_info"`
}

type TpDataTransponDetailValid struct {
	DataTranspondId string `json:"data_transpond_id" valid:"Required;MaxSize(36)"`
}

type TpDataTransponSwitchValid struct {
	DataTranspondId string `json:"data_transpond_id" valid:"Required;MaxSize(36)"`
	Switch          int    `json:"switch"`
}

type TpDataTransponTargetInfoValid struct {
	URL  string                            `json:"url,omitempty"`
	MQTT TpDataTransponTargetInfoMQTTValid `json:"mqtt,omitempty"`
}

type TpDataTransponTargetInfoMQTTValid struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	UserName string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	ClientId string `json:"client_id,omitempty"`
	Topic    string `json:"topic,omitempty"`
}

type TpDataTransponDeviceInfoValid struct {
	DeviceId     string `json:"device_id"`
	MessageType  int    `json:"message_type"`
	BusinessId   string `json:"business_id"`
	AssetGroupId string `json:"asset_group_id"`
}

type TpDataTransponListValid struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
}
