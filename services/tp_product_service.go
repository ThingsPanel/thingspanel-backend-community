package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpProductService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpProductService) GetTpProductDetail(tp_product_id string) []models.TpProduct {
	var tp_product []models.TpProduct
	psql.Mydb.First(&tp_product, "id = ?", tp_product_id)
	return tp_product
}

// 获取列表
func (*TpProductService) GetTpProductList(PaginationValidate valid.TpProductPaginationValidate) (bool, []models.TpProduct, int64) {
	var TpProducts []models.TpProduct
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=?"
	var params []interface{}
	params = append(params, 1)
	if PaginationValidate.SerialNumber != "" {
		sqlWhere += " and serial_number like ?"
		params = append(params, PaginationValidate.SerialNumber)
	}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = ?"
		params = append(params, PaginationValidate.Id)
	}
	var count int64
	psql.Mydb.Model(&models.TpProduct{}).Where(sqlWhere, params...).Count(&count)
	result := psql.Mydb.Model(&models.TpProduct{}).Where(sqlWhere, params...).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_time desc").Find(&TpProducts)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpProducts, 0
	}
	return true, TpProducts, count
}

// 新增数据
func (*TpProductService) AddTpProduct(tp_product models.TpProduct) (error, models.TpProduct) {
	var uuid = uuid.GetUuid()
	tp_product.Id = uuid
	result := psql.Mydb.Create(&tp_product)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error, tp_product
	}
	return nil, tp_product
}

// 修改数据
func (*TpProductService) EditTpProduct(tp_product valid.TpProductValidate) bool {
	result := psql.Mydb.Model(&models.TpProduct{}).Where("id = ?", tp_product.Id).Updates(&tp_product)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpProductService) DeleteTpProduct(tp_product models.TpProduct) error {
	result := psql.Mydb.Delete(&tp_product)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
