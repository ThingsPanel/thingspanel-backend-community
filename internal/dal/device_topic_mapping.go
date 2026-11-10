package dal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	model "project/internal/model"
	"project/internal/query"
	"project/pkg/global"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func CreateDeviceTopicMapping(mapping *model.DeviceTopicMapping) error {
	flavor, err := relational.FlavorOf(global.DB)
	if err != nil {
		return err
	}
	if flavor == relational.FlavorMariaDB {
		mapping.UniqueKeyDigest = topicMappingUniqueDigest(mapping.DeviceConfigID, mapping.Direction, mapping.SourceTopic, mapping.TargetTopic)
		return global.DB.Create(mapping).Error
	}
	return global.DB.Omit("UniqueKeyDigest").Create(mapping).Error
}

func topicMappingUniqueDigest(deviceConfigID, direction, sourceTopic, targetTopic string) string {
	key := strings.Join([]string{deviceConfigID, direction, sourceTopic, targetTopic}, string(rune(31)))
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
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
	flavor, err := relational.FlavorOf(global.DB)
	if err != nil {
		return err
	}
	if flavor == relational.FlavorMariaDB {
		return global.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current model.DeviceTopicMapping
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&current, "id = ?", id).Error; err != nil {
				return err
			}
			deviceConfigID := valueOrString(updateMap, "device_config_id", current.DeviceConfigID)
			direction := valueOrString(updateMap, "direction", current.Direction)
			sourceTopic := valueOrString(updateMap, "source_topic", current.SourceTopic)
			targetTopic := valueOrString(updateMap, "target_topic", current.TargetTopic)
			updateMap["ux_device_topic_mapping_unique__uk"] = topicMappingUniqueDigest(deviceConfigID, direction, sourceTopic, targetTopic)
			return tx.Model(&model.DeviceTopicMapping{}).Where("id = ?", id).UpdateColumns(updateMap).Error
		})
	}
	q := query.DeviceTopicMapping
	_, err = q.WithContext(ctx).Where(q.ID.Eq(id)).UpdateColumns(updateMap)
	return err
}

func valueOrString(values map[string]interface{}, key, fallback string) string {
	if value, ok := values[key]; ok {
		if s, ok := value.(string); ok {
			return s
		}
		return fmt.Sprint(value)
	}
	return fallback
}

func DeleteDeviceTopicMappingByID(ctx context.Context, id int64) error {
	q := query.DeviceTopicMapping
	_, err := q.WithContext(ctx).Where(q.ID.Eq(id)).Delete()
	return err
}
