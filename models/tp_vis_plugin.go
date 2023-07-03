package models

type TpVisPlugin struct {
	Id                string `json:"id" gorm:"primaryKey"`
	PluginName        string `json:"plugin_name,omitempty"`
	PluginDescription string `json:"plugin_description"`
	CreatedAt         int64  `gorm:"column:create_at" json:"create_at,omitempty"`
	TenantId          string `json:"tenant_id,omitempty" gorm:"size:36"` // 租户id
}

func (TpVisPlugin) TableName() string {
	return "tp_vis_plugin"
}

type TpVisFiles struct {
	Id          string `json:"id" gorm:"primaryKey"`
	VisPluginId string `json:"vis_plugin_id,omitempty"`
	FileName    string `json:"file_name,omitempty"`
	FileUrl     string `json:"file_url,omitempty"`
	FileSize    string `json:"file_size,omitempty"`
	CreatedAt   int64  `gorm:"column:create_at" json:"create_at,omitempty"`
}

func (TpVisFiles) TableName() string {
	return "tp_vis_files"
}
