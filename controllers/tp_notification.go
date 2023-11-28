package controllers

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	sendmessage "ThingsPanel-Go/initialize/send_message"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
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

	config := make(map[string]interface{})
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

func (c *TpNotification) ConfigDetail() {

	var input struct {
		NoticeType int `json:"notice_type" valid:"Required"`
	}

	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if ok {
		if authority != "SYS_ADMIN" {
			response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
		return
	}

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	var res []models.ThirdPartyCloudServicesConfig

	err = psql.Mydb.
		Model(&models.ThirdPartyCloudServicesConfig{}).
		Where("notice_type = ?", input.NoticeType).
		Find(&res).Error
	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}

	response.SuccessWithDetailed(200, "success", res, map[string]string{}, (*context2.Context)(c.Ctx))

}

func (c *TpNotification) ConfigSave() {

	// TODO 限制超级管理员

	var input struct {
		NoticeType int    `json:"notice_type" valid:"Required"`
		Config     string `json:"config" valid:"Required"`
		Status     int    `json:"status" valid:"Required"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if ok {
		if authority != "SYS_ADMIN" {
			response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
		return
	}

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误1", (*context2.Context)(c.Ctx))
		return
	}

	var s services.TpNotificationService

	// 阿里云 短信
	// {
	// 	"notice_type" :1,
	// 	"config" :"{\"cloud_type\":1,\"access_key_id\":\"yanhao\",\"access_key_secret\":\"haoyan\",\"endpoint\":\"www.qq.com\",\"sign_name\":\"tp\",\"template_code\":\"sm2s1123123\"}"
	// ,
	// 	"status":2
	// }

	if input.NoticeType == models.NotificationConfigType_Message || input.NoticeType == models.NotificationConfigType_VerificationCode {

		var configInfo models.CloudServicesConfig_Ali

		err := json.Unmarshal([]byte(input.Config), &configInfo)
		if err != nil {
			response.SuccessWithMessage(400, "参数解析错误2", (*context2.Context)(c.Ctx))
			return
		}

		res := s.SaveNotificationConfigAli(input.NoticeType, configInfo, input.Status)
		if err != nil {
			response.SuccessWithMessage(400, res.Error(), (*context2.Context)(c.Ctx))
		}

	} else if input.NoticeType == models.NotificationConfigType_Email {

		// 网易邮箱：

		// {
		// 	"notice_type" :2,
		// 	"config" :"{\"host\":\"ww2w\",\"port\":122,\"from_password\":\"fasef\",\"from_email\":\"f\",\"ssl\":false}",
		// 	"status":2
		// }

		var configInfo models.CloudServicesConfig_Email
		err := json.Unmarshal([]byte(input.Config), &configInfo)
		if err != nil {
			response.SuccessWithMessage(400, "参数解析错误2", (*context2.Context)(c.Ctx))
			return
		}
		res := s.SaveNotificationConfigEmail(configInfo, input.Status)
		if err != nil {
			response.SuccessWithMessage(400, res.Error(), (*context2.Context)(c.Ctx))
		}

	} else {
		response.SuccessWithMessage(400, "不支持的通知类型", (*context2.Context)(c.Ctx))
		return
	}

	response.Success(200, c.Ctx)

}

func (c *TpNotification) SendEmail() {
	var input struct {
		Email        string `json:"email" valid:"Required"`
		Content      string `json:"content" valid:"Required"`
		Host         string `json:"host" valid:"Required"`
		Port         int    `json:"port" valid:"Required"`
		FromPassword string `json:"from_password" valid:"Required"`
		FromEmail    string `json:"from_email" valid:"Required"`
		SSL          bool   `json:"ssl" valid:"Required"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if ok {
		if authority != "SYS_ADMIN" {
			response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
		return
	}

	err = sendmessage.SendEmailMessageForDebug(
		input.Content, input.Host, input.Port, input.FromPassword, input.FromEmail, input.Email, input.SSL)

	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, c.Ctx)
}

// 发送短信
func (c *TpNotification) SendMessage() {
	// TODO 限制超级管理员
	var input struct {
		PhoneNumber int    `json:"phone_number" valid:"Required"`
		Content     string `json:"content" valid:"Required"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	authority, ok := c.Ctx.Input.GetData("authority").(string)
	if ok {
		if authority != "SYS_ADMIN" {
			response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
			return
		}
	} else {
		response.SuccessWithMessage(400, "authority error", (*context2.Context)(c.Ctx))
		return
	}

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	arr := strings.Split(input.Content, ",")

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	if len(arr) == 3 {
		err = sendmessage.SendSMS_SMS_461790263(input.PhoneNumber, arr[0], arr[1], arr[2], tenantId)
	} else if len(arr) == 1 {
		err = sendmessage.SendSMSVerificationCode(input.PhoneNumber, input.Content, tenantId)
	} else {
		response.SuccessWithMessage(400, "暂不支持的消息类型", (*context2.Context)(c.Ctx))
		return
	}

	if err != nil {
		response.SuccessWithMessage(400, err.Error(), (*context2.Context)(c.Ctx))
		return
	}
	response.Success(200, c.Ctx)
}

func (c *TpNotification) HistoryList() {
	var input struct {
		CurrentPage      int    `json:"current_page"`
		PerPage          int    `json:"per_page"`
		NotificationType int    `json:"notification_type"`
		ReceiveTarget    string `json:"receive_target"`
		StartTime        int64  `json:"start_time"`
		EndTime          int64  `json:"end_time"`
	}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	if input.StartTime > input.EndTime {
		response.SuccessWithMessage(400, "time error", (*context2.Context)(c.Ctx))
		return
	}

	// 获取用户租户id
	tenantId, ok := c.Ctx.Input.GetData("tenant_id").(string)
	if !ok {
		response.SuccessWithMessage(400, "租户ID获取失败", (*context2.Context)(c.Ctx))
		return
	}

	var s services.TpNotificationService

	offset := (input.CurrentPage - 1) * input.PerPage

	d, count := s.GetNotificationHistory(offset, input.PerPage, input.NotificationType, input.ReceiveTarget, input.StartTime, input.EndTime, tenantId)

	if err != nil {
		response.SuccessWithMessage(400, "mysql search error", (*context2.Context)(c.Ctx))
		return
	}

	ret := make(map[string]interface{})

	ret["total"] = count
	ret["data"] = d
	ret["current_page"] = input.CurrentPage
	ret["per_page"] = input.PerPage

	response.SuccessWithDetailed(200, "success", ret, map[string]string{}, (*context2.Context)(c.Ctx))
}

func (c *TpNotification) GetCaptcha() {
	var input struct {
		PhoneNumber string `json:"phone_number" valid:"Required"`
	}

	err := json.Unmarshal(c.Ctx.Input.RequestBody, &input)

	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	verificationCode := fmt.Sprintf("%04d", rand.Intn(10000))
	err = redis.SetStr(input.PhoneNumber+"_code", verificationCode, 5*time.Minute)

	if err != nil {
		response.SuccessWithMessage(400, "redis err", (*context2.Context)(c.Ctx))
		return
	}

	num, err := strconv.Atoi(input.PhoneNumber)
	if err != nil {
		response.SuccessWithMessage(400, "参数解析错误", (*context2.Context)(c.Ctx))
		return
	}

	err = sendmessage.SendSMSVerificationCode(num, verificationCode, "")

	if err != nil {
		response.SuccessWithMessage(400, "message err", (*context2.Context)(c.Ctx))
		return
	}

	response.SuccessWithDetailed(200, "success", nil, map[string]string{}, (*context2.Context)(c.Ctx))
}
