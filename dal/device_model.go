package dal

import (
	"context"
	"project/internal/model"
	"project/query"

	"github.com/sirupsen/logrus"
)

// create
func CreateDeviceModelTelemetry(d *model.DeviceModelTelemetry) (err error) {
	return query.DeviceModelTelemetry.Create(d)
}

func CreateDeviceModelAttribute(d *model.DeviceModelAttribute) (err error) {
	return query.DeviceModelAttribute.Create(d)
}

func CreateDeviceModelEvent(d *model.DeviceModelEvent) (err error) {
	return query.DeviceModelEvent.Create(d)
}

func CreateDeviceModelCommand(d *model.DeviceModelCommand) (err error) {
	return query.DeviceModelCommand.Create(d)
}

// delete
func DeleteDeviceModelTelemetry(id string) (err error) {
	r, err := query.DeviceModelTelemetry.Where(query.DeviceModelTelemetry.ID.Eq(id)).Delete()
	if r.RowsAffected == 0 {
		return nil
	}
	return err
}

func DeleteDeviceModelAttribute(id string) (err error) {
	r, err := query.DeviceModelAttribute.Where(query.DeviceModelAttribute.ID.Eq(id)).Delete()
	if r.RowsAffected == 0 {
		return nil
	}
	return err
}

func DeleteDeviceModelEvent(id string) (err error) {
	r, err := query.DeviceModelEvent.Where(query.DeviceModelEvent.ID.Eq(id)).Delete()
	if r.RowsAffected == 0 {
		return nil
	}
	return err
}

func DeleteDeviceModelCommand(id string) (err error) {
	r, err := query.DeviceModelCommand.Where(query.DeviceModelCommand.ID.Eq(id)).Delete()
	if r.RowsAffected == 0 {
		return nil
	}
	return err
}

// update
func UpdateDeviceModelTelemetry(d *model.DeviceModelTelemetry) (err error) {
	p := query.DeviceModelTelemetry
	r, err := query.DeviceModelTelemetry.Where(p.ID.Eq(d.ID)).Updates(d)
	if r.RowsAffected == 0 {
		return nil
	} else {
		return err
	}
}

func UpdateDeviceModelAttribute(d *model.DeviceModelAttribute) (err error) {
	p := query.DeviceModelAttribute
	r, err := query.DeviceModelAttribute.Where(p.ID.Eq(d.ID)).Updates(d)
	if r.RowsAffected == 0 {
		return nil
	} else {
		return err
	}
}

func UpdateDeviceModelEvent(d *model.DeviceModelEvent) (err error) {
	p := query.DeviceModelEvent
	r, err := query.DeviceModelEvent.Where(p.ID.Eq(d.ID)).Updates(d)
	if r.RowsAffected == 0 {
		return nil
	} else {
		return err
	}
}

func UpdateDeviceModelCommand(d *model.DeviceModelCommand) (err error) {
	p := query.DeviceModelCommand
	r, err := query.DeviceModelCommand.Where(p.ID.Eq(d.ID)).Updates(d)
	if r.RowsAffected == 0 {
		return nil
	} else {
		return err
	}
}

func GetDeviceModelTelemetryListByPage(r model.GetDeviceModelListByPageReq, tenant_id string) (count int64, data []*model.DeviceModelTelemetry, err error) {
	q := query.DeviceModelTelemetry
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))
	queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(r.DeviceTemplateId))
	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}
	if r.Page != 0 && r.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(r.PageSize)
		queryBuilder = queryBuilder.Offset((r.Page - 1) * r.PageSize)
	}
	data, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)

	}
	return count, data, err
}

func GetDeviceModelAttributesListByPage(r model.GetDeviceModelListByPageReq, tenant_id string) (count int64, data []*model.DeviceModelAttribute, err error) {
	q := query.DeviceModelAttribute
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))
	queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(r.DeviceTemplateId))
	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}
	if r.Page != 0 && r.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(r.PageSize)
		queryBuilder = queryBuilder.Offset((r.Page - 1) * r.PageSize)
	}
	data, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)

	}
	return count, data, err
}

func GetDeviceModelEventsListByPage(r model.GetDeviceModelListByPageReq, tenant_id string) (count int64, data []*model.DeviceModelEvent, err error) {
	q := query.DeviceModelEvent
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))
	queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(r.DeviceTemplateId))
	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}
	if r.Page != 0 && r.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(r.PageSize)
		queryBuilder = queryBuilder.Offset((r.Page - 1) * r.PageSize)
	}
	data, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)

	}
	return count, data, err
}

func GetDeviceModelCommandsListByPage(r model.GetDeviceModelListByPageReq, tenant_id string) (count int64, data []*model.DeviceModelCommand, err error) {
	q := query.DeviceModelCommand
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))
	queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(r.DeviceTemplateId))
	count, err = queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, data, err
	}
	if r.Page != 0 && r.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(r.PageSize)
		queryBuilder = queryBuilder.Offset((r.Page - 1) * r.PageSize)
	}
	data, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)

	}
	return count, data, err
}

func GetDeviceModelEventDataList(device_template_id string) ([]*model.DeviceModelEvent, error) {
	data, err := query.DeviceModelEvent.
		Where(query.DeviceModelEvent.DeviceTemplateID.Eq(device_template_id)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetDeviceModelCommandDataList(device_template_id string) ([]*model.DeviceModelCommand, error) {
	data, err := query.DeviceModelCommand.
		Where(query.DeviceModelCommand.DeviceTemplateID.Eq(device_template_id)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetDeviceModelTelemetryDataList(device_template_id string) ([]*model.DeviceModelTelemetry, error) {
	data, err := query.DeviceModelTelemetry.
		Where(query.DeviceModelTelemetry.DeviceTemplateID.Eq(device_template_id)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetDeviceModelAttributeDataList(device_template_id string) ([]*model.DeviceModelAttribute, error) {
	data, err := query.DeviceModelAttribute.
		Where(query.DeviceModelAttribute.DeviceTemplateID.Eq(device_template_id)).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetIdentifierNameTelemetry() func(device_template_id, identifier string) string {
	return func(device_template_id, identifier string) string {
		q := query.DeviceModelTelemetry
		var result model.DeviceModelTelemetry
		q.Where(q.DeviceTemplateID.Eq(device_template_id), q.DataIdentifier.Eq(identifier)).Select(q.DataName).Scan(&result)
		if result.DataName == nil {
			return identifier
		}
		return *result.DataName
	}
}

func GetIdentifierNameAttribute() func(device_template_id, identifier string) string {
	return func(device_template_id, identifier string) string {
		q := query.DeviceModelAttribute
		var result model.DeviceModelAttribute
		q.Where(q.DeviceTemplateID.Eq(device_template_id), q.DataIdentifier.Eq(identifier)).Select(q.DataName).Scan(&result)
		if result.DataName == nil {
			return identifier
		}
		return *result.DataName
	}
}
func GetIdentifierNameEvent() func(device_template_id, identifier string) string {
	return func(device_template_id, identifier string) string {
		q := query.DeviceModelEvent
		var result model.DeviceModelEvent
		q.Where(q.DeviceTemplateID.Eq(device_template_id), q.DataIdentifier.Eq(identifier)).Select(q.DataName).Scan(&result)
		if result.DataName == nil {
			return identifier
		}
		return *result.DataName
	}
}
