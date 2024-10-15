package dal

import (
	"context"
	"errors"
	model "project/internal/model"
	query "project/internal/query"
	"time"

	"github.com/sirupsen/logrus"
)

type ExpectedDataDal struct {
}

// 创建预期数据
func (d ExpectedDataDal) Create(ctx context.Context, data *model.ExpectedData) (err error) {
	err = query.ExpectedData.WithContext(ctx).Create(data)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 删除预期数据
func (d ExpectedDataDal) Delete(ctx context.Context, id string) error {
	info, err := query.ExpectedData.WithContext(ctx).Where(query.ExpectedData.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(ctx, err)
		return err
	}
	if info.RowsAffected == 0 {
		return errors.New("no data")

	}
	return nil
}

// 详情查询
func (d ExpectedDataDal) GetByID(ctx context.Context, id string) (data *model.ExpectedData, err error) {
	data, err = query.ExpectedData.WithContext(ctx).Where(query.ExpectedData.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 分页查询
func (d ExpectedDataDal) PageList(ctx context.Context, req *model.GetExpectedDataPageReq, tenantID string) (total int64, list []map[string]interface{}, err error) {
	ed := query.ExpectedData
	queryBuilder := ed.WithContext(ctx)
	queryBuilder = queryBuilder.Where(ed.TenantID.Eq(tenantID), ed.DeviceID.Eq(req.DeviceID))

	if req.Label != nil {
		queryBuilder = queryBuilder.Where(ed.Label.Eq(*req.Label))
	}
	if req.SendType != nil {
		queryBuilder = queryBuilder.Where(ed.SendType.Eq(*req.SendType))
	}
	if req.Status != nil {
		queryBuilder = queryBuilder.Where(ed.Status.Eq(*req.Status))
	}

	// 总数
	total, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(ctx, err)
		return
	}

	// 分页
	if req.Page > 0 && req.PageSize > 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize).Order(ed.CreatedAt.Desc())
	}
	queryBuilder = queryBuilder.Select(ed.ALL)
	err = queryBuilder.Scan(&list)
	if err != nil {
		logrus.Error(ctx, err)
		return
	}
	if len(list) == 0 {
		list = []map[string]interface{}{}
	}
	return
}

// 根据设备ID获取全部未处理的预期数据
func (d ExpectedDataDal) GetAllByDeviceID(ctx context.Context, deviceID string) (list []*model.ExpectedData, err error) {
	ed := query.ExpectedData
	queryBuilder := ed.WithContext(ctx)
	queryBuilder = queryBuilder.Where(ed.DeviceID.Eq(deviceID))
	queryBuilder = queryBuilder.Where(ed.Status.Eq("pending"))
	queryBuilder = queryBuilder.Select(ed.ALL)
	list, err = queryBuilder.Find()
	if err != nil {
		logrus.Error(ctx, err)
		return
	}
	if len(list) == 0 {
		list = []*model.ExpectedData{}
	}
	return
}

// 更新状态
func (d ExpectedDataDal) UpdateStatus(ctx context.Context, id string, status string, message *string, sendTime *time.Time) error {
	expectedData := model.ExpectedData{Status: status, Message: message, SendTime: sendTime}
	info, err := query.ExpectedData.WithContext(ctx).Where(query.ExpectedData.ID.Eq(id)).Updates(expectedData)
	if err != nil {
		logrus.Error(ctx, err)
		return err
	}
	if info.RowsAffected == 0 {
		return errors.New("no data")
	}
	return nil
}
