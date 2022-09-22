package models

type ObjectModel struct {
	Id             string `json:"id" gorm:"primaryKey,size:36"`
	Sort           int64  `json:"sort,omitempty"`
	ObjectDescribe string `json:"object_describe,omitempty" gorm:"size:255"`
	ObjectName     string `json:"object_name,omitempty" gorm:"size:99"`       // 物模型名称
	ObjectType     string `json:"object_type,omitempty" gorm:"size:36"`       // 物模型类型
	ObjectData     string `json:"object_data,omitempty" gorm:"type:longtext"` // 物模型json
	CreatedAt      int64  `json:"created_at,omitempty"`
	Remark         string `json:"remark,omitempty" gorm:"size:255"`
}

func (ObjectModel) TableName() string {
	return "object_model"
}
