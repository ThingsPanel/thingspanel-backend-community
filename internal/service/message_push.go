package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"project/internal/dal"
	"project/internal/model"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type MessagePush struct {
}

func (receiver *MessagePush) CreateMessagePush(req *model.CreateMessagePushReq, userId string) error {
	exists, err := dal.GetMessagePushMangeExists(userId, req.PushId)
	if err != nil {
		return err
	}
	if exists {
		return dal.ActiveMessagePushMange(userId, req.PushId, req.DeviceType)
	}
	return dal.CreateMessagePushMange(&model.MessagePushManage{
		ID:         uuid.New(),
		UserID:     userId,
		PushID:     req.PushId,
		DeviceType: req.DeviceType,
		Status:     1,
		CreateTime: time.Now(),
	})
}

func (receiver *MessagePush) MessagePushMangeLogout(req *model.MessagePushMangeLogoutReq, userId string) error {
	exists, err := dal.GetMessagePushMangeExists(userId, req.PushId)
	if err != nil {
		return err
	}
	if exists {
		return dal.LogoutMessagePushMange(userId, req.PushId)
	}
	return errors.New("当前用户推送id不存在")
}

func (receiver *MessagePush) GetMessagePushConfig() (*model.MessagePushConfigRes, error) {
	return dal.GetMessagePushConfig()
}

func (receiver *MessagePush) SetMessagePushConfig(req *model.MessagePushConfigReq) error {
	return dal.SetMessagePushConfig(req)
}

func (receiver *MessagePush) MessagePushSend(message model.MessagePushSend) (res string, err error) {
	config, err := dal.GetMessagePushConfig()
	if err != nil {
		return
	}
	if config.Url == "" {
		return
	}
	jsonData, _ := json.Marshal(message)
	logrus.Debug(fmt.Sprintf("发送url:%s, 请求参数：%s", config.Url, string(jsonData)))
	req, err := http.NewRequest("POST", config.Url, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}
	// 设置请求头，指定内容类型为 JSON
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}
	defer resp.Body.Close()
	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response:", err)
		return
	}
	// 打印响应
	logrus.Debug("Response:", string(body))
	return string(body), nil
}

func (receiver *MessagePush) AlarmMessagePushSend(triggered, alarmConfigId string, deviceInfo *model.Device) {
	pushManges, err := dal.GetUserMessagePushId(deviceInfo.TenantID)
	if err != nil {
		logrus.Error("查询用户pushIs失败:", err)
		return
	}
	if len(pushManges) == 0 {
		return
	}
	logrus.Debug(fmt.Sprintf("pushManges:%#v", len(pushManges)))
	message := model.MessagePushSend{
		Title:   fmt.Sprintf("告警:%v", triggered),
		Content: deviceInfo.DeviceNumber,
		Payload: model.MessagePushSendPayload{
			AlarmConfigId: alarmConfigId,
			TenantId:      deviceInfo.TenantID,
		},
	}
	for _, v := range pushManges {
		if v.PushID == "" {
			continue
		}
		message.CIds = v.PushID
		receiver.MessagePushSendAndLog(message, v, 1)
	}
}

func (receiver *MessagePush) MessagePushSendAndLog(message model.MessagePushSend, mange model.MessagePushManage, messageType int64) {
	res, err := receiver.MessagePushSend(message)
	contents, _ := json.Marshal(message)
	log := model.MessagePushLog{
		ID:          uuid.New(),
		UserID:      mange.UserID,
		MessageType: messageType,
		Content:     string(contents),
		CreateTime:  time.Now(),
	}
	if err != nil {
		log.ErrMessage = err.Error()
		log.Status = 2
	} else {
		var result map[string]interface{}
		err = json.Unmarshal([]byte(res), &result)
		if err != nil {
			logrus.Error("发送结果传map失败:", err, "返回结果:", res)
			log.Status = 2
			log.ErrMessage = fmt.Sprintf("发送结果传map失败:%v,返回结果:%v", err, res)
		} else if errCode, ok := result["errCode"]; ok {
			switch value := errCode.(type) {
			case float64:
				if value == 0 {
					log.Status = 1
					log.ErrMessage = res
				} else {
					log.Status = 2
					log.ErrMessage = res
				}
			default:
				log.Status = 2
				log.ErrMessage = res
			}
		} else {
			log.Status = 2
			log.ErrMessage = res
		}
	}
	err = dal.MessagePushSendLogSave(&log)
	if err != nil {
		logrus.Error("消息推送日志记录失败:", err)
	}
	updates := map[string]interface{}{
		"last_push_time": time.Now(),
	}
	if log.Status == 1 {
		updates["err_count"] = 0
	} else {
		updates["err_count"] = gorm.Expr("err_count + ?", 1)
	}
	err = dal.MessagePushMangeSendUpdate(mange.ID, updates)
	if err != nil {
		logrus.Error("消息推送更新最近发送时间失败:", err)
	}
}

// NotificationMessagePushSend 处理通知触发的手机端推送
func (receiver *MessagePush) NotificationMessagePushSend(tenantId string, title string, content string, payload map[string]interface{}) {
	pushManges, err := dal.GetUserMessagePushId(tenantId)
	if err != nil {
		logrus.Error("查询用户pushId失败:", err)
		return
	}
	if len(pushManges) == 0 {
		logrus.Debug("租户", tenantId, "没有绑定推送的用户")
		return
	}
	logrus.Debug(fmt.Sprintf("推送用户数量: %d", len(pushManges)))

	message := model.MessagePushSend{
		Title:   title,
		Content: content,
		Payload: payload,
	}

	for _, mange := range pushManges {
		if mange.PushID == "" {
			continue
		}
		message.CIds = mange.PushID
		receiver.MessagePushSendAndLog(message, mange, 2) // 2表示通知推送
	}
}

func (receiver *MessagePush) MessagePushMangeClear() {
	//获取被标记7天 而且没有上线的用户(注销) 已上线取消标志
	err := dal.GetMessagePushMangeInactiveWithSeven()
	if err != nil {
		logrus.Error("获取被标记7天 而且没有上线的用户(注销) 已上线取消标志:", err)
		return
	}
	//获取连续推送失败超过3次 30天不活跃的用户 设置未不活跃标记
	err = dal.GetMessagePushMangeInactive()
	if err != nil {
		logrus.Error("连续推送失败超过3次 30天不活跃的用户失败:", err)
		return
	}
}
