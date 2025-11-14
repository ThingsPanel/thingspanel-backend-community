package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/internal/query"
	"project/pkg/errcode"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type DeviceTopicMapping struct{}

func (*DeviceTopicMapping) CreateDeviceTopicMapping(req *model.CreateDeviceTopicMappingReq, claims *utils.UserClaims) (model.DeviceTopicMapping, error) {
	var mapping model.DeviceTopicMapping

	ctx := context.Background()

	deviceConfig, err := query.DeviceConfig.WithContext(ctx).
		Where(query.DeviceConfig.ID.Eq(req.DeviceConfigID)).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mapping, errcode.NewWithMessage(errcode.CodeNotFound, "device config not found")
		}
		logrus.Error(err)
		return mapping, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.TenantID != claims.TenantID {
		return mapping, errcode.NewWithMessage(errcode.CodeNoPermission, "device config not owned by current tenant")
	}

	name := strings.TrimSpace(req.Name)
	sourceTopic := strings.TrimSpace(req.SourceTopic)
	targetTopic := strings.TrimSpace(req.TargetTopic)
	if name == "" || sourceTopic == "" || targetTopic == "" {
		return mapping, errcode.NewWithMessage(errcode.CodeParamError, "name/source_topic/target_topic cannot be blank")
	}

	exists, err := dal.TopicMappingExists(ctx, req.DeviceConfigID, req.Direction, sourceTopic, targetTopic)
	if err != nil {
		return mapping, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if exists {
		return mapping, errcode.NewWithMessage(errcode.CodeParamError, "topic mapping already exists")
	}

	mapping.DeviceConfigID = req.DeviceConfigID
	mapping.Name = name
	mapping.Direction = strings.ToLower(req.Direction)
	mapping.SourceTopic = sourceTopic
	mapping.TargetTopic = targetTopic
	if req.Priority != nil {
		mapping.Priority = *req.Priority
	} else {
		mapping.Priority = 100
	}
	if req.Enabled != nil {
		mapping.Enabled = *req.Enabled
	} else {
		mapping.Enabled = true
	}
	mapping.Description = req.Description
	now := time.Now().UTC()
	mapping.CreatedAt = now
	mapping.UpdatedAt = now

	if err := dal.CreateDeviceTopicMapping(&mapping); err != nil {
		logrus.Error(err)
		return mapping, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	if err := invalidateTopicMappingCache(ctx, req.DeviceConfigID); err != nil {
		logrus.Error(err)
		return mapping, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{
			"cache_error": err.Error(),
		})
	}

	return mapping, nil
}

type listResp struct {
	Total int64                      `json:"total"`
	List  []model.DeviceTopicMapping `json:"list"`
}

func (*DeviceTopicMapping) ListDeviceTopicMappings(req *model.ListDeviceTopicMappingReq, claims *utils.UserClaims) (listResp, error) {
	ctx := context.Background()
	var resp listResp

	deviceConfig, err := query.DeviceConfig.WithContext(ctx).
		Where(query.DeviceConfig.ID.Eq(req.DeviceConfigID)).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return resp, errcode.NewWithMessage(errcode.CodeNotFound, "device config not found")
		}
		logrus.Error(err)
		return resp, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.TenantID != claims.TenantID {
		return resp, errcode.NewWithMessage(errcode.CodeNoPermission, "device config not owned by current tenant")
	}

	page := req.Page
	pageSize := req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	items, total, err := dal.ListDeviceTopicMappings(ctx, req)
	if err != nil {
		return resp, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// convert to non-pointer slice for JSON stability
	resp.Total = total
	resp.List = make([]model.DeviceTopicMapping, 0, len(items))
	for _, it := range items {
		resp.List = append(resp.List, *it)
	}
	return resp, nil
}

func (*DeviceTopicMapping) UpdateDeviceTopicMapping(idStr string, req *model.UpdateDeviceTopicMappingReq, claims *utils.UserClaims) (model.DeviceTopicMapping, error) {
	var result model.DeviceTopicMapping
	ctx := context.Background()

	id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if err != nil || id <= 0 {
		return result, errcode.NewWithMessage(errcode.CodeParamError, "invalid id")
	}

	// load existing
	exist, err := dal.GetDeviceTopicMappingByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return result, errcode.NewWithMessage(errcode.CodeNotFound, "topic mapping not found")
		}
		return result, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// tenant check via device_config
	deviceConfig, err := query.DeviceConfig.WithContext(ctx).
		Where(query.DeviceConfig.ID.Eq(exist.DeviceConfigID)).
		First()
	if err != nil {
		return result, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.TenantID != claims.TenantID {
		return result, errcode.NewWithMessage(errcode.CodeNoPermission, "no permission")
	}

	// build updates
	updateMap := make(map[string]interface{})
	if req.DeviceConfigID != nil {
		updateMap["device_config_id"] = strings.TrimSpace(*req.DeviceConfigID)
	}
	if req.Name != nil {
		updateMap["name"] = strings.TrimSpace(*req.Name)
	}
	if req.Direction != nil {
		updateMap["direction"] = strings.ToLower(strings.TrimSpace(*req.Direction))
	}
	if req.SourceTopic != nil {
		updateMap["source_topic"] = strings.TrimSpace(*req.SourceTopic)
	}
	if req.TargetTopic != nil {
		updateMap["target_topic"] = strings.TrimSpace(*req.TargetTopic)
	}
	if req.Priority != nil {
		updateMap["priority"] = *req.Priority
	}
	if req.Enabled != nil {
		updateMap["enabled"] = *req.Enabled
	}
	if req.Description != nil {
		updateMap["description"] = req.Description
	}
	updateMap["updated_at"] = time.Now().UTC()

	if len(updateMap) == 1 { // only updated_at
		return *exist, nil
	}

	if err := dal.UpdateDeviceTopicMappingByID(ctx, id, updateMap); err != nil {
		return result, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// figure device_config_id for invalidation
	targetIDs := map[string]struct{}{
		exist.DeviceConfigID: {},
	}
	if v, ok := updateMap["device_config_id"]; ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			if _, already := targetIDs[s]; !already {
				targetIDs[s] = struct{}{}
			}
		}
	}
	for id := range targetIDs {
		if err := invalidateTopicMappingCache(ctx, id); err != nil {
			logrus.Errorf("invalidate topic mapping cache (%s) failed: %v", id, err)
		}
	}

	updated, err := dal.GetDeviceTopicMappingByID(ctx, id)
	if err != nil {
		return result, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return *updated, nil
}

func (*DeviceTopicMapping) DeleteDeviceTopicMapping(idStr string, claims *utils.UserClaims) error {
	ctx := context.Background()
	id, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
	if err != nil || id <= 0 {
		return errcode.NewWithMessage(errcode.CodeParamError, "invalid id")
	}
	exist, err := dal.GetDeviceTopicMappingByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errcode.NewWithMessage(errcode.CodeNotFound, "topic mapping not found")
		}
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// tenant check via device_config
	deviceConfig, err := query.DeviceConfig.WithContext(ctx).
		Where(query.DeviceConfig.ID.Eq(exist.DeviceConfigID)).
		First()
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if deviceConfig.TenantID != claims.TenantID {
		return errcode.NewWithMessage(errcode.CodeNoPermission, "no permission")
	}

	if err := dal.DeleteDeviceTopicMappingByID(ctx, id); err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	if err := invalidateTopicMappingCache(ctx, exist.DeviceConfigID); err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeCacheError, map[string]interface{}{
			"cache_error": err.Error(),
		})
	}
	return nil
}

// 说明：删除设备主题转换缓存
func invalidateTopicMappingCache(ctx context.Context, deviceConfigID string) error {
	if global.REDIS == nil {
		return fmt.Errorf("redis client not initialized")
	}
	keys := []string{
		fmt.Sprintf("tp:topicmap:up:%s", deviceConfigID),
		fmt.Sprintf("tp:topicmap:down:%s", deviceConfigID),
		fmt.Sprintf("tp:topicmap:downrev:%s", deviceConfigID),
	}
	if err := global.REDIS.Del(ctx, keys...).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}
