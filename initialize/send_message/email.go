package sendmessage

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"crypto/tls"
	"encoding/json"
	"fmt"

	"github.com/beego/beego/v2/core/logs"
	"gopkg.in/gomail.v2"
)

// 发送邮件
func SendEmailMessage(message string, subject string, tenantId string, to ...string) (err error) {
	var NetEase models.CloudServicesConfig_Email
	c, err := models.NotificationConfigByNoticeTypeAndStatus(models.NotificationConfigType_Email, models.NotificationSwitch_Open)
	if err != nil {
		return err
	} else if len(c.Config) == 0 {
		return fmt.Errorf("查询不到配置，请检查配置是否保存")
	}

	err = json.Unmarshal([]byte(c.Config), &NetEase)
	if err != nil {
		return err
	}

	d := gomail.NewDialer(NetEase.Host, NetEase.Port, NetEase.FromEmail, NetEase.FromPassword)
	if NetEase.SSL {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: NetEase.SSL}
	}

	m := gomail.NewMessage()
	m.SetHeader("From", NetEase.FromEmail)
	m.SetHeader("To", to...)
	m.SetBody("text/plain", message)
	m.SetHeader("Subject", subject)

	// 记录数据库
	if err := d.DialAndSend(m); err != nil {
		logs.Error(err)
		models.SaveNotificationHistory(utils.GetUuid(), message, to[0], models.NotificationSendFail, models.NotificationConfigType_Email, tenantId)
		return err
	} else {
		models.SaveNotificationHistory(utils.GetUuid(), message, to[0], models.NotificationSendSuccess, models.NotificationConfigType_Email, tenantId)
	}
	return nil
}

// 发送调试邮件
func SendEmailMessageForDebug(message, host string, port int, fromPassword, fromEmail, toEmail string, ssl bool) (err error) {
	d := gomail.NewDialer(host, port, fromEmail, fromPassword)
	if ssl {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: ssl}
	}
	m := gomail.NewMessage()
	m.SetHeader("From", fromEmail)
	m.SetHeader("To", toEmail)
	m.SetBody("text/plain", message)
	m.SetHeader("Subject", "Debug!")
	if err := d.DialAndSend(m); err != nil {
		logs.Error(err)
		return err
	}
	return nil
}
