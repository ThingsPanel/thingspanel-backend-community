package service

import (
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

	// if emailConf.SSL != nil {
	// 	// 检查是否启用了 SSL
	// 	if *emailConf.SSL {
	// 		d.TLSConfig = &tls.Config{
	// 			MinVersion:         tls.VersionTLS12, // 设置最低支持 TLS 1.2
	// 			MaxVersion:         tls.VersionTLS13, // 设置最高支持 TLS 1.3
	// 			InsecureSkipVerify: false,            // 显式禁用不安全的证书验证跳过
	// 			CipherSuites: []uint16{ // 设置安全的密码套件
	// 				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	// 				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	// 				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	// 				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	// 			},
	// 		}
	// 	}
	// }

	m := gomail.NewMessage()
	m.SetHeader("From", emailConf.FromEmail)
	m.SetHeader("To", to...)
	m.SetBody("text/plain", message)
	m.SetHeader("Subject", subject)

	// 记录数据库
	if err := d.DialAndSend(m); err != nil {
		logrus.Error(err)
		result := "FAILURE"
		remark := err.Error()
		GroupApp.NotificationHisory.SaveNotificationHistory(&model.NotificationHistory{
			ID:               uuid.New(),
			SendTime:         time.Now().UTC(),
			SendContent:      &message,
			SendTarget:       to[0],
			SendResult:       &result,
			NotificationType: model.NoticeType_Email,
			TenantID:         tenantId,
			Remark:           &remark,
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
		logrus.Debug("通知配置:", nConfig)
		emailList := strings.Split(nConfig["EMAIL"], ",")
		for _, ev := range emailList {
			logrus.Debug("发送邮件地址：", ev)
			err := sendEmailMessage(title, content, notificationGroup.TenantID, ev)
			if err != nil {
				logrus.Error("发送邮件失败:", err)
			}
		}
	case model.NoticeType_Webhook:
		type WebhookConfig struct {
			PayloadURL string
			Secret     string
		}
		//nConfig := make(map[string]WebhookConfig)
		var nConfig WebhookConfig
		err = json.Unmarshal([]byte(*notificationGroup.NotificationConfig), &nConfig)
		if err != nil {
			logrus.Error(err)
		}
		info := make(map[string]string)
		info["alert_title"] = title
		info["alert_details"] = content
		infoByte, _ := json.Marshal(info)
		//err = http_client.SendSignedRequest(nConfig["webhook"].PayloadURL, string(infoByte), nConfig["webhook"].Secret)
		err = http_client.SendSignedRequest(nConfig.PayloadURL, string(infoByte), nConfig.Secret)
		if err != nil {
			logrus.Error(err)
		}
	default:

		return
	}
}
