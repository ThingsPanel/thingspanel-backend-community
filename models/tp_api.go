package models

type TpApi struct {
	ID          string `json:"id" gorm:"primarykey"`
	Name        string `json:"name" gorm:"size:50"`        //名称
	Url         string `json:"url" gorm:"size:500"`        // 客户ID
	ApiType     string `json:"api_type" gorm:"size:20"`    //接口类型 http socket
	ServiceType string `json:"service_type" gorm:"size:2"` //服务类型 1-OpenApi
	Remark      string `json:"remark" gorm:"size:255"`     //备注
	IsAdd       string `json:"isAdd"`                      //备注
}

func (t *TpApi) TableName() string {
	return "tp_api"
}
