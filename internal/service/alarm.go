package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
		logrus.Error(err)
		return false, ""
	}

	if alarmConfig.Enabled != "Y" {
		return false, ""
	}

	if alarmConfig.NotificationGroupID != "" {
		// 组装标准的通知内容
		subject := fmt.Sprintf("[ALERT] %s [%s]", alarmConfig.Name, alarmConfig.AlarmLevel)

		// 处理描述字段的指针类型
		description := ""
		if alarmConfig.Description != nil {
			description = *alarmConfig.Description
		}

		notificationContent := fmt.Sprintf(`Alert: %s
Level: %s
Time: %s
Description: %s
Details: %s`,
			alarmConfig.Name,
			alarmConfig.AlarmLevel,
			time.Now().Format("2006-01-02 15:04:05"),
			description,
			content)

		// 获取租户管理员ID
		var tenantAdminID string
		if tenantAdmin, err := dal.GetTenantAdmin(alarmConfig.TenantID); err == nil && tenantAdmin != nil {
			tenantAdminID = tenantAdmin.ID
		}

		// 构建增强的告警JSON (AddAlarmInfo方法没有device_ids参数，设为空数组)
		alertData := map[string]interface{}{
			"subject":         subject,
			"content":         notificationContent,
			"timestamp":       time.Now().Format(time.RFC3339),
			"alarm_config_id": alarmConfig.ID,
			"alarm_level":     alarmConfig.AlarmLevel,
			"tenant_id":       alarmConfig.TenantID,
			"tenant_admin_id": tenantAdminID,
			"device_ids":      []string{},
			"devices":         []map[string]interface{}{},
		}

		// 序列化JSON，不转义HTML字符
		buffer := &bytes.Buffer{}
		encoder := json.NewEncoder(buffer)
		encoder.SetEscapeHTML(false)
		err = encoder.Encode(alertData)
		if err != nil {
			logrus.Error("构建告警JSON失败:", err)
		} else {
			alertJson := strings.TrimSpace(buffer.String())
			GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, alertJson)
		}
	}

	id := uuid.New()
	t := time.Now().UTC()
	err = dal.CreateAlarmInfo(&model.AlarmInfo{
		ID:               id,
		Name:             alarmConfig.Name,
		AlarmConfigID:    alarmConfigID,
		AlarmLevel:       &alarmConfig.AlarmLevel,
		Content:          &content,
		AlarmTime:        t,
		Description:      alarmConfig.Description,
		ProcessingResult: "UND",
		TenantID:         alarmConfig.TenantID,
	})
	if err != nil {
		logrus.Error(err)
		return false, ""
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
		logrus.Error(err)
		return false, alarmName, err.Error()
	}

	if alarmConfig.Enabled != "Y" {
		return false, alarmName, "告警配置未启用"
	}
	alarmName = alarmConfig.Name
	id := uuid.New()
	if alarmConfig.NotificationGroupID != "" {
		// 组装标准的通知内容
		subject := fmt.Sprintf("[ALERT] %s [%s]", alarmConfig.Name, alarmConfig.AlarmLevel)

		// 处理描述字段的指针类型
		description := ""
		if alarmConfig.Description != nil {
			description = *alarmConfig.Description
		}

		notificationContent := fmt.Sprintf(`Alert: %s
Level: %s
Time: %s
Description: %s
Details: %s`,
			alarmConfig.Name,
			alarmConfig.AlarmLevel,
			time.Now().Format("2006-01-02 15:04:05"),
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
			"timestamp":         time.Now().Format(time.RFC3339),
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
			logrus.Error("构建告警JSON失败:", err)
		} else {
			alertJson := strings.TrimSpace(buffer.String())
			GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, alertJson)
		}
	}
	device_ids_str, _ := json.Marshal(device_ids)

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
		AlarmStatus:       alarmConfig.AlarmLevel,
		CreateAt:          t,
	})
	if err != nil {
		logrus.Error(err)
		return false, alarmName, err.Error()
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
