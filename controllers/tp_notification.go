package controllers

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpNotification struct {
	beego.Controller
}

func (c *TpNotification) List() {
	var input struct {
		CurrentPage int `json:"current_page"`
		PerPage     int `json:"per_page"`
	}

	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	offset := (input.CurrentPage - 1) * input.PerPage
	data, count := services.GetNotificationListByTenantId(offset, input.PerPage, tenantId)
	d := DataTransponList{
		CurrentPage: input.CurrentPage,
		Total:       count,
		PerPage:     input.PerPage,
		Data:        data,
	}

	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 新增和保存使用同一个
func (c *TpNotification) Save() {
	reqData := valid.TpNotificationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	// id不等于0为修改现有的记录，同时ID不等于36位为错误的ID
	if len(reqData.Id) != 0 && len(reqData.Id) != 36 {
		response.SuccessWithMessage(400, "id错误", (*context2.Context)(c.Ctx))
		return
	}

	var id string
	var isCreate bool
	if len(reqData.Id) != 0 {
		id = reqData.Id
		isCreate = false
	} else {
		id = utils.GetUuid()
		isCreate = true
	}

	// 组装  NotificationGroups
	group := models.TpNotificationGroups{
		Id:               id,
		GroupName:        reqData.GroupName,
		Desc:             reqData.Desc,
		NotificationType: reqData.NotificationType,
		Status:           models.NotificationSwitch_Close, // 默认关闭
		TenantId:         tenantId,
	}

	if isCreate {
		group.CreateTime = time.Now().Unix()
	}

	config := make(map[string]string)
	config["email"] = ""
	config["webhook"] = ""
	config["message"] = ""
	config["phone"] = ""
	config["wechat"] = ""
	config["dingding"] = ""
	config["feishu"] = ""

	var members []models.TpNotificationMembers
	switch reqData.NotificationType {

	case models.NotificationType_Members:
		// 成员通知
		group.NotificationConfig = "[]"
		// 组装NotificationMenbers
		for _, v := range reqData.NotificationMenbers {
			tmp := models.TpNotificationMembers{
				Id:                     utils.GetUuid(),
				TpNotificationGroupsId: id,
				UsersId:                v.UserId,
				IsEmail:                v.IsEmail,
				IsPhone:                v.IsPhone,
				IsMessage:              v.IsMessage,
			}
			members = append(members, tmp)
		}

	case models.NotificationType_Email:
		// 邮件通知
		config["email"] = reqData.NotificationConfig.Email
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_Webhook:
		// webhook通知
		config["webhook"] = reqData.NotificationConfig.Webhook
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_Message:

		config["message"] = reqData.NotificationConfig.Message
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_Phone:
		// webhook通知
		config["phone"] = reqData.NotificationConfig.Phone
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_WeChat:
		// webhook通知
		config["wechat"] = reqData.NotificationConfig.WeChatBot
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_DingDing:
		// webhook通知
		config["dingding"] = reqData.NotificationConfig.DingDingBot
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	case models.NotificationType_Feishu:
		// webhook通知
		config["feishu"] = reqData.NotificationConfig.FeiShuBot
		confStr, _ := json.Marshal(config)
		group.NotificationConfig = string(confStr)
	default:
		response.SuccessWithMessage(400, "不支持的通知类型", (*context2.Context)(c.Ctx))
		return
	}

	if !services.SaveNotification(group, members, isCreate) {
		response.SuccessWithMessage(400, "mysql error", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, c.Ctx)
}

func (c *TpNotification) Detail() {
	var input struct {
		Id string `json:"id" valid:"Required;MaxSize(36)"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	n := services.GetNotificationGroups(input.Id)

	detail := make(map[string]interface{})
	detail["notification_config"] = n.NotificationConfig
	var m []models.TpNotificationMembers
	// 如果是成员通知，则需要继续查找member表
	if n.NotificationType == models.NotificationType_Members {
		m = services.GetNotificationMenbers(input.Id)
		detail["notification_members"] = m
	}

	detail["id"] = n.Id
	detail["status"] = n.Status
	detail["notification_type"] = n.NotificationType
	detail["desc"] = n.Desc
	detail["group_name"] = n.GroupName

	response.SuccessWithDetailed(200, "success", detail, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 删除
func (c *TpNotification) Delete() {
	var input struct {
		Id string `json:"id" valid:"Required;MaxSize(36)"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	// 删除告警配置表
	if !services.DeleteNotificationGroups(input.Id) {
		response.SuccessWithMessage(400, "mysql error", (*context2.Context)(c.Ctx))
		return
	}

	// 删除成员信息表
	if !services.DeleteNotificationMembers(input.Id) {
		response.SuccessWithMessage(400, "mysql error", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, c.Ctx)
}

// 开关
func (c *TpNotification) Switch() {
	var input struct {
		Id     string `json:"id" valid:"Required;MaxSize(36)"`
		Switch int    `json:"switch"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	status := services.GetNotificationGroupsStatus(input.Id)

	if status == -1 {
		response.SuccessWithMessage(400, "mysql error", (*context2.Context)(c.Ctx))
		return
	}

	if status == input.Switch {
		response.Success(200, c.Ctx)
		return
	}

	if !services.UpdateNotificationGroupsStatus(input.Id, input.Switch) {
		response.SuccessWithMessage(400, "mysql error", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, c.Ctx)

}
