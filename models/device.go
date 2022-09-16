package models

type Device struct {
	ID             string `json:"id" gorm:"primaryKey,size:36"`
	AssetID        string `json:"asset_id" gorm:"size:36"`              // 资产id
	Token          string `json:"token,omitempty"`                      // 安全key
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
	DId            string `json:"d_id" gorm:"size:255"`
	Location       string `json:"location" gorm:"size:255"`
}

func (Device) TableName() string {
	return "device"
}
