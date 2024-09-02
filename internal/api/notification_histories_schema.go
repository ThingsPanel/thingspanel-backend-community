// 定义出参schema
package api

// NotificationHistory通知历史记录定义
// type NotificationHistory struct {
// 	ID               string    `gorm:"column:id;primaryKey" json:"id"`
// 	SendTime         time.Time `gorm:"column:send_time;not null" json:"send_time"`
// 	SendContent      *string   `gorm:"column:send_content" json:"send_content"`
// 	SendTarget       string    `gorm:"column:send_target;not null" json:"send_target"`
// 	SendResult       *string   `gorm:"column:send_result" json:"send_result"`
// 	NotificationType string    `gorm:"column:notification_type;not null" json:"notification_type"`
// 	TenantID         string    `gorm:"column:tenant_id;not null" json:"tenant_id"`
// 	Remark           *string   `gorm:"column:remark" json:"remark"`
// }

// ReadNotificationHistorySchema通知历史记录JSON序列化结构定义
type ReadNotificationHistorySchema struct {
	ID               string  `json:"id"`
	SendTime         string  `json:"send_time"`
	SendContent      *string `json:"send_content"`
	SendTarget       string  `json:"send_target"`
	SendResult       *string `json:"send_result"`
	NotificationType string  `json:"notification_type"`
	TenantID         string  `json:"tenant_id"`
	Remark           *string `json:"remark"`
}

// WriteNotificationHistorySchema通知历史记录JSON反序列化结构定义
type WriteNotificationHistorySchema struct {
	SendTime         string  `json:"send_time"`
	SendContent      *string `json:"send_content"`
	SendTarget       string  `json:"send_target"`
	SendResult       *string `json:"send_result"`
	NotificationType string  `json:"notification_type"`
	TenantID         string  `json:"tenant_id"`
	Remark           *string `json:"remark"`
}

type GetNotificationHistoryListByPageResponse struct {
	Code    int                                       `json:"code"`
	Message string                                    `json:"message"`
	Data    GetNotificationHistoryListByPageOutSchema `json:"data"`
}

type GetNotificationHistoryListByPageOutSchema struct {
	Total int                             `json:"total"`
	List  []ReadNotificationHistorySchema `json:"list"`
}
