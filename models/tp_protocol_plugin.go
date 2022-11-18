package models

type TpProtocolPlugin struct {
	Id             string `json:"id"  gorm:"primaryKey"`
	Name           string `json:"name,omitempty"`
	ProtocolType   string `json:"protocol_type,omitempty"`
	AccessAddress  string `json:"access_address,omitempty"`
	HttpAddress    string `json:"http_address,omitempty"`
	SubTopicPrefix string `json:"sub_topic_prefix,omitempty"`
	CreatedAt      int64  `json:"created_at,omitempty"`
	Description    string `json:"description,omitempty"`
}

func (TpProtocolPlugin) TableName() string {
	return "tp_protocol_plugin"
}
