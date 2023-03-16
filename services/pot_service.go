package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type PotTypeService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*PotTypeService) GetPotTypeDetail(tp_product_id string) []models.PotType {
	var pot_type []models.PotType
	psql.Mydb.First(&pot_type, "id = ?", tp_product_id)
	return pot_type
}

// 获取列表
func (*PotTypeService) GetPotTypeList(PaginationValidate valid.TpProductPaginationValidate) (bool, []models.PotType, int64) {
	var PotType []models.PotType
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.PotType{})
	if PaginationValidate.Name != "" {
		db.Where("name like ?", "%"+PaginationValidate.Name+"%")
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("create_at desc").Find(&PotType)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, PotType, 0
	}
	return true, PotType, count
}

// 新增数据
func (*PotTypeService) AddPotType(pot models.PotType) (error, models.PotType) {
	var uuid = uuid.GetUuid()
	pot.Id = uuid
	result := psql.Mydb.Create(&pot)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return result.Error, pot
	}
	return nil, pot
}

// 修改数据
func (*PotTypeService) EditPotType(pot valid.PotType) bool {
	result := psql.Mydb.Model(&models.PotType{}).Where("id = ?", pot.Id).Updates(&pot)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*PotTypeService) DeletePotType(pot models.PotType) error {
	result := psql.Mydb.Delete(&pot)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
