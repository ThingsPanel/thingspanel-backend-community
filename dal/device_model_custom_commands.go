package dal

import (
	"context"
	"fmt"
	"project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateDeviceModelCustomCommand(data *model.DeviceModelCustomCommand) error {
	return query.DeviceModelCustomCommand.Create(data)
}

func UpdateDeviceModelCustomCommand(data *model.DeviceModelCustomCommand) (*model.DeviceModelCustomCommand, error) {
	info, err := query.DeviceModelCustomCommand.Where(query.DeviceModelCustomCommand.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		return nil, err
	} else if info.RowsAffected == 0 {
		return nil, fmt.Errorf("update device model custom command failed, no rows affected")
	}
	return data, err
}

func DeleteDeviceModelCustomCommandById(id string) error {
	info, err := query.DeviceModelCustomCommand.Where(query.DeviceModelCustomCommand.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}

	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}

	return err

}

func GetDeviceModelCustomCommandsByPage(page model.GetDeviceModelListByPageReq, tenantID string) (int64, []*model.DeviceModelCustomCommand, error) {
	var count int64
	q := query.DeviceModelCustomCommand
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

	data, err := queryBuilder.Select(q.ALL).Find()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}

	return count, data, nil

}

func GetDeviceModelCustomCommandsByDeviceId(deviceId, tenantId string) ([]*model.DeviceModelCustomCommand, error) {
	d, err := GetDeviceByID(deviceId)
	if err != nil {
		return nil, err
	}
	if d.DeviceConfigID == nil {
		return nil, nil
	}
	dc, err := GetDeviceConfigByID(*d.DeviceConfigID)
	if err != nil {
		return nil, err
	}
	if dc.DeviceTemplateID == nil {
		return nil, nil
	}
	data, err := query.DeviceModelCustomCommand.
		Where(query.DeviceModelCustomCommand.DeviceTemplateID.Eq(*dc.DeviceTemplateID)).
		Where(query.DeviceModelCustomCommand.TenantID.Eq(tenantId)).
		Find()
	if err != nil {
		logrus.Error(err)
	}

	return data, nil
}
