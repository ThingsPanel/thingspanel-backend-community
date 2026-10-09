package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"project/initialize"
	"project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type Alarm struct{}

// CreateAlarmConfig 创建告警配置
func (*Alarm) CreateAlarmConfig(req *model.CreateAlarmConfigReq) (data *model.AlarmConfig, err error) {
	data = &model.AlarmConfig{}
	t := time.Now().UTC()
	data.ID = uuid.New()
	data.Name = req.Name
	data.Description = req.Description
	data.AlarmLevel = req.AlarmLevel
	data.NotificationGroupID = req.NotificationGroupID
	data.CreatedAt = t
	data.UpdatedAt = t
	data.TenantID = req.TenantID
	data.Remark = req.Remark
	data.Enabled = req.Enabled

	err = dal.CreateAlarmConfig(data)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return
}

// DeleteAlarmConfig 删除告警配置
func (*Alarm) DeleteAlarmConfig(id string) (err error) {
	err = dal.DeleteAlarmConfig(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清理告警名称缓存
	_ = dal.DeleteAlarmNameCache(id)
	// 清理 alarm_cach_alarm_v6_{alarmId}_{deviceId} 相关缓存
	go func() {
		if err := initialize.NewAlarmCache().DeleteByAlarmId(id); err != nil {
			logrus.Error("DeleteAlarmConfig 清理告警设备缓存失败: ", err)
		}
	}()
	// 清理 alarm_history 中该告警配置的历史记录
	go func() {
		if err := dal.DeleteAlarmHistoryByConfigId(id); err != nil {
			logrus.Error("DeleteAlarmConfig 清理告警历史记录失败: ", err)
		}
	}()
	return
}

// UpdateAlarmConfig 更新告警配置
func (*Alarm) UpdateAlarmConfig(req *model.UpdateAlarmConfigReq) (data *model.AlarmConfig, err error) {
	data = &model.AlarmConfig{}
	data.ID = req.ID
	if req.Name != nil {
		data.Name = *req.Name
	}
	if req.Description != nil {
		data.Description = req.Description
	}
	if req.AlarmLevel != nil {
		data.AlarmLevel = *req.AlarmLevel
	}
	if req.NotificationGroupID != nil {
		data.NotificationGroupID = *req.NotificationGroupID
	}
	data.UpdatedAt = time.Now().UTC()
	data.TenantID = *req.TenantID
	data.Remark = req.Remark
	if req.Enabled != nil {
		data.Enabled = *req.Enabled
	}

	err = dal.UpdateAlarmConfig(data)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 清理告警名称缓存，确保后续 GetAlarmNameWithCache 获取最新名称
	go func() {
		if err := dal.DeleteAlarmNameCache(req.ID); err != nil {
			logrus.Error("UpdateAlarmConfig 清理告警名称缓存失败: ", err)
		}
	}()
	data, err = dal.GetAlarmByID(req.ID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}

// GetAlarmConfigListByPage 分页查询告警配置
func (*Alarm) GetAlarmConfigListByPage(req *model.GetAlarmConfigListByPageReq) (data map[string]interface{}, err error) {
	total, list, err := dal.GetAlarmConfigListByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}

// UpdateAlarmInfo 更新告警信息
func (*Alarm) UpdateAlarmInfo(req *model.UpdateAlarmInfoReq, userid string) (alarmInfo *model.AlarmInfo, err error) {
	alarmInfo, err = dal.GetAlarmInfoByID(req.Id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	alarmInfo.Processor = &userid
	if req.ProcessingResult != nil && *req.ProcessingResult != "" {
		alarmInfo.ProcessingResult = *req.ProcessingResult
	}
	err = dal.UpdateAlarmInfo(alarmInfo)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return
}

// UpdateAlarmInfoBatch 批量更新告警信息
func (*Alarm) UpdateAlarmInfoBatch(req *model.UpdateAlarmInfoBatchReq, userid string) error {
	if len(req.Id) == 0 {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"id": "id is empty",
		})
	}
	err := dal.UpdateAlarmInfoBatch(req, userid)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

// GetAlarmInfoListByPage 分页查询告警信息
func (*Alarm) GetAlarmInfoListByPage(req *model.GetAlarmInfoListByPageReq) (data map[string]interface{}, err error) {
	total, list, err := dal.GetAlarmInfoListByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}

// GetAlarmHisttoryListByPage 分页查询告警信息
func (*Alarm) GetAlarmHisttoryListByPage(req *model.GetAlarmHisttoryListByPage, tenantID string) (data map[string]interface{}, err error) {
	total, list, err := dal.GetAlarmHistoryListByPage(req, tenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}

func (*Alarm) AlarmHistoryDescUpdate(req *model.AlarmHistoryDescUpdateReq, tenantID string) (err error) {
	err = dal.AlarmHistoryDescUpdate(req, tenantID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return
}

func (*Alarm) GetDeviceAlarmStatus(req *model.GetDeviceAlarmStatusReq) bool {
	return dal.GetDeviceAlarmStatus(req)
}

func (*Alarm) GetConfigByDevice(req *model.GetDeviceAlarmStatusReq) ([]model.AlarmConfig, error) {
	data, err := dal.GetConfigByDevice(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}

// AddAlarmInfo 触发告警信息，增加告警信息及发送通知
func (*Alarm) AddAlarmInfo(alarmConfigID, content string) (bool, string) {
	alarmConfig, err := dal.GetAlarmByID(alarmConfigID)
	if err != nil {
		logrus.Warn("alarm configuration lookup failed")
		return false, ""
	}

	if alarmConfig.Enabled != "Y" {
		return false, ""
	}

	id := uuid.New()
	t := time.Now().UTC()
	alarmRow := &model.AlarmInfo{
		ID:               id,
		Name:             alarmConfig.Name,
		AlarmConfigID:    alarmConfigID,
		AlarmLevel:       &alarmConfig.AlarmLevel,
		Content:          &content,
		AlarmTime:        t,
		Description:      alarmConfig.Description,
		ProcessingResult: "UND",
		TenantID:         alarmConfig.TenantID,
	}
	var subject, notificationContent, alertJSON string
	if alarmConfig.NotificationGroupID != "" {
		subject = fmt.Sprintf("[ALERT] %s [%s]", alarmConfig.Name, alarmConfig.AlarmLevel)
		description := ""
		if alarmConfig.Description != nil {
			description = *alarmConfig.Description
		}
		notificationContent = fmt.Sprintf(`Alert: %s
Level: %s
Time: %s
Description: %s
Details: %s`, alarmConfig.Name, alarmConfig.AlarmLevel, t.Format("2006-01-02 15:04:05"), description, content)
		var tenantAdminID string
		if tenantAdmin, lookupErr := dal.GetTenantAdmin(alarmConfig.TenantID); lookupErr == nil && tenantAdmin != nil {
			tenantAdminID = tenantAdmin.ID
		}
		alertData := map[string]interface{}{"subject": subject, "content": notificationContent, "timestamp": t.Format(time.RFC3339), "alarm_config_id": alarmConfig.ID, "alarm_level": alarmConfig.AlarmLevel, "tenant_id": alarmConfig.TenantID, "tenant_admin_id": tenantAdminID, "device_ids": []string{}, "devices": []map[string]interface{}{}}
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(alertData); err != nil {
			logrus.Warn("alarm notification payload could not be encoded")
		} else {
			alertJSON = strings.TrimSpace(buffer.String())
		}
	}

	route := dal.SourceRouteSnapshot{Engine: "legacy"}
	if alarmConfig.NotificationGroupID == "" {
		err = dal.SaveAlarmInfoQuietly(context.Background(), alarmRow)
	} else if bridge := currentSourceBridge(); bridge == nil || !bridge.Enabled() {
		err = dal.SaveAlarmInfoQuietly(context.Background(), alarmRow)
	} else if alertJSON == "" {
		remark := "source_payload_unavailable"
		alarmRow.Remark = &remark
		err = dal.SaveAlarmInfoQuietly(context.Background(), alarmRow)
		route.Engine = "blocked"
	} else {
		key := dal.SourceRouteKey{DeploymentID: bridge.DeploymentID(), TenantID: alarmConfig.TenantID, LegacyGroup: alarmConfig.NotificationGroupID}
		route, err = dal.SaveAlarmInfoWithSource(context.Background(), alarmRow, key, func(snapshot dal.SourceRouteSnapshot) (*dal.SourceOutboxRecord, error) {
			return bridge.buildOutbox(snapshot, alarmConfig.TenantID, alarmConfig.NotificationGroupID, id, uuid.New(), t, subject, notificationContent, alertJSON)
		})
		if errors.Is(err, dal.ErrSourceRouteUnavailable) {
			// Routing metadata failure preserves the alarm and suppresses every sender.
			remark := "source_route_unavailable"
			alarmRow.Remark = &remark
			err = dal.SaveAlarmInfoQuietly(context.Background(), alarmRow)
			route.Engine = "blocked"
			if err == nil {
				logrus.Warn("notification source route unavailable; alarm saved and delivery suppressed")
			}
		}
	}
	if err != nil {
		logrus.Warn("alarm persistence failed")
		return false, ""
	}
	if route.Engine == "legacy" && alarmConfig.NotificationGroupID != "" && alertJSON != "" {
		GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, alertJSON)
	}
	return true, id
}

func (*Alarm) AlarmRecovery(alarmConfigID, content, scene_automation_id, group_id string, device_ids []string) (bool, string) {
	alarmConfig, err := dal.GetAlarmByID(alarmConfigID)
	if err != nil {
		logrus.Error(err)
		return false, ""
	}

	device_ids_str, _ := json.Marshal(device_ids)
	id := uuid.New()
	t := time.Now().UTC()
	err = dal.AlarmHistorySave(&model.AlarmHistory{
		ID:                id,
		Name:              alarmConfig.Name,
		AlarmConfigID:     alarmConfigID,
		Content:           &content,
		Description:       alarmConfig.Description,
		TenantID:          alarmConfig.TenantID,
		SceneAutomationID: scene_automation_id,
		GroupID:           group_id,
		AlarmDeviceList:   string(device_ids_str),
		AlarmStatus:       "N",
		CreateAt:          t,
	})
	if err != nil {
		logrus.Error(err)
		return false, ""
	}
	return true, id
}

func (*Alarm) AlarmExecute(alarmConfigID, content, scene_automation_id, group_id string, device_ids []string) (bool, string, string) {
	var alarmName string
	alarmConfig, err := dal.GetAlarmByID(alarmConfigID)
	if err != nil {
		logrus.Warn("alarm configuration lookup failed")
		return false, alarmName, "告警配置读取失败"
	}

	if alarmConfig.Enabled != "Y" {
		return false, alarmName, "告警配置未启用"
	}
	alarmName = alarmConfig.Name
	id := uuid.New()
	var subject, notificationContent, alertJSON string
	t := time.Now().UTC()
	if alarmConfig.NotificationGroupID != "" {
		// 组装标准的通知内容
		subject = fmt.Sprintf("[ALERT] %s [%s]", alarmConfig.Name, alarmConfig.AlarmLevel)

		// 处理描述字段的指针类型
		description := ""
		if alarmConfig.Description != nil {
			description = *alarmConfig.Description
		}

		notificationContent = fmt.Sprintf(`Alert: %s
Level: %s
Time: %s
Description: %s
Details: %s`,
			alarmConfig.Name,
			alarmConfig.AlarmLevel,
			t.Format("2006-01-02 15:04:05"),
			description,
			content)

		// 获取租户管理员ID
		var tenantAdminID string
		if tenantAdmin, err := dal.GetTenantAdmin(alarmConfig.TenantID); err == nil && tenantAdmin != nil {
			tenantAdminID = tenantAdmin.ID
		}

		// 获取设备详细信息
		var devices []map[string]interface{}
		for _, deviceID := range device_ids {
			if deviceInfo, err := dal.GetDeviceByID(deviceID); err == nil && deviceInfo != nil {
				device := map[string]interface{}{
					"id":              deviceInfo.ID,
					"device_number":   deviceInfo.DeviceNumber,
					"name":            deviceInfo.Name,
					"current_version": deviceInfo.CurrentVersion,
					"created_at":      deviceInfo.CreatedAt,
					"label":           deviceInfo.Label,
					"product_id":      deviceInfo.ProductID,
					"is_online":       deviceInfo.IsOnline,
					"access_way":      deviceInfo.AccessWay,
					"description":     deviceInfo.Description,
					"tenant_id":       deviceInfo.TenantID,
				}
				devices = append(devices, device)
			}
		}

		// 构建增强的告警JSON
		alertData := map[string]interface{}{
			"id":                id,
			"alarm_config_id":   alarmConfigID,
			"alarm_config_name": alarmConfig.Name,
			"subject":           subject,
			"content":           notificationContent,
			"timestamp":         t.Format(time.RFC3339),
			"alarm_level":       alarmConfig.AlarmLevel,
			"tenant_id":         alarmConfig.TenantID,
			"tenant_admin_id":   tenantAdminID,
			"device_ids":        device_ids,
			"devices":           devices,
		}

		// 序列化JSON，不转义HTML字符
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		err = encoder.Encode(alertData)
		if err != nil {
			logrus.Warn("alarm notification payload could not be encoded")
		} else {
			alertJSON = strings.TrimSpace(buffer.String())
			notificationContent, _ = alertData["content"].(string)
		}
	}
	device_ids_str, _ := json.Marshal(device_ids)

	alarmRow := &model.AlarmHistory{
		ID:                id,
		Name:              alarmConfig.Name,
		AlarmConfigID:     alarmConfigID,
		Content:           &content,
		Description:       alarmConfig.Description,
		TenantID:          alarmConfig.TenantID,
		SceneAutomationID: scene_automation_id,
		GroupID:           group_id,
		AlarmDeviceList:   string(device_ids_str),
		AlarmStatus:       alarmConfig.AlarmLevel,
		CreateAt:          t,
	}
	route := dal.SourceRouteSnapshot{Engine: "legacy"}
	if alarmConfig.NotificationGroupID == "" {
		err = dal.SaveAlarmHistoryQuietly(context.Background(), alarmRow)
	} else if bridge := currentSourceBridge(); bridge == nil || !bridge.Enabled() {
		err = dal.SaveAlarmHistoryQuietly(context.Background(), alarmRow)
	} else if alertJSON == "" {
		remark := "source_payload_unavailable"
		alarmRow.Remark = &remark
		err = dal.SaveAlarmHistoryQuietly(context.Background(), alarmRow)
		route.Engine = "blocked"
	} else {
		key := dal.SourceRouteKey{DeploymentID: bridge.DeploymentID(), TenantID: alarmConfig.TenantID, LegacyGroup: alarmConfig.NotificationGroupID}
		route, err = dal.SaveAlarmHistoryWithSource(context.Background(), alarmRow, key, func(snapshot dal.SourceRouteSnapshot) (*dal.SourceOutboxRecord, error) {
			return bridge.buildOutbox(snapshot, alarmConfig.TenantID, alarmConfig.NotificationGroupID, id, uuid.New(), t, subject, notificationContent, alertJSON)
		})
		if errors.Is(err, dal.ErrSourceRouteUnavailable) {
			remark := "source_route_unavailable"
			alarmRow.Remark = &remark
			err = dal.SaveAlarmHistoryQuietly(context.Background(), alarmRow)
			route.Engine = "blocked"
			if err == nil {
				logrus.Warn("notification source route unavailable; alarm saved and delivery suppressed")
			}
		}
	}
	if err != nil {
		logrus.Warn("alarm persistence failed")
		return false, alarmName, "告警持久化失败"
	}
	if route.Engine == "legacy" && alarmConfig.NotificationGroupID != "" && alertJSON != "" {
		GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, alertJSON)
	}
	for _, deviceId := range device_ids {
		// 已废弃：手机端推送现在通过通知系统统一处理
		// 不再需要获取 deviceInfo，因为推送已通过通知系统处理
		_ = deviceId // 避免未使用变量警告
	}
	//return true, alarmName, err.Error()
	return true, alarmName, ""
}

// 通过id获取告警信息
func (*Alarm) GetAlarmInfoHistoryByID(id string) (map[string]interface{}, error) {
	alarmInfo, err := dal.GetAlarmInfoHistoryByID(id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return alarmInfo, nil
}

// GetAlarmDeviceCountsByTenant 获取租户下告警设备数量
func (a *Alarm) GetAlarmDeviceCountsByTenant(tenantID string) (*model.AlarmDeviceCountsResponse, error) {
	ctx := context.Background()
	db := &dal.LatestDeviceAlarmQuery{}

	// 查询所有告警设备总数
	totalCount, err := db.CountDevicesByTenantAndStatus(ctx, tenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"operation": "count_alarm_devices",
			"error":     err.Error(),
		})
	}

	return &model.AlarmDeviceCountsResponse{
		AlarmDeviceTotal: int64(totalCount),
	}, nil
}

// DeleteAlarmHistory 删除告警历史
func (*Alarm) DeleteAlarmHistory(id string, tenantID string) (err error) {
	err = dal.DeleteAlarmHistory(id, tenantID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return
}
