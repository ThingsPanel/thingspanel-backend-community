package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"

	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type TpOtaTaskService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpOtaTaskService) GetTpOtaTaskList(PaginationValidate valid.TpOtaTaskPaginationValidate) (bool, []models.TpOtaTask, int64) {
	var TpOtaTasks []models.TpOtaTask
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpOtaTask{})
	if PaginationValidate.Id != "" {
		db.Where("ota_id = ?", PaginationValidate.OtaId)
	}
	if PaginationValidate.Id != "" {
		db.Where("id = ?", PaginationValidate.Id)
	}
	var count int64
	db.Count(&count)
	result := db.Limit(PaginationValidate.PerPage).Offset(offset).Order("created_at desc").Find(&TpOtaTasks)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, TpOtaTasks, 0
	}
	return true, TpOtaTasks, count
}

// 新增数据
func (*TpOtaTaskService) AddTpOtaTask(tp_ota_task models.TpOtaTask) (models.TpOtaTask, error) {
	result := psql.Mydb.Create(&tp_ota_task)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return tp_ota_task, result.Error
	}
	return tp_ota_task, nil
}
func (*TpOtaTaskService) DeleteTpOtaTask(tp_ota_task models.TpOtaTask) error {
	result := psql.Mydb.Delete(&tp_ota_task)
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
