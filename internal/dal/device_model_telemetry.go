package dal

import (
	"context"
	"project/internal/model"
	"project/internal/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
)

type DeviceModelTelemetryQuery struct {
}

func (DeviceModelTelemetryQuery) First(ctx context.Context, option ...gen.Condition) (info *model.DeviceModelTelemetry, err error) {
	info, err = query.DeviceModelTelemetry.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceModelTelemetryQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.DeviceModelTelemetry, err error) {
	list, err = query.DeviceModelTelemetry.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func GetDataNameByIdentifierAndTemplateId(device_template_id string, identifier ...string) ([]*model.DeviceModelTelemetry, error) {
	data, err := query.DeviceModelTelemetry.
		Where(query.DeviceModelTelemetry.DeviceTemplateID.Eq(device_template_id)).
		Where(query.DeviceModelTelemetry.DataIdentifier.In(identifier...)).
		Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}
