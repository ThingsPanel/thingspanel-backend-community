package models

type TpGenerateDevice struct {
	Id          string `json:"id"  gorm:"primaryKey"`
	BatchId     string `json:"batch_id,omitempty"`
	Token       string `json:"token,omitempty"`
	Password    string `json:"password,omitempty"`
	AddFlag     string `json:"add_flag,omitempty"`
	AddDate     string `json:"add_date,omitempty"`
	DeviceId    string `json:"device_id,omitempty"`
	CreatedTime int64  `json:"created_time,omitempty"`
	Remark      string `json:"remark,omitempty"`
	DeviceCode  string `json:"device_code,omitempty"`
}

func (TpGenerateDevice) TableName() string {
	return "tp_generate_device"
}
