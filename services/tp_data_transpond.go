package services

import (
	"ThingsPanel-Go/models"
)

type TpDataTranspondService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpDataTranspondService) AddTpDataTranspond(data models.TpDataTranspon) {

}
