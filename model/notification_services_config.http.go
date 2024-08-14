package model

type SaveNotificationServicesConfigReq struct {
	EMailConfig *EmailConfig `json:"email_config" validate:"omitempty"`                         // 邮箱配置
	SMEConfig   *SMEConfig   `json:"sme_config" validate:"omitempty"`                           // 短信配置
	NoticeType  string       `json:"notice_type" form:"notice_type" validate:"required,max=36"` // 通知类型 EMAIL / SME
	Status      string       `json:"status" form:"status" validate:"required,max=36"`           // 开关
	Remark      *string      `json:"remark" form:"remark" validate:"omitempty,max=36"`          // 备注
}

const NoticeType_Email = "EMAIL"
const NoticeType_SME = "SME"
const NoticeType_Member = "MEMBER"
const NoticeType_Voice = "VOICE"
const NoticeType_Webhook = "WEBHOOK"

type EmailConfig struct {
	// Email        string `json:"email" validate:"required"`
	Host         string `json:"host" form:"host" validate:"required"`
	Port         int    `json:"port" form:"port" validate:"required"`
	FromPassword string `json:"from_password" form:"from_password" validate:"required"`
	FromEmail    string `json:"from_email" form:"from_email" validate:"required"`
	SSL          *bool  `json:"ssl" form:"ssl" validate:"omitempty"`
}

type SMEConfig struct {
}

type SendTestEmailReq struct {
	Email string `json:"email" validate:"required"`
	Body  string `json:"body" form:"body" validate:"required"`
}
