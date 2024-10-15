package service

import (
	"encoding/json"
	"errors"
	"project/internal/dal"
	model "project/internal/model"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type Alarm struct {
}

// CreateAlarmConfig 创建告警配置
func (a *Alarm) CreateAlarmConfig(req *model.CreateAlarmConfigReq) (data *model.AlarmConfig, err error) {
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
	return
}

// DeleteAlarmConfig 删除告警配置
func (a *Alarm) DeleteAlarmConfig(id string) (err error) {
	err = dal.DeleteAlarmConfig(id)
	return
}

// UpdateAlarmConfig 更新告警配置
func (a *Alarm) UpdateAlarmConfig(req *model.UpdateAlarmConfigReq) (data *model.AlarmConfig, err error) {
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
		return nil, err
	}
	data, err = dal.GetAlarmByID(req.ID)
	return
}

// GetAlarmConfigListByPage 分页查询告警配置
func (a *Alarm) GetAlarmConfigListByPage(req *model.GetAlarmConfigListByPageReq) (data map[string]interface{}, err error) {

	total, list, err := dal.GetAlarmConfigListByPage(req)
	if err != nil {
		return nil, err
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}

// UpdateAlarmInfo 更新告警信息
func (a *Alarm) UpdateAlarmInfo(req *model.UpdateAlarmInfoReq, userid string) (alarmInfo *model.AlarmInfo, err error) {

	alarmInfo, err = dal.GetAlarmInfoByID(req.Id)
	if err != nil {
		return nil, err
	}
	alarmInfo.Processor = &userid
	if req.ProcessingResult != nil && *req.ProcessingResult != "" {
		alarmInfo.ProcessingResult = *req.ProcessingResult
	}
	err = dal.UpdateAlarmInfo(alarmInfo)
	return
}

// UpdateAlarmInfoBatch 批量更新告警信息
func (a *Alarm) UpdateAlarmInfoBatch(req *model.UpdateAlarmInfoBatchReq, userid string) error {
	if len(req.Id) == 0 {
		return errors.New("no data update")
	}
	return dal.UpdateAlarmInfoBatch(req, userid)
}

// GetAlarmInfoListByPage 分页查询告警信息
func (a *Alarm) GetAlarmInfoListByPage(req *model.GetAlarmInfoListByPageReq) (data map[string]interface{}, err error) {

	total, list, err := dal.GetAlarmInfoListByPage(req)
	if err != nil {
		return nil, err
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}

// GetAlarmHisttoryListByPage 分页查询告警信息
func (a *Alarm) GetAlarmHisttoryListByPage(req *model.GetAlarmHisttoryListByPage, tenantID string) (data map[string]interface{}, err error) {

	total, list, err := dal.GetAlarmHistoryListByPage(req, tenantID)
	if err != nil {
		return nil, err
	}
	data = make(map[string]interface{})
	data["total"] = total
	data["list"] = list
	return
}
func (a *Alarm) AlarmHistoryDescUpdate(req *model.AlarmHistoryDescUpdateReq, tenantID string) (err error) {

	return dal.AlarmHistoryDescUpdate(req, tenantID)
}
func (a *Alarm) GetDeviceAlarmStatus(req *model.GetDeviceAlarmStatusReq) bool {

	return dal.GetDeviceAlarmStatus(req)
}

func (a *Alarm) GetConfigByDevice(req *model.GetDeviceAlarmStatusReq) ([]model.AlarmConfig, error) {

	return dal.GetConfigByDevice(req)
}

// AddAlarmInfo 触发告警信息，增加告警信息及发送通知
func (a *Alarm) AddAlarmInfo(alarmConfigID, content string) (bool, string) {

	alarmConfig, err := dal.GetAlarmByID(alarmConfigID)
	if err != nil {
		logrus.Error(err)
		return false, ""
	}

	if alarmConfig.Enabled != "Y" {
		return false, ""
	}

	if alarmConfig.NotificationGroupID != "" {
		title := alarmConfig.Name + "[" + alarmConfig.AlarmLevel + "]" + time.Now().Format("2006-01-02 15:04:05")
		GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, title, content)
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

func (a *Alarm) AlarmRecovery(alarmConfigID, content, scene_automation_id, group_id string, device_ids []string) (bool, string) {
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

func (a *Alarm) AlarmExecute(alarmConfigID, content, scene_automation_id, group_id string, device_ids []string) (bool, string) {
	var alarmName string
	alarmConfig, err := dal.GetAlarmByID(alarmConfigID)
	if err != nil {
		logrus.Error(err)
		return false, alarmName
	}

	if alarmConfig.Enabled != "Y" {
		return false, alarmName
	}
	alarmName = alarmConfig.Name
	if alarmConfig.NotificationGroupID != "" {
		title := alarmConfig.Name + "[" + alarmConfig.AlarmLevel + "]" + time.Now().Format("2006-01-02 15:04:05")
		GroupApp.NotificationServicesConfig.ExecuteNotification(alarmConfig.NotificationGroupID, title, content)
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
		AlarmStatus:       alarmConfig.AlarmLevel,
		CreateAt:          t,
	})
	if err != nil {
		logrus.Error(err)
		return false, alarmName
	}
	return true, alarmName
}
