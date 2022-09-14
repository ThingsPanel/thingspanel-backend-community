package models

type TpDict struct {
	ID        string `json:"id" gorm:"primaryKey,size:36"`
	DictCode  string `json:"dict_code,omitempty" gorm:"size:36"`
	DictValue string `json:"dict_value,omitempty" gorm:"size:99"`
	Describe  string `json:"describe,omitempty" gorm:"size:99"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

func (TpDict) TableName() string {
	return "tp_dict"
}
