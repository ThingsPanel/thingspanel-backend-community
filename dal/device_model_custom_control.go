package dal

import (
	"context"
	"fmt"
	model "project/internal/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateDeviceModelCustomControl(data *model.DeviceModelCustomControl) error {
	return query.DeviceModelCustomControl.Create(data)
}

func DeleteDeviceModelCustomControlById(id string) error {
	info, err := query.DeviceModelCustomControl.Where(query.DeviceModelCustomControl.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}

	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}

	return err

}

func UpdateDeviceModelCustomControl(data *model.DeviceModelCustomControl) (*model.DeviceModelCustomControl, error) {
	info, err := query.DeviceModelCustomControl.Where(query.DeviceModelCustomControl.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		return nil, err
	} else if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device model custom control failed, no rows affected")
	}
	return data, err
}

func GetDeviceModelCustomControlByPage(page model.GetDeviceModelListByPageReq, tenantID string) (int64, []*model.DeviceModelCustomControl, error) {
	var count int64
	q := query.DeviceModelCustomControl
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(page.DeviceTemplateId))
	if page.EnableStatus != nil {
		queryBuilder = queryBuilder.Where(q.EnableStatus.Eq(*page.EnableStatus))
	}
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	if page.Page != 0 && page.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(page.PageSize)
		queryBuilder = queryBuilder.Offset((page.Page - 1) * page.PageSize)
	}

	data, err := queryBuilder.Select(q.ALL).Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}

	return count, data, nil

}
