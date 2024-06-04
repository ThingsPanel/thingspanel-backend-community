// 定义出参schema
package api

import "time"

type ReadNotificationGroupOutSchema struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	NotificationType   string    `json:"notification_type"`
	Status             string    `json:"status"`
	NotificationConfig *string   `json:"notification_config"`
	Description        *string   `json:"description"`
	TenantID           string    `json:"tenant_id"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Remark             *string   `json:"remark"`
}

type CreateNotificationGroupResponse struct {
	Code    int                            `json:"code" example:"200"`
	Message string                         `json:"message" example:"success"`
	Data    ReadNotificationGroupOutSchema `json:"data"`
}

type GetNotificationGroupResponse struct {
	Code    int                            `json:"code"`
	Message string                         `json:"message"`
	Data    ReadNotificationGroupOutSchema `json:"data"`
}

type UpdateNotificationGroupResponse struct {
	Code    int                              `json:"code"`
	Message string                           `json:"message"`
	Data    UpdateNotificationGroupOutSchema `json:"data"`
}

type UpdateNotificationGroupOutSchema struct {
	Name               *string   `json:"name"`
	NotificationType   *string   `json:"notification_type"`
	Status             *string   `json:"status"`
	NotificationConfig *string   `json:"notification_config"`
	Description        *string   `json:"description"`
	Remark             *string   `json:"remark"`
	UpdatedAt          time.Time `json:"updated_at"`
	TenantID           string    `json:"tenant_id"`
}

type DeleteNotificationGroupResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type GetNotificationGroupListByPageResponse struct {
	Code    int                                     `json:"code"`
	Message string                                  `json:"message"`
	Data    GetNotificationGroupListByPageOutSchema `json:"data"`
}

type GetNotificationGroupListByPageOutSchema struct {
	Total int                              `json:"total"`
	List  []ReadNotificationGroupOutSchema `json:"list"`
}
