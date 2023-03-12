package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpOtaService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpOtaService) GetTpOtaList(PaginationValidate valid.TpOtaPaginationValidate) (bool, []models.TpOta, int64) {
	var TpOtas []models.TpOta
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpOta{})
	if PaginationValidate.ProductId != "" {
		db.Where("product_id = ?", PaginationValidate.ProductId)
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	if PaginationValidate.PackageName != "" {
		db.Where("package_name like ?", "%"+PaginationValidate.PackageName+"%")
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpOtas)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtas, 0
	}
	return true, TpOtas, count
}

// 新增数据
func (*TpOtaService) AddTpOta(tp_ota models.TpOta) (models.TpOta, error) {
	result := psql.Mydb.Create(&tp_ota)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota, result.Error
	}
	return tp_ota, nil
}
func (*TpOtaService) DeleteTpOta(tp_ota models.TpOta) error {
	result := psql.Mydb.Delete(&tp_ota)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
