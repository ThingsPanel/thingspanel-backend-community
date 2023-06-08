package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	valid "ThingsPanel-Go/validate"
	"time"

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

//获取列表
func (*TpOtaTaskService) GetTpOtaTaskList(PaginationValidate valid.TpOtaTaskPaginationValidate) (bool, []models.TpOtaTask, int64) {
	var TpOtaTasks []models.TpOtaTask
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.TpOtaTask{})
	if PaginationValidate.OtaId != "" {
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
	// 判断升级状态
	for i, tpOtaTask := range TpOtaTasks {
		// 获取当前时间并格式化为2006-01-02 15:04:05
		nowTime := time.Now().Format("2006-01-02 15:04:05")
		// 如果升级时间类型是1-定时升级
		if tpOtaTask.UpgradeTimeType == "1" {
			// 如果开始时间大于当前时间，说明还未开始升级
			if tpOtaTask.StartTime > nowTime {
				TpOtaTasks[i].TaskStatus = "0"
				continue
			} else if tpOtaTask.EndTime < nowTime {
				TpOtaTasks[i].TaskStatus = "2"
				continue
			}
		}
		// 查询升级任务下的设备不是升级成功或者失败或已取消状态的数量
		var count int64
		if err := psql.Mydb.Model(&models.TpOtaDevice{}).Where("ota_task_id = ? and upgrade_status not in ('3','4','5')", tpOtaTask.Id).Count(&count).Error; err != nil {
			logs.Error(err)
			// 设置升级状态为空
			TpOtaTasks[i].TaskStatus = ""
			continue
		} else {
			// 如果count大于0，说明还有设备没有升级完成
			logs.Info("count:", count)
			if count == int64(0) {
				TpOtaTasks[i].TaskStatus = "2"
			} else {
				TpOtaTasks[i].TaskStatus = "1"
			}
		}

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
	result = psql.Mydb.Where("ota_task_id = ?", tp_ota_task.Id).Delete(&models.TpOtaDevice{})
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}
	return nil
}
