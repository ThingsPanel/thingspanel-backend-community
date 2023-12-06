package models

type TpTenantConfig struct {
	TenantId     string `json:"tenant_id"  gorm:"primaryKey"`
	CustomConfig string `json:"custom_config"`
	SYSConfig    string `json:"sys_config,omitempty"`
	Remark       string `json:"remark,omitempty"`
}

func (TpTenantConfig) TableName() string {
	return "tp_tenant_config"
}

type TpTenantAIConfig struct {
	ModelType string `json:"model_type"`
	APIKey    string `json:"api_key"`
	BashURL   string `json:"bash_url"`
	WhiteList string `json:"white_list"`
	UpdateAt  int64  `json:"update_at"`
}
