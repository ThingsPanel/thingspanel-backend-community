package models

type DeviceModel struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	ModelName string `json:"model_name,omitempty" gorm:"size:99"`
	Flag      int64  `json:"flag"`
	ChartData string `json:"chart_data,omitempty" gorm:"type:longtext"`
	ModelType string `json:"model_type,omitempty" gorm:"size:36"`
	Describe  string `json:"describe,omitempty" gorm:"size:255"`
	Version   string `json:"version,omitempty" gorm:"size:36"`
	Author    string `json:"author,omitempty" gorm:"size:36"`
	Sort      int64  `json:"sort"`
	Issued    int64  `json:"issued"`
	Remark    string `json:"remark,omitempty" gorm:"size:255"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

func (DeviceModel) TableName() string {
	return "device_model"
}
