package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpBatchService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpBatchService) GetTpBatchDetail(tp_batch_id string) []models.TpBatch {
	var tp_batch []models.TpBatch
	psql.Mydb.First(&tp_batch, "id = ?", tp_batch_id)
	return tp_batch
}

// 获取列表
func (*TpBatchService) GetTpBatchList(PaginationValidate valid.TpBatchPaginationValidate) (bool, []models.TpBatch, int64) {
	var TpBatchs []models.TpBatch
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	sqlWhere := "1=1"
	if PaginationValidate.BatchNumber != "" {
		sqlWhere += " and batch_number like '" + PaginationValidate.BatchNumber + "'"
	}
	if PaginationValidate.Id != "" {
		sqlWhere += " and id = '" + PaginationValidate.Id + "'"
	}
	var count int64
	psql.Mydb.Model(&models.TpBatch{}).Where(sqlWhere).Count(&count)
	result := psql.Mydb.Model(&models.TpBatch{}).Where(sqlWhere).Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpBatchs)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpBatchs, 0
	}
	return true, TpBatchs, count
}

// 新增数据
func (*TpBatchService) AddTpBatch(tp_batch models.TpBatch) (models.TpBatch, error) {
	var uuid = uuid.GetUuid()
	tp_batch.Id = uuid
	result := psql.Mydb.Create(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_batch, result.Error
	}
	return tp_batch, nil
}

// 修改数据
func (*TpBatchService) EditTpBatch(tp_batch valid.TpBatchValidate) bool {
	result := psql.Mydb.Model(&models.TpBatch{}).Where("id = ?", tp_batch.Id).Updates(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// 删除数据
func (*TpBatchService) DeleteTpBatch(tp_batch models.TpBatch) error {
	result := psql.Mydb.Delete(&tp_batch)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
