package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID        string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Token     string `json:"token"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Publish   string `json:"publish"`
	Subscribe string `json:"subscribe"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// AddDevice 校验
type AddDevice struct {
	Token     string `json:"token"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Publish   string `json:"publish"`
	Subscribe string `json:"subscribe"`
	Username  string `json:"username"`
	Password  string `json:"password"`
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
	Values    interface{} `json:"values" alias:"values" valid:"Required"`
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
	Extension      string `json:"extension" gorm:"size:50"` // 插件( 目录名)
	Protocol       string `json:"protocol" gorm:"size:50"`
	Port           string `json:"port" gorm:"size:50"`
	Publish        string `json:"publish" gorm:"size:255"`
	Subscribe      string `json:"subscribe" gorm:"size:255"`
	Username       string `json:"username" gorm:"size:255"`
	Password       string `json:"password" gorm:"size:255"`
}
