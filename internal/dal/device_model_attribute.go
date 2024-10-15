package dal

import (
	"context"
	"project/internal/model"
	"project/internal/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
)

type DeviceModelAttributeQuery struct {
}

func (d DeviceModelAttributeQuery) First(ctx context.Context, option ...gen.Condition) (info *model.DeviceModelAttribute, err error) {
	info, err = query.DeviceModelAttribute.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (d DeviceModelAttributeQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.DeviceModelAttribute, err error) {
	list, err = query.DeviceModelAttribute.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}
