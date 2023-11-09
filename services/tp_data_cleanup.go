package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"time"

	"github.com/beego/beego/v2/core/logs"
)

type TpDataCleanupService struct {
}

func (*TpDataCleanupService) GetTpDataCleanupDetail() ([]models.TpDataCleanup, error) {
	var data []models.TpDataCleanup
	result := psql.Mydb.Model(&models.TpDataCleanup{}).Find(&data)
	return data, result.Error
}

func (*TpDataCleanupService) EditTpDataCleanup(id string, retentionDays int, remark string) error {
	result := psql.Mydb.
		Model(&models.TpDataCleanup{}).
		Omit("cleanup_type", "last_cleanup_time", "last_cleanup_data_time").
		Where("id = ?", id).
		Updates(map[string]interface{}{"retention_days": retentionDays, "remark": remark})
	return result.Error
}

func (c *TpDataCleanupService) ExecuteTpDataCleanup() error {
	data, err := c.GetTpDataCleanupDetail()
	if err != nil {
		return err
	}
	for _, v := range data {
		now := time.Now().Unix()
		// 判断今天是否清理过
		if utils.IsToday(now) {
			continue
		}
		if v.CleanupType == 1 {
			// 清理 ts_kv（微秒）
			ts := time.Now().AddDate(0, 0, -v.RetentionDays).UnixNano() / 1000
			err := psql.Mydb.Model(&models.TSKV{}).Where("ts < ?", ts).Delete(&models.TSKV{}).Error
			if err != nil {
				logs.Error("删除ts_kv表中的数据失败", err.Error())
			} else {
				// 保存清理结果
				err = psql.Mydb.Model(&models.TpDataCleanup{}).Where("id = ?", v.Id).Updates(map[string]interface{}{"last_cleanup_time": now, "last_cleanup_data_time": ts}).Error
				if err != nil {
					logs.Error("保存清理结果失败", err.Error())
				}
			}

		} else if v.CleanupType == 2 {
			//  清理 operation_log（秒）
			ts := time.Now().AddDate(0, 0, -v.RetentionDays).Unix()
			err = psql.Mydb.Model(&models.OperationLog{}).Where("created_at < ?", ts).Delete(&models.OperationLog{}).Error
			if err != nil {
				logs.Error("删除operation_log表中的数据失败", err.Error())
			} else {
				err = psql.Mydb.Model(&models.TpDataCleanup{}).Where("id = ?", v.Id).Updates(map[string]interface{}{"last_cleanup_time": now, "last_cleanup_data_time": ts}).Error
				if err != nil {
					logs.Error("保存清理结果失败", err.Error())
				}
			}
		} else {
			logs.Error("暂不支持的清理类型")
		}
	}
	return nil
}
