package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"
	"project/third_party/others/http_client"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

type NotificationServicesConfig struct{}

// 统一的通知历史记录保存方法
func (n *NotificationServicesConfig) saveNotificationHistory(notificationType, tenantID, target, content, status string, remark *string) error {
	history := &model.NotificationHistory{
		ID:               uuid.New(),
		SendTime:         time.Now().UTC(),
		SendContent:      &content,
		SendTarget:       target,
		SendResult:       &status,
		NotificationType: notificationType,
		TenantID:         tenantID,
		Remark:           remark,
	}

	err := GroupApp.NotificationHisory.SaveNotificationHistory(history)
	if err != nil {
		logrus.Error("保存通知历史记录失败:", err)
		return err
	}
	return nil
}

// 发送webhook通知的方法
func (n *NotificationServicesConfig) sendWebhookMessage(payloadURL, secret, alertJson, tenantID string) error {
	// 验证JSON格式并确保不转义
	var alertData map[string]interface{}
	err := json.Unmarshal([]byte(alertJson), &alertData)
	if err != nil {
		logrus.Error("告警JSON格式错误:", err)
		return err
	}

	// 重新序列化JSON，禁用HTML转义
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false) // 关键：不转义HTML字符，避免 > 变成 \u003e
	err = encoder.Encode(alertData)
	if err != nil {
		logrus.Error("重新序列化JSON失败:", err)
		return err
	}
	cleanJson := strings.TrimSpace(buffer.String()) // 去掉encoder.Encode添加的换行符

	// 创建PENDING记录
	historyID := uuid.New()
	pendingStatus := "PENDING"
	history := &model.NotificationHistory{
		ID:               historyID,
		SendTime:         time.Now().UTC(),
		SendContent:      &cleanJson,
		SendTarget:       payloadURL,
		SendResult:       &pendingStatus,
		NotificationType: model.NoticeType_Webhook,
		TenantID:         tenantID,
		Remark:           nil,
	}

	err = GroupApp.NotificationHisory.SaveNotificationHistory(history)
	if err != nil {
		logrus.Error("创建webhook通知历史记录失败:", err)
		return err
	}

	// 发送webhook，带重试机制
	var lastErr error
	maxRetries := 2 // 总共尝试2次（第一次+重试1次）

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			logrus.Info(fmt.Sprintf("Webhook发送重试，第%d次", i))
		}

		// 创建带超时的context
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		err = http_client.SendSignedRequestWithTimeout(ctx, payloadURL, cleanJson, secret)
		if err == nil {
			// 发送成功，更新记录
			successStatus := "SUCCESS"
			_, updateErr := dal.UpdateNotificationHistory(historyID, &successStatus, nil)
			if updateErr != nil {
				logrus.Error("更新webhook通知历史记录失败:", updateErr)
			}
			logrus.Info("Webhook发送成功:", payloadURL)
			return nil
		}
		lastErr = err
		logrus.Error(fmt.Sprintf("Webhook发送失败，第%d次尝试:", i+1), err)
	}

	// 所有重试都失败，更新记录，在JSON后追加错误信息
	failureStatus := "FAILURE"
	errorContent := cleanJson + "; Webhook发送失败: " + lastErr.Error()
	remarkText := lastErr.Error()

	// 更新记录的内容和状态
	_, updateErr := dal.UpdateNotificationHistoryWithContent(historyID, &failureStatus, &remarkText, &errorContent)
	if updateErr != nil {
		logrus.Error("更新webhook通知历史记录失败:", updateErr)
	}

	return lastErr
}

// handleMemberNotification 处理成员通知，支持嵌套的通知类型
func (n *NotificationServicesConfig) handleMemberNotification(notificationGroup *model.NotificationGroup, alertJson, subject, content, tenantID string) error {
	logrus.Info("开始处理成员通知，通知组:", notificationGroup.ID)

	if notificationGroup.NotificationConfig == nil {
		return fmt.Errorf("notification config is nil")
	}

	// 解析告警JSON获取基本信息
	var alertData map[string]interface{}
	err := json.Unmarshal([]byte(alertJson), &alertData)
	if err != nil {
		return fmt.Errorf("parse alert json failed: %v", err)
	}

	var config map[string]interface{}
	err = json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &config)
	if err != nil {
		return fmt.Errorf("parse notification config failed: %v", err)
	}

	// 获取MEMBER配置
	memberConfig, ok := config["MEMBER"]
	if !ok {
		return fmt.Errorf("MEMBER config not found")
	}

	// 解析MEMBER配置，支持数组和对象格式
	var members []map[string]interface{}
	switch memberData := memberConfig.(type) {
	case []interface{}:
		// 数组格式
		for _, item := range memberData {
			if memberMap, ok := item.(map[string]interface{}); ok {
				members = append(members, memberMap)
			}
		}
	case map[string]interface{}:
		// 单个对象格式
		members = append(members, memberData)
	default:
		return fmt.Errorf("invalid MEMBER config format")
	}

	// 处理每个成员的通知类型
	for _, member := range members {
		name, _ := member["name"].(string)
		logrus.Info("处理成员:", name)

		notificationTypes, ok := member["notificationType"]
		if !ok {
			logrus.Info("成员", name, "没有notificationType配置")
			continue
		}

		// 处理通知类型数组
		var notifyTypes []string
		switch nt := notificationTypes.(type) {
		case []interface{}:
			for _, item := range nt {
				if typeStr, ok := item.(string); ok {
					notifyTypes = append(notifyTypes, typeStr)
				}
			}
		case string:
			notifyTypes = append(notifyTypes, nt)
		}

		// 为每个通知类型创建通知上下文
		for _, notifyType := range notifyTypes {
			switch notifyType {
			case "APP":
				logrus.Info("执行成员APP推送通知:", notificationGroup.ID, "成员:", name)

				// 获取成员的推送管理信息
				pushManage, err := dal.GetUserMessagePushManage(name)
				if err != nil {
					logrus.Warn("用户未绑定推送ID:", name, "跳过APP推送")
					// 记录失败的通知历史
					pushTarget := fmt.Sprintf("用户:%s", name)
					pushContentWithPayload := fmt.Sprintf("%s|%s", content, alertJson)
					remark := "用户未绑定推送ID"
					n.saveNotificationHistory("APP", tenantID, pushTarget, pushContentWithPayload, "FAILURE", &remark)
					continue
				}

				if pushManage.PushID == "" {
					logrus.Warn("用户推送ID为空:", name, "跳过APP推送")
					// 记录失败的通知历史
					pushTarget := fmt.Sprintf("用户:%s", name)
					pushContentWithPayload := fmt.Sprintf("%s|%s", content, alertJson)
					remark := "推送ID为空"
					n.saveNotificationHistory("APP", tenantID, pushTarget, pushContentWithPayload, "FAILURE", &remark)
					continue
				}

				// 构建推送消息
				message := model.MessagePushSend{
					Title:   subject,
					Content: content,
					Payload: map[string]interface{}{
						"notification_group_id": notificationGroup.ID,
						"tenant_id":             tenantID,
						"member_name":           name,
						"alert_data":            alertData,
					},
					CIds: pushManage.PushID,
				}

				// 调用消息推送服务并记录日志
				GroupApp.MessagePush.MessagePushSendAndLog(message, *pushManage, 2) // 2表示通知推送

				// 记录APP推送通知历史
				pushTarget := fmt.Sprintf("用户:%s", name)
				pushContentWithPayload := fmt.Sprintf("%s|%s", content, alertJson)
				n.saveNotificationHistory("APP", tenantID, pushTarget, pushContentWithPayload, "SUCCESS", nil)
			default:
				logrus.Warn("不支持的成员通知类型:", notifyType)
			}
		}
	}

	return nil
}

func (*NotificationServicesConfig) SaveNotificationServicesConfig(req *model.SaveNotificationServicesConfigReq) (*model.NotificationServicesConfig, error) {
	// 查找数据库中是否存在
	c, err := dal.GetNotificationServicesConfigByType(req.NoticeType)
	if err != nil {
		return nil, err
	}

	config := model.NotificationServicesConfig{}

	var strconf []byte
	switch req.NoticeType {
	case model.NoticeType_Email:
		strconf, err = json.Marshal(req.EMailConfig)
		if err != nil {
			return nil, err
		}
	case model.NoticeType_SME_CODE:
		strconf, err = json.Marshal(req.SMEConfig)
		if err != nil {
			return nil, err
		}
	}

	if c == nil {
		config.ID = uuid.New()
	} else {
		config.ID = c.ID
	}

	configStr := string(strconf)
	config.NoticeType = req.NoticeType
	config.Remark = req.Remark
	config.Status = req.Status
	config.Config = &configStr

	data, err := dal.SaveNotificationServicesConfig(&config)
	if err != nil {
		return nil, err
	}

	return data, err
}

func (*NotificationServicesConfig) GetNotificationServicesConfig(noticeType string) (*model.NotificationServicesConfig, error) {
	c, err := dal.GetNotificationServicesConfigByType(noticeType)
	return c, err
}

func (*NotificationServicesConfig) SendTestEmail(req *model.SendTestEmailReq) error {
	// 校验邮箱
	if !utils.ValidateEmail(req.Email) {
		return errcode.New(200014)
	}
	c, err := dal.GetNotificationServicesConfigByType(model.NoticeType_Email)
	if err != nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"notice_type": err.Error(),
		})
	}
	if c == nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "邮件服务配置不存在",
		})
	}
	if c.Config == nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "邮件服务配置内容为空",
		})
	}
	var emailConf model.EmailConfig
	err = json.Unmarshal([]byte(*c.Config), &emailConf)
	if err != nil {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	m := gomail.NewMessage()
	// 设置发件人
	m.SetHeader("From", emailConf.FromEmail)
	// 设置收件人，可以有多个
	m.SetHeader("To", req.Email)
	// 设置邮件主题
	m.SetHeader("Subject", "Iot平台-验证码通知")
	// 设置邮件正文。可以是纯文本或者HTML
	m.SetBody("text/html", req.Body)

	// cokyahsoudtdbahe
	// 设置SMTP服务器（以Gmail为例），并提供认证信息
	d := gomail.NewDialer(emailConf.Host, emailConf.Port, emailConf.FromEmail, emailConf.FromPassword)

	// 发送邮件
	if err := d.DialAndSend(m); err != nil {
		return errcode.WithData(errcode.CodeSystemError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return nil
}

// Send email message
func sendEmailMessage(message string, subject string, tenantId string, to ...string) (err error) {
	c, err := dal.GetNotificationServicesConfigByType(model.NoticeType_Email)
	if err != nil {
		return err
	}
	if c == nil {
		return fmt.Errorf("邮件服务配置不存在")
	}
	if c.Config == nil {
		return fmt.Errorf("邮件服务配置内容为空")
	}
	var emailConf model.EmailConfig
	err = json.Unmarshal([]byte(*c.Config), &emailConf)
	if err != nil {
		return err
	}

	d := gomail.NewDialer(emailConf.Host, emailConf.Port, emailConf.FromEmail, emailConf.FromPassword)

	m := gomail.NewMessage()
	m.SetHeader("From", emailConf.FromEmail)
	m.SetHeader("To", to...)
	m.SetBody("text/plain", message)
	m.SetHeader("Subject", subject)

	// 使用统一的通知历史记录方法
	nsc := &NotificationServicesConfig{}

	// 记录数据库
	if err := d.DialAndSend(m); err != nil {
		logrus.Error("邮件发送失败:", err)
		remark := err.Error()
		nsc.saveNotificationHistory(model.NoticeType_Email, tenantId, to[0], message, "FAILURE", &remark)
		return err
	} else {
		logrus.Info("邮件发送成功:", to[0])
		nsc.saveNotificationHistory(model.NoticeType_Email, tenantId, to[0], message, "SUCCESS", nil)
	}
	return nil
}

// Send notification
func (*NotificationServicesConfig) ExecuteNotification(notificationGroupId, alertJson string) {
	logrus.Info("开始执行通知，通知组ID:", notificationGroupId)

	notificationGroup, err := dal.GetNotificationGroupById(notificationGroupId)
	if err != nil {
		logrus.Error("获取通知组失败:", err)
		return
	}

	logrus.Info("通知组类型:", notificationGroup.NotificationType, "状态:", notificationGroup.Status)

	if notificationGroup.Status != "OPEN" {
		logrus.Info("通知组未开启:", notificationGroupId)
		return
	}

	// 解析通知JSON获取基本信息
	var alertData map[string]interface{}
	err = json.Unmarshal([]byte(alertJson), &alertData)
	if err != nil {
		logrus.Error("解析告警JSON失败:", err)
		return
	}

	subject, _ := alertData["subject"].(string)
	content, _ := alertData["content"].(string)

	// 处理通知类型，支持组合类型（如"EMAIL,APP"）
	notificationTypes := strings.Split(notificationGroup.NotificationType, ",")
	for _, notifyType := range notificationTypes {
		notifyType = strings.TrimSpace(notifyType)
		switch notifyType {
		case model.NoticeType_Member:
			// 处理成员通知，支持嵌套的通知类型（如APP）
			nsc := &NotificationServicesConfig{}
			err := nsc.handleMemberNotification(notificationGroup, alertJson, subject, content, notificationGroup.TenantID)
			if err != nil {
				logrus.Error("成员通知处理失败:", err)
			}

		case model.NoticeType_Email:
			nConfig := make(map[string]string)
			err := json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &nConfig)
			if err != nil {
				logrus.Error("解析邮件配置失败:", err)
				continue
			}

			// 邮件特定格式：添加邮件签名
			emailBody := content + "\n\n---\nThis email was sent by ThingsPanel"

			emailList := strings.Split(nConfig["EMAIL"], ",")
			for _, emailAddr := range emailList {
				emailAddr = strings.TrimSpace(emailAddr)
				if emailAddr != "" {
					err := sendEmailMessage(emailBody, subject, notificationGroup.TenantID, emailAddr)
					if err != nil {
						// 在JSON后追加错误信息
						errorContent := alertJson + "; 邮件发送失败: " + err.Error()
						nsc := &NotificationServicesConfig{}
						nsc.saveNotificationHistory(model.NoticeType_Email, notificationGroup.TenantID, emailAddr, errorContent, "FAILURE", nil)
						logrus.Error("发送邮件失败:", err)
					}
				}
			}

		case model.NoticeType_Webhook:
			type WebhookConfig struct {
				PayloadURL string
				Secret     string
			}
			var nConfig WebhookConfig
			err = json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &nConfig)
			if err != nil {
				logrus.Error("解析Webhook配置失败:", err)
				continue
			}

			// 使用新的webhook发送方法，传递原始JSON
			nsc := &NotificationServicesConfig{}
			err = nsc.sendWebhookMessage(nConfig.PayloadURL, nConfig.Secret, alertJson, notificationGroup.TenantID)
			if err != nil {
				logrus.Error("Webhook通知发送失败:", err)
			}

		case "APP":
			// 直接APP类型：给租户下所有绑定推送ID的用户推送
			// 注意：当前业务中主要使用MEMBER类型配合成员配置来实现精确推送
			logrus.Info("执行直接APP类型推送通知:", notificationGroupId)

			// 构建推送内容
			pushTitle := subject
			pushContent := content

			// 构建payload，包含业务相关信息
			pushPayload := map[string]interface{}{
				"notification_group_id": notificationGroupId,
				"tenant_id":             notificationGroup.TenantID,
				"alert_data":            alertData,
			}

			// 调用消息推送服务
			GroupApp.MessagePush.NotificationMessagePushSend(
				notificationGroup.TenantID,
				pushTitle,
				pushContent,
				pushPayload,
			)

			// 记录APP推送通知历史
			nsc := &NotificationServicesConfig{}
			pushTarget := "租户全员用户"
			pushContentWithPayload := fmt.Sprintf("%s|%s", pushContent, alertJson)
			nsc.saveNotificationHistory("APP", notificationGroup.TenantID, pushTarget, pushContentWithPayload, "SUCCESS", nil)

		default:
			logrus.Warn("未支持的通知类型:", notifyType)
		}
	}
}
