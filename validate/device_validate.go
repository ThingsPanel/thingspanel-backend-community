package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID             string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Token          string `json:"token"`
	Protocol       string `json:"protocol"`
	Port           string `json:"port"`
	Publish        string `json:"publish"`
	Subscribe      string `json:"subscribe"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	AssetID        string `json:"asset_id"`
	Type           string `json:"type"`
	Name           string `json:"name"`
	ChartOption    string `json:"chart_option"`
	DeviceType     string `json:"device_type" gorm:"size:2"`
	ParentId       string `json:"parent_id" gorm:"size:36"`
	ProtocolConfig string `json:"protocol_config"`
}

// AddDevice 校验
type AddDevice struct {
	Token          string `json:"token"`
	Name           string `json:"name"`
	AssetId        string `json:"asset_id"`
	DeviceType     string `json:"device_type" gorm:"size:2"`
	ParentId       string `json:"parent_id" gorm:"size:36"`
	ProtocolConfig string `json:"protocol_config"`
}

// DeleteDevice 校验
type DeleteDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// ConfigureDevice 校验
type ConfigureDevice struct {
	Protocol string `json:"protocol" alias:"protocol" valid:"Required;MaxSize(36)"`
}

// ConfigureDevice 校验
type OperatingDevice struct {
	DeviceId string      `json:"device_id" alias:"device_id" valid:"Required;MaxSize(500)"`
	Values   interface{} `json:"values" alias:"values" valid:"Required"`
}
type Device struct {
	ID             string `json:"id" gorm:"primaryKey,size:36"`
	AssetID        string `json:"asset_id" gorm:"size:36"`              // 资产id
	Token          string `json:"token"`                                // 安全key
	AdditionalInfo string `json:"additional_info" gorm:"type:longtext"` // 存储基本配置
	CustomerID     string `json:"customer_id" gorm:"size:36"`
	Type           string `json:"type"` // 插件类型
	Name           string `json:"name"` // 插件名
	Label          string `json:"label"`
	SearchText     string `json:"search_text"`
	ChartOption    string `json:"chart_option"`
	Protocol       string `json:"protocol" gorm:"size:50"`
	Port           string `json:"port" gorm:"size:50"`
	Publish        string `json:"publish" gorm:"size:255"`
	Subscribe      string `json:"subscribe" gorm:"size:255"`
	Username       string `json:"username" gorm:"size:255"`
	Password       string `json:"password" gorm:"size:255"`
	DId            string `json:"d_id" gorm:"size:255"`
	Location       string `json:"location" gorm:"size:255"`
	DeviceType     string `json:"device_type" gorm:"size:2"`
	ParentId       string `json:"parent_id" gorm:"size:36"`
	ProtocolConfig string `json:"protocol_config"`
}

type UpdateDevice struct {
	AssetID        string `json:"asset_id,omitempty" gorm:"size:36"`              // 资产id
	Token          string `json:"token,omitempty"`                                // 安全key
	AdditionalInfo string `json:"additional_info,omitempty" gorm:"type:longtext"` // 存储基本配置
	Type           string `json:"type,omitempty"`                                 // 插件类型
	Name           string `json:"name,omitempty"`                                 // 插件名
	Label          string `json:"label,omitempty"`
	SearchText     string `json:"search_text,omitempty"`
	ChartOption    string `json:"chart_option"`
	Protocol       string `json:"protocol,omitempty" gorm:"size:50"`
	Port           string `json:"port,omitempty" gorm:"size:50"`
	Publish        string `json:"publish,omitempty" gorm:"size:255"`
	Subscribe      string `json:"subscribe,omitempty" gorm:"size:255"`
	Username       string `json:"username,omitempty" gorm:"size:255"`
	Password       string `json:"password,omitempty" gorm:"size:255"`
	DId            string `json:"d_id,omitempty" gorm:"size:255"`
	Location       string `json:"location,omitempty" gorm:"size:255"`
	DeviceType     string `json:"device_type" gorm:"size:2"`
	ParentId       string `json:"parent_id" gorm:"size:36"`
	ProtocolConfig string `json:"protocol_config"`
}

type ResetDevice struct {
	DeviceId  string `json:"device_id" alias:"device_id" valid:"Required;MaxSize(99)"`
	ValidTime int    `json:"valid_time" alias:"valid_time" valid:"Required;Min(10)"`
}

// WarningLogListValidate 校验
type DevicePageListValidate struct {
	AssetId     string `json:"asset_id" alias:"资产id" valid:"MaxSize(36)"`
	BusinessId  string `json:"business_id" alias:"业务id" valid:"MaxSize(36)"`
	DeviceId    string `json:"device_id" alias:"设备id" valid:"MaxSize(36)"`
	CurrentPage int    `json:"current_page" alias:"页码" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page" alias:"条数" valid:"Required;Min(10)"`
	DeviceType  string `json:"device_type" alias:"设备id" valid:"MaxSize(36)"`
	Token       string `json:"token" alias:"设备id" valid:"MaxSize(36)"`
	Name        string `json:"name" alias:"设备名称" valid:"MaxSize(99)"`
	ParentId    string `json:"parent_id" gorm:"size:36"`
}

type AccessTokenValidate struct {
	AccessToken string `json:"AccessToken" alias:"密钥" valid:"Required;MaxSize(36)"`
}
