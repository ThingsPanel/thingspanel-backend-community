package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type SoupDataService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*SoupDataService) GetList(PaginationValidate valid.SoupDataPaginationValidate) (bool, []models.AddSoupData, int64) {
	var AddSoupData []models.AddSoupData
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.PotType{}).Where("is_del", false)
	if PaginationValidate.ShopName != "" {
		db.Where("name like ?", "%"+PaginationValidate.ShopName+"%")
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("create_at desc").Find(&AddSoupData)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, AddSoupData, 0
	}
	return true, AddSoupData, count
}


