package models

type Logo struct {
	Id             string `json:"id" gorm:"primaryKey"`        // ID
	SystemName     string `json:"system_name" gorm:"size:255"` // 系统名称
	Theme          string `json:"theme" gorm:"size:99"`        // 主题
	LogoOne        string `json:"logo_one" gorm:"size:255"`    // 首页logo
	LogoTwo        string `json:"logo_two" gorm:"size:255"`    // 缓冲logo
	LogoThree      string `json:"logo_three"  gorm:"size:255"`
	HomeBackground string `json:"home_background" gorm:"size:255"`
	Remark         string `json:"remark" gorm:"size:255"`
}

func (Logo) TableName() string {
	return "logo"
}
