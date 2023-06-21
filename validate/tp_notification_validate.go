package valid

type TpNotificationValidate struct {
	Id                  string                          `json:"id,omitempty" valid:"Required;MaxSize(36)"`
	GroupName           string                          `json:"group_name" valid:"Required"`
	Desc                string                          `json:"desc,omitempty"`
	NotificationType    int                             `json:"notification_type" valid:"Required"`
	NotificationConfig  TpNotificationConfigValidate    `json:"notification_config,omitempty"`
	NotificationMenbers []TpNotificationMenbersValidate `json:"notification_members,omitempty"`
}

type TpNotificationMenbersValidate struct {
	UserId    string `json:"user_id,omitempty"`
	IsEmail   int    `json:"is_email,omitempty"`
	IsPhone   int    `json:"is_phone,omitempty"`
	IsMessage int    `json:"is_message,omitempty"`
}

type TpNotificationConfigValidate struct {
	Email       string `json:"email,omitempty"`
	Webhook     string `json:"webhook,omitempty"`
	Message     string `json:"message,omitempty"`
	Phone       string `json:"phone,omitempty"`
	WeChatBot   string `json:"wechat,omitempty"`
	DingDingBot string `json:"dingding,omitempty"`
	FeiShuBot   string `json:"feishu,omitempty"`
}
