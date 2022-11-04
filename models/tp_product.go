package models

type TpProduct struct {
	Id           string `json:"id"  gorm:"primaryKey"`
	Name         string `json:"name,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	ProtocolType string `json:"protocol_type,omitempty"`
	AuthType     string `json:"auth_type,omitempty"`
	Plugin       string `json:"plugin,omitempty"`
	Describe     string `json:"describe,omitempty"`
	CreatedTime  int64  `json:"created_time,omitempty"`
	Remark       string `json:"remark,omitempty"`
}

func (TpProduct) TableName() string {
	return "tp_product"
}
