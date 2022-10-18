package models

type TpScript struct {
	Id             string `json:"id"  gorm:"primaryKey"`
	ProtocolType   string `json:"protocol_type,omitempty"`
	ScriptName     string `json:"script_name"`
	Company        string `json:"company,omitempty"`
	ProductName    string `json:"product_name,omitempty"`
	ScriptContentA string `json:"script_content_a,omitempty"`
	ScriptContentB string `json:"script_content_b,omitempty"`
	CreatedAt      int64  `json:"created_at,omitempty"`
	ScriptType     string `json:"script_type,omitempty"`
	Remark         string `json:"remark,omitempty"`
}

func (TpScript) TableName() string {
	return "tp_script"
}
