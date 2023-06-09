package models

type TpNotificationMembers struct {
	Id                     string `json:"id"` // ID
	TpNotificationGroupsId string `json:"tp_notification_groups_id"`
	UsersId                string `json:"users_id"`
	IsEmail                int    `json:"is_email"`
	IsPhone                int    `json:"is_phone"`
	IsMessage              int    `json:"is_message"`
	Remark                 string `json:"remark"`
}

func (TpNotificationMembers) TableName() string {
	return "tp_notification_members"
}
