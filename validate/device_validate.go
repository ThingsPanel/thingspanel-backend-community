package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID             string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
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
	SubDeviceAddr  string `json:"sub_device_addr,omitempty" alias:"子设备地址" valid:"MaxSize(36)"`
	ScriptId       string `json:"script_id" gorm:"size:36"`
}

// AddDevice 校验
type AddDevice struct {
	Token          string `json:"token"`
	Name           string `json:"name"`
	AssetId        string `json:"asset_id"`
	DeviceType     string `json:"device_type" gorm:"size:2"`
	ParentId       string `json:"parent_id" gorm:"size:36"`
	ProtocolConfig string `json:"protocol_config"`
	SubDeviceAddr  string `json:"sub_device_addr,omitempty" alias:"子设备地址" valid:"MaxSize(36)"`
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
	SubDeviceAddr  string `json:"sub_device_addr,omitempty" alias:"子设备地址" valid:"MaxSize(36)"`
	ScriptId       string `json:"script_id" gorm:"size:36"`
	CreatedAt      int64  `json:"created_at,omitempty" alias:"创建时间" `
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
	SubDeviceAddr  string `json:"sub_device_addr,omitempty" alias:"子设备地址" valid:"MaxSize(36)"`
	ScriptId       string `json:"script_id" gorm:"size:36"`
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
	NotGateway  int    `json:"not_gateway" valid:"Max(2)"`
}

// 地图显示设备校验
type DeviceMapValidate struct {
	GroupId       string `json:"group_id" alias:"分组id" valid:"MaxSize(36)"`
	BusinessId    string `json:"business_id" alias:"业务id" valid:"MaxSize(36)"`
	DeviceId      string `json:"device_id" alias:"设备id" valid:"MaxSize(36)"`
	DeviceType    string `json:"device_type" alias:"设备id" valid:"MaxSize(36)"`
	DeviceModelId string `json:"device_model_id" alias:"设备插件id" valid:"MaxSize(36)"`
	Name          string `json:"name" alias:"设备名称" valid:"MaxSize(99)"`
}

type AccessTokenValidate struct {
	AccessToken string `json:"AccessToken" alias:"密钥" valid:"Required;MaxSize(36)"`
}
type ProtocolFormValidate struct {
	ProtocolType string `json:"protocol_type" alias:"协议类型" valid:"Required;MaxSize(36)"`
	DeviceType   string `json:"device_type" alias:"设备类型" valid:"Required;MaxSize(36)"`
}
type TokenSubDeviceAddrValidate struct {
	AccessToken   string `json:"AccessToken" alias:"网关密钥" valid:"Required;MaxSize(36)"`
	SubDeviceAddr string `json:"SubDeviceAddr" alias:"子设备地址" valid:"Required;MaxSize(36)"`
}
type DeviceIdListValidate struct {
	DeviceIdList []string `json:"device_id_list" alias:"设备id" valid:"Required;MaxSize(36)"`
}

type RspDevicePaginationValidate struct {
	CurrentPage int                      `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                      `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []map[string]interface{} `json:"data" alias:"返回数据"`
	Total       int64                    `json:"total" alias:"总数" valid:"Max(10000)"`
}
type DevicePaginationValidate struct {
	CurrentPage    int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage        int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	CurrentVersion string `json:"current_version" alias:"版本" valid:"MaxSize(36)"`
	Name           string `json:"name"  alias:"名称" valid:"MaxSize(36)"`
	ProductId      string `json:"product_id,omitempty" alias:"产品id"`
}
