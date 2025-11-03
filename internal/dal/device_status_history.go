package dal

import (
	"context"
	"time"

	"project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

// GetDeviceStatusHistoryByPage 获取设备状态历史记录（分页）
func GetDeviceStatusHistoryByPage(req *model.GetDeviceStatusHistoryReq, tenantID string) (int64, []*model.DeviceStatusHistory, error) {
	q := query.DeviceStatusHistory
	var count int64
	var list []*model.DeviceStatusHistory

	queryBuilder := q.WithContext(context.Background())

	// 租户过滤
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))

	// 设备ID过滤（必填）
	queryBuilder = queryBuilder.Where(q.DeviceID.Eq(req.DeviceID))

	// 时间范围过滤
	if req.StartTime != nil && req.EndTime != nil {
		startTime := time.Unix(*req.StartTime, 0)
		endTime := time.Unix(*req.EndTime, 0)
		queryBuilder = queryBuilder.Where(q.ChangeTime.Between(startTime, endTime))
	} else if req.StartTime != nil {
		startTime := time.Unix(*req.StartTime, 0)
		queryBuilder = queryBuilder.Where(q.ChangeTime.Gte(startTime))
	} else if req.EndTime != nil {
		endTime := time.Unix(*req.EndTime, 0)
		queryBuilder = queryBuilder.Where(q.ChangeTime.Lte(endTime))
	}

	// 状态筛选
	if req.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*req.Status))
	}

	// 查询总数
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	// 分页
	if req.Page > 0 && req.PageSize > 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	// 按时间降序排列（最新的在前）
	list, err = queryBuilder.Select().Order(q.ChangeTime.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return count, list, err
	}

	return count, list, nil
}

// SaveDeviceStatusHistory 保存设备状态历史记录
func SaveDeviceStatusHistory(tenantID, deviceID string, status int16) error {
	q := query.DeviceStatusHistory

	history := &model.DeviceStatusHistory{
		TenantID:   tenantID,
		DeviceID:   deviceID,
		Status:     status,
		ChangeTime: time.Now().UTC(),
	}

	err := q.WithContext(context.Background()).Create(history)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"tenant_id": tenantID,
			"device_id": deviceID,
			"status":    status,
		}).Error("Failed to save device status history")
		return err
	}

	return nil
}
