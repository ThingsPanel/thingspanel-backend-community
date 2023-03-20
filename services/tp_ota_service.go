package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"errors"

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

//获取列表
func (*TpOtaService) GetTpOtaList(PaginationValidate valid.TpOtaPaginationValidate) (bool, []map[string]interface{}, int64) {
	sqlWhere := `select o.*,p.name from tp_ota o left join tp_product p on o.product_id=p.id where 1=1 `
	sqlWhereCount := `select count(1) from tp_ota o left join tp_product p on o.product_id=p.id where 1=1`
	var values []interface{}
	var where = ""
	if PaginationValidate.PackageName != "" {
		values = append(values, "%"+PaginationValidate.PackageName+"%")
		where += " and o.package_name like ?"
	}
	if PaginationValidate.Id != "" {
		values = append(values, PaginationValidate.Id)
		where += " and o.id = ?"
	}
	if PaginationValidate.ProductId != "" {
		values = append(values, PaginationValidate.ProductId)
		where += " and o.product_id = ?"
	}
	sqlWhere += where
	sqlWhereCount += where
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	var offset int = (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	var limit int = PaginationValidate.PerPage
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var otaList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&otaList)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	}
	return true, otaList, count
}

//根据id获取升级包信息
func (*TpOtaService) GetTpOtaVersionById(otaid string) (bool, models.TpOta) {
	var TpOtas models.TpOta
	result := psql.Mydb.Model(&models.TpOta{}).Where("id=?", otaid).Find(&TpOtas)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtas
	}
	return true, TpOtas
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
