package sendmessage

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"crypto/tls"
	"encoding/json"
	"fmt"

	"gopkg.in/gomail.v2"
)

func SendEmailMessage(message string, subject string, tenantId string, to ...string) (err error) {

	c, err := models.NotificationConfigByNoticeTypeAndStatus(models.NotificationConfigType_Email, models.NotificationSwitch_Open)

	if len(c.Config) == 0 {
		return fmt.Errorf("无可用配置")
	}

	var NetEase models.CloudServicesConfig_Email

	if err == nil {
		json.Unmarshal([]byte(c.Config), &NetEase)
	}

	d := gomail.NewDialer(NetEase.Host, NetEase.Port, NetEase.FromEmail, NetEase.FromPassword)

	d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	m := gomail.NewMessage()
	m.SetHeader("From", NetEase.FromEmail)

	m.SetHeader("To", to...)
	m.SetBody("text/html", message)
	m.SetHeader("Subject", subject)
	// 记录数据库

	if err := d.DialAndSend(m); err != nil {

		models.SaveNotificationHistory(utils.GetUuid(), message, to[0], models.NotificationSendFail, models.NotificationConfigType_Email, tenantId)
		return err
	} else {
		models.SaveNotificationHistory(utils.GetUuid(), message, to[0], models.NotificationSendSuccess, models.NotificationConfigType_Email, tenantId)
	}
	return nil
}
