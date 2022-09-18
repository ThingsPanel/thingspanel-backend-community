package models

type Device struct {
	ID             string `json:"id" gorm:"primaryKey,size:36"`
	AssetID        string `json:"asset_id,omitempty" gorm:"size:36"`              // 资产id
	Token          string `json:"token,omitempty"`                                // 安全key
	AdditionalInfo string `json:"additional_info,omitempty" gorm:"type:longtext"` // 存储基本配置
	CustomerID     string `json:"customer_id" gorm:"size:36"`
	Type           string `json:"type,omitempty"` // 插件类型
	Name           string `json:"name,omitempty"` // 插件名
	Label          string `json:"label,omitempty"`
	SearchText     string `json:"search_text,omitempty"`
	Extension      string `json:"extension,omitempty" gorm:"size:50"` // 插件( 目录名)
	Protocol       string `json:"protocol,omitempty" gorm:"size:50"`
	Port           string `json:"port,omitempty" gorm:"size:50"`
	Publish        string `json:"publish,omitempty" gorm:"size:255"`
	Subscribe      string `json:"subscribe,omitempty" gorm:"size:255"`
	Username       string `json:"username,omitempty" gorm:"size:255"`
	Password       string `json:"password,omitempty" gorm:"size:255"`
	DId            string `json:"d_id,omitempty" gorm:"size:255"`
	Location       string `json:"location,omitempty" gorm:"size:255"`
}

func (Device) TableName() string {
	return "device"
}
