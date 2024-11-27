package service

import (
	"context"
	"encoding/json"
	"fmt"
	"project/internal/dal"
	model "project/internal/model"
	utils "project/pkg/utils"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type ExpectedData struct{}

func mergeIdentifyAndPayload(identify string, paramsStr *string) (string, error) {
	// 创建包含 identify 和 params 的 map
	mergedData := map[string]interface{}{
		"method": identify,
	}
	// 解析 payload 为 map
	if paramsStr != nil {
		var params any
		err := json.Unmarshal([]byte(*paramsStr), &params)
		if err != nil {
			return "", fmt.Errorf("error parsing payload JSON: %v", err)
		}
		mergedData["params"] = params
	}

	// 将合并后的 map 转换为 JSON 字符串
	mergedJSON, err := json.Marshal(mergedData)
	if err != nil {
		return "", fmt.Errorf("error marshaling merged data to JSON: %v", err)
	}

	return string(mergedJSON), nil
}

// 创建预期数据
func (e *ExpectedData) Create(ctx context.Context, req *model.CreateExpectedDataReq, userClaims *utils.UserClaims) (*model.ExpectedData, error) {
	if req.SendType == "command" {
		if req.Identify == nil {
			return nil, fmt.Errorf("identify 字段不能为空")
		}
		// 将identify和payload合并成一个json字符串
		payload, err := mergeIdentifyAndPayload(*req.Identify, req.Payload)
		if err != nil {
			return nil, err
		}
		req.Payload = &payload
	} else if req.Payload == nil {
		return nil, fmt.Errorf("payload 字段不能为空")
	}
	// 创建预期数据
	ed := &model.ExpectedData{
		ID:         uuid.New(),
		DeviceID:   req.DeviceID,
		SendType:   req.SendType,
		Payload:    *req.Payload,
		CreatedAt:  time.Now(),
		Status:     "pending",
		ExpiryTime: req.Expiry,
		Label:      req.Label,
		TenantID:   userClaims.TenantID,
	}
	err := dal.ExpectedDataDal{}.Create(ctx, ed)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	// 查询预期数据
	expectedData, err := dal.ExpectedDataDal{}.GetByID(ctx, ed.ID)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	// 查询设备在线状态
	deviceStatus, err := GroupApp.Device.GetDeviceOnlineStatus(req.DeviceID)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	if deviceStatus["is_online"] == 1 {
		// 发送预期数据
		err := e.Send(ctx, req.DeviceID)
		if err != nil {
			logrus.Error(err)
			return nil, err
		}
	}

	return expectedData, nil

}

// 删除预期数据
func (*ExpectedData) Delete(ctx context.Context, id string) error {
	return dal.ExpectedDataDal{}.Delete(ctx, id)
}

// 分页查询
func (*ExpectedData) PageList(ctx context.Context, req *model.GetExpectedDataPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.ExpectedDataDal{}.PageList(ctx, req, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total": total,
		"list":  list,
	}, nil
}

// 发送预期数据
func (*ExpectedData) Send(ctx context.Context, deviceID string) error {
	// 查询预期数据
	ed, err := dal.ExpectedDataDal{}.GetAllByDeviceID(ctx, deviceID)
	if err != nil {
		logrus.WithError(err).Error("查询预期数据失败")
		return err
	}
	logrus.WithField("deviceID", deviceID).Debug("获取到的预期数据", ed)

	// 遍历预期数据并处理
	for _, v := range ed {
		if v.ExpiryTime != nil && v.ExpiryTime.Before(time.Now()) {
			logrus.WithField("dataID", v.ID).Debug("预期数据已过期")
			if err := updateStatus(ctx, v.ID, "expired", nil); err != nil {
				return err
			}
			continue
		}

		var (
			status  = "sent"
			message string
		)

		// 发送预期数据
		switch v.SendType {
		case "telemetry":
			message, err = sendTelemetry(ctx, deviceID, v.Payload)
		case "attribute":
			message, err = sendAttribute(ctx, deviceID, v.Payload)
		case "command":
			message, err = sendCommand(ctx, deviceID, v.Payload)
		default:
			logrus.WithField("sendType", v.SendType).Error("未知的发送类型")
			continue
		}

		if err != nil {
			status = "expired" //失败的都算作失效
			logrus.WithError(err).WithField("sendType", v.SendType).Error("发送数据失败")
		}

		if err := updateStatus(ctx, v.ID, status, &message); err != nil {
			return err
		}
	}

	return nil
}

// 发送遥测数据
func sendTelemetry(ctx context.Context, deviceID, payload string) (string, error) {
	logrus.Debug("发送预期遥测数据")
	putMessage := &model.PutMessage{
		DeviceID: deviceID,
		Value:    payload,
	}
	err := GroupApp.TelemetryData.TelemetryPutMessage(ctx, "", putMessage, "2")
	if err != nil {
		return err.Error(), err
	}
	return "发送成功", nil
}

// 发送属性数据
func sendAttribute(ctx context.Context, deviceID, payload string) (string, error) {
	logrus.Debug("发送预期属性数据")
	putMessage := &model.AttributePutMessage{
		DeviceID: deviceID,
		Value:    payload,
	}
	err := GroupApp.AttributeData.AttributePutMessage(ctx, "", putMessage, "2")
	if err != nil {
		return err.Error(), err
	}
	return "发送成功", nil
}

// 发送命令数据
func sendCommand(ctx context.Context, deviceID, payload string) (string, error) {
	logrus.Debug("发送预期命令数据")

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		return fmt.Sprintf("Error parsing JSON payload: %s", err.Error()), err
	}

	method, ok := data["method"].(string)
	if !ok {
		return "identify 字段不存在或类型错误", fmt.Errorf("identify 字段不存在或类型错误")
	}

	var paramsStr *string
	if params, exists := data["params"]; exists {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Sprintf("Error converting params to string: %s", err.Error()), err
		}
		p := string(paramsJSON)
		paramsStr = &p
	}

	putMessage := &model.PutMessageForCommand{
		DeviceID: deviceID,
		Identify: method,
		Value:    paramsStr,
	}

	err := GroupApp.CommandData.CommandPutMessage(ctx, "", putMessage, "2")
	if err != nil {
		return err.Error(), err
	}

	return "发送成功", nil
}

// 更新预期数据状态
func updateStatus(ctx context.Context, id string, status string, message *string) error {
	var sendTime time.Time
	if status == "sent" {
		sendTime = time.Now()
	}

	err := dal.ExpectedDataDal{}.UpdateStatus(ctx, id, status, message, &sendTime)
	if err != nil {
		logrus.WithError(err).WithField("dataID", id).Error("更新预期数据状态失败")
	}
	return err
}
