package models

type TpBatch struct {
	Id            string `json:"id"  gorm:"primaryKey"`
	BatchNumber   string `json:"batch_number,omitempty"`
	ProductId     string `json:"product_id,omitempty"`
	DeviceNumber  int    `json:"device_number,omitempty"`
	GenerateFlag  string `json:"generate_flag,omitempty"`
	Describle     string `json:"describle,omitempty"`
	CreatedTime   int64  `json:"created_time,omitempty"`
	Remark        string `json:"remark,omitempty"`
	AccessAddress string `json:"access_address,omitempty"`
}

func (TpBatch) TableName() string {
	return "tp_batch"
}
