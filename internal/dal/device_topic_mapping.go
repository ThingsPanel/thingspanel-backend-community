package dal

import (
	"context"

	model "project/internal/model"
	"project/internal/query"

	"github.com/sirupsen/logrus"
)

func CreateDeviceTopicMapping(mapping *model.DeviceTopicMapping) error {
	return query.DeviceTopicMapping.Create(mapping)
}

func TopicMappingExists(ctx context.Context, deviceConfigID, direction, sourceTopic, targetTopic string) (bool, error) {
	q := query.DeviceTopicMapping
	count, err := q.WithContext(ctx).
		Where(
			q.DeviceConfigID.Eq(deviceConfigID),
			q.Direction.Eq(direction),
			q.SourceTopic.Eq(sourceTopic),
			q.TargetTopic.Eq(targetTopic),
		).
		Count()
	if err != nil {
		logrus.Error(err)
		return false, err
	}
	return count > 0, nil
}

func GetDeviceTopicMappingByID(ctx context.Context, id int64) (*model.DeviceTopicMapping, error) {
	q := query.DeviceTopicMapping
	return q.WithContext(ctx).Where(q.ID.Eq(id)).First()
}

func ListDeviceTopicMappings(ctx context.Context, req *model.ListDeviceTopicMappingReq) ([]*model.DeviceTopicMapping, int64, error) {
	q := query.DeviceTopicMapping
	dao := q.WithContext(ctx).Where(q.DeviceConfigID.Eq(req.DeviceConfigID))
	if req.Direction != nil {
		dao = dao.Where(q.Direction.Eq(*req.Direction))
	}
	if req.SourceTopic != nil && *req.SourceTopic != "" {
		dao = dao.Where(q.SourceTopic.Like("%" + *req.SourceTopic + "%"))
	}
	if req.TargetTopic != nil && *req.TargetTopic != "" {
		dao = dao.Where(q.TargetTopic.Eq(*req.TargetTopic))
	}
	if req.Enabled != nil {
		if *req.Enabled {
			dao = dao.Where(q.Enabled.Is(true))
		} else {
			dao = dao.Where(q.Enabled.Is(false))
		}
	}
	if req.Description != nil && *req.Description != "" {
		dao = dao.Where(q.Description.Like("%" + *req.Description + "%"))
	}
	if req.DataIdentifier != nil && *req.DataIdentifier != "" {
		dao = dao.Where(q.DataIdentifier.Eq(*req.DataIdentifier))
	}
	// order by priority asc, id asc
	dao = dao.Order(q.Priority, q.ID)
	offset := 0
	limit := 20
	if req.Page > 0 && req.PageSize > 0 {
		offset = (req.Page - 1) * req.PageSize
		limit = req.PageSize
	}
	result, err := dao.Offset(offset).Limit(limit).Find()
	if err != nil {
		return nil, 0, err
	}
	// rebuild for count with same filters
	daoCount := q.WithContext(ctx).Where(q.DeviceConfigID.Eq(req.DeviceConfigID))
	if req.Direction != nil {
		daoCount = daoCount.Where(q.Direction.Eq(*req.Direction))
	}
	if req.SourceTopic != nil && *req.SourceTopic != "" {
		daoCount = daoCount.Where(q.SourceTopic.Like("%" + *req.SourceTopic + "%"))
	}
	if req.TargetTopic != nil && *req.TargetTopic != "" {
		daoCount = daoCount.Where(q.TargetTopic.Eq(*req.TargetTopic))
	}
	if req.Enabled != nil {
		if *req.Enabled {
			daoCount = daoCount.Where(q.Enabled.Is(true))
		} else {
			daoCount = daoCount.Where(q.Enabled.Is(false))
		}
	}
	if req.Description != nil && *req.Description != "" {
		daoCount = daoCount.Where(q.Description.Like("%" + *req.Description + "%"))
	}
	if req.DataIdentifier != nil && *req.DataIdentifier != "" {
		daoCount = daoCount.Where(q.DataIdentifier.Eq(*req.DataIdentifier))
	}
	total, err := daoCount.Count()
	if err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

func UpdateDeviceTopicMappingByID(ctx context.Context, id int64, updateMap map[string]interface{}) error {
	q := query.DeviceTopicMapping
	_, err := q.WithContext(ctx).Where(q.ID.Eq(id)).UpdateColumns(updateMap)
	return err
}

func DeleteDeviceTopicMappingByID(ctx context.Context, id int64) error {
	q := query.DeviceTopicMapping
	_, err := q.WithContext(ctx).Where(q.ID.Eq(id)).Delete()
	return err
}
