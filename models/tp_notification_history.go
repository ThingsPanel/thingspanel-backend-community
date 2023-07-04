package models

import (
	"ThingsPanel-Go/initialize/psql"
	"fmt"
	"time"
)

type TpNotificationHistory struct {
	Id               string `json:"id"`
	SendTime         int64  `json:"send_time"`
	SendContent      string `json:"send_content"`
	SendTarget       string `json:"send_target"`
	SendResult       int    `json:"send_result"`
	NotificationType int    `json:"notification_type"`
	TenantId         string `json:"tenant_id"`
}

func (TpNotificationHistory) TableName() string {
	return "tp_notification_history"
}

const (
	NotificationSendSuccess = 1
	NotificationSendFail    = 2
)

func SaveNotificationHistory(id, sendContent, sendTarget string,
	sendResult, notificationType int,
	tenantId string) (err error) {

	d := TpNotificationHistory{
		Id:               id,
		SendTime:         time.Now().Unix(),
		SendContent:      sendContent,
		SendTarget:       sendTarget,
		SendResult:       sendResult,
		NotificationType: notificationType,
		TenantId:         tenantId,
	}

	result := psql.Mydb.Save(d)
	if result.Error != nil {
		fmt.Println(result.Error)
		return result.Error
	}

	return err
}
