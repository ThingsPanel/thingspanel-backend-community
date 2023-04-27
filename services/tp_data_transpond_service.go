package services

import (
	"ThingsPanel-Go/initialize/psql"
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

// 新建转发规则
func (*TpDataTranspondService) AddTpDataTranspond(
	dataTranspond models.TpDataTranspon,
	dataTranspondDetail []models.TpDataTransponDetail,
	dataTranspondTarget []models.TpDataTransponTarget,
) bool {

	// fmt.Println(dataTranspond, dataTranspondDetail, dataTranspondTarget)

	// 启动事物

	psql.Mydb.Create(&dataTranspond)
	psql.Mydb.Create(&dataTranspondDetail)
	psql.Mydb.Create(&dataTranspondTarget)

	return true
}
