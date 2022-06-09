package models

type TpFunction struct {
	Id           string `json:"id" gorm:"primaryKey"`         // ID
	FunctionName string `json:"function_name" gorm:"size:99"` // 功能名称
}

func (TpFunction) TableName() string {
	return "tp_function"
}
