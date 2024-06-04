package service

import (
	dal "project/dal"
	model "project/model"
	"project/utils"
	"time"

	"github.com/sirupsen/logrus"
)

type DataPolicy struct{}

func (p *DataPolicy) UpdateDataPolicy(UpdateDataPolicyReq *model.UpdateDataPolicyReq) error {
	var datapolicy = model.DataPolicy{}
	datapolicy.ID = UpdateDataPolicyReq.Id
	datapolicy.RetentionDay = UpdateDataPolicyReq.RetentionDays
	datapolicy.Enabled = UpdateDataPolicyReq.Enabled
	datapolicy.Remark = UpdateDataPolicyReq.Remark
	err := dal.UpdateDataPolicy(&datapolicy)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (p *DataPolicy) GetDataPolicyListByPage(Params *model.GetDataPolicyListByPageReq) (map[string]interface{}, error) {

	total, list, err := dal.GetDataPolicyListByPage(Params)
	if err != nil {
		return nil, err
	}
	datapolicyListRsp := make(map[string]interface{})
	datapolicyListRsp["total"] = total
	datapolicyListRsp["list"] = list

	return datapolicyListRsp, err
}

func (p *DataPolicy) CleanSystemDataByCron() error {
	data, err := dal.GetDataPolicy()
	if err != nil {
		return err
	}

	now := time.Now()
	for _, v := range data {
		// 判断是否开启（1-启用 2-停止）
		if v.Enabled == "2" {
			continue
		}

		// 判断今天是否清理过
		if utils.IsToday(*v.LastCleanupTime) {
			continue
		}

		if v.DataType == "1" {
			daysAgeInt64 := utils.MillisecondsTimestampDaysAgo(int(v.RetentionDay))
			daysAgeTime := utils.DaysAgo(int(v.RetentionDay))
			err := dal.DeleteTelemetrDataByTime(daysAgeInt64)
			if err != nil {
				return err
			}
			// 更新数据库
			var datapolicy = model.DataPolicy{}
			datapolicy.ID = v.ID
			datapolicy.LastCleanupTime = &now
			datapolicy.LastCleanupDataTime = &daysAgeTime
			err = dal.UpdateDataPolicy(&datapolicy)
			if err != nil {
				return err
			}

		} else if v.DataType == "2" {
			// 清理操作日志（operation_logs）
			// 清理x天前的内容
			daysAge := utils.DaysAgo(int(v.RetentionDay))
			err := dal.DeleteOperationLogsByTime(daysAge)
			if err != nil {
				return err
			}
			// 更新数据库
			var datapolicy = model.DataPolicy{}
			datapolicy.ID = v.ID
			datapolicy.LastCleanupTime = &now
			datapolicy.LastCleanupDataTime = &daysAge
			err = dal.UpdateDataPolicy(&datapolicy)
			if err != nil {
				return err
			}
		}

	}
	return nil
}
