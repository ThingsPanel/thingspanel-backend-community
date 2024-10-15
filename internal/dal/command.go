package dal

import (
	"context"
	"project/internal/model"
	"project/internal/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
)

type DeviceModelCommandsQuery struct {
}

func (d DeviceModelCommandsQuery) First(ctx context.Context, option ...gen.Condition) (info *model.DeviceModelCommand, err error) {
	info, err = query.DeviceModelCommand.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceModelCommandsQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.DeviceModelCommand, err error) {
	list, err = query.DeviceModelCommand.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}
