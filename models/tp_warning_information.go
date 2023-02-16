package models

type TpWarningInformation struct {
	Id                     string `json:"id" gorm:"primaryKey"`
	TenantId               string `json:"tenant_id,omitempty"`
	WarningName            string `json:"warning_name,omitempty"`
	WarningLevel           string `json:"warning_level,omitempty"`           // 告警级别
	WarningDescription     string `json:"warning_description,omitempty"`     // 告警描述
	WarningContent         string `json:"warning_content,omitempty"`         // 告警内容
	ProcessingResult       string `json:"processing_result,omitempty"`       // 处理结果 0-未处理 1-已处理 2-已忽略
	ProcessingInstructions string `json:"processing_instructions,omitempty"` // 处理说明
	ProcessingTime         string `json:"processing_time,omitempty"`         // 处理时间
	ProcessingPeopleId     string `json:"processing_people_id,omitempty"`    // 处理人
	CreatedAt              int64  `json:"created_at,omitempty"`
	Remark                 string `json:"remark,omitempty"` // 备注
}

func (t *TpWarningInformation) TableName() string {
	return "tp_warning_information"
}
