package service

import (
	"crypto/tls"
	"encoding/json"
	"strings"
	"time"

	dal "project/dal"
	model "project/model"
	"project/others/http_client"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/gomail.v2"
)

type NotificationServicesConfig struct{}

func (n *NotificationServicesConfig) SaveNotificationServicesConfig(req *model.SaveNotificationServicesConfigReq) (*model.NotificationServicesConfig, error) {
	// 查找数据库中是否存在
	c, err := dal.GetNotificationServicesConfigByType(req.NoticeType)
	if err != nil {
		return nil, err
	}

	var config = model.NotificationServicesConfig{}

	var strconf []byte
	if req.NoticeType == model.NoticeType_Email {
		strconf, err = json.Marshal(req.EMailConfig)
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

func (n *NotificationServicesConfig) GetNotificationServicesConfig(noticeType string) (*model.NotificationServicesConfig, error) {
	c, err := dal.GetNotificationServicesConfigByType(noticeType)
	return c, err
}

func (n *NotificationServicesConfig) SendTestEmail(req *model.SendTestEmailReq) error {
	c, err := dal.GetNotificationServicesConfigByType(model.NoticeType_Email)
	if err != nil {
		return err
	}
	var emailConf model.EmailConfig
	err = json.Unmarshal([]byte(*c.Config), &emailConf)
	if err != nil {
		return err
	}

	m := gomail.NewMessage()
	// 设置发件人
	m.SetHeader("From", emailConf.Email)
	// 设置收件人，可以有多个
	m.SetHeader("To", req.Email)
	// 设置邮件主题
	m.SetHeader("Subject", "iot email test")
	// 设置邮件正文。可以是纯文本或者HTML
	m.SetBody("text/html", req.Body)

	// cokyahsoudtdbahe
	// 设置SMTP服务器（以Gmail为例），并提供认证信息
	d := gomail.NewDialer(emailConf.Host, emailConf.Port, emailConf.Email, emailConf.FromPassword)

	// 发送邮件
	if err := d.DialAndSend(m); err != nil {
		return err
	}

	return nil
}

// Send email message
func sendEmailMessage(message string, subject string, tenantId string, to ...string) (err error) {
	c, err := dal.GetNotificationServicesConfigByType(model.NoticeType_Email)
	if err != nil {
		return err
	}
	var emailConf model.EmailConfig
	err = json.Unmarshal([]byte(*c.Config), &emailConf)
	if err != nil {
		return err
	}

	d := gomail.NewDialer(emailConf.Host, emailConf.Port, emailConf.FromEmail, emailConf.FromPassword)
	if *emailConf.SSL {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: *emailConf.SSL}
	}

	m := gomail.NewMessage()
	m.SetHeader("From", emailConf.FromEmail)
	m.SetHeader("To", to...)
	m.SetBody("text/plain", message)
	m.SetHeader("Subject", subject)

	// 记录数据库
	if err := d.DialAndSend(m); err != nil {
		logrus.Error(err)
		result := "FAILURE"
		GroupApp.NotificationHisory.SaveNotificationHistory(&model.NotificationHistory{
			ID:               uuid.New(),
			SendTime:         time.Now().UTC(),
			SendContent:      &message,
			SendTarget:       to[0],
			SendResult:       &result,
			NotificationType: model.NoticeType_Email,
			TenantID:         tenantId,
			Remark:           nil,
		})
		return err
	} else {
		result := "SUCCESS"
		GroupApp.NotificationHisory.SaveNotificationHistory(&model.NotificationHistory{
			ID:               uuid.New(),
			SendTime:         time.Now().UTC(),
			SendContent:      &message,
			SendTarget:       to[0],
			SendResult:       &result,
			NotificationType: model.NoticeType_Email,
			TenantID:         tenantId,
			Remark:           nil,
		})
	}
	return nil
}

// Send notification
func (*NotificationServicesConfig) ExecuteNotification(notificationGroupId, title, content string) {

	notificationGroup, err := dal.GetNotificationGroupById(notificationGroupId)
	if err != nil {
		return
	}

	if notificationGroup.Status != "OPEN" {
		return
	}

	switch notificationGroup.NotificationType {
	case model.NoticeType_Member:
		// TODO: SEND TO MEMBER

	case model.NoticeType_Email:
		nConfig := make(map[string]string)
		err := json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &nConfig)
		if err != nil {
			logrus.Error(err)
			return
		}
		emailList := strings.Split(nConfig["email"], ",")
		for _, ev := range emailList {
			sendEmailMessage(title, content, notificationGroup.TenantID, ev)
		}
	case model.NoticeType_Webhook:
		type WebhookConfig struct {
			PayloadURL string `json:"payload_url"`
			Secret     string `json:"secret"`
		}
		nConfig := make(map[string]WebhookConfig)
		err := json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &nConfig)
		if err != nil {
			logrus.Error(err)
		}
		info := make(map[string]string)
		info["alert_title"] = title
		info["alert_details"] = content
		infoByte, _ := json.Marshal(info)
		err = http_client.SendSignedRequest(nConfig["webhook"].PayloadURL, string(infoByte), nConfig["webhook"].Secret)
		if err != nil {
			logrus.Error(err)
		}
	default:

		return
	}
}
