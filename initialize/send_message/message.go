package sendmessage

import (
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi20170525 "github.com/alibabacloud-go/dysmsapi-20170525/v3/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

func CreateClient(accessKeyId *string, accessKeySecret *string, endpoint string) (_result *dysmsapi20170525.Client, _err error) {
	config := &openapi.Config{
		// 必填，您的 AccessKey ID
		AccessKeyId: accessKeyId,
		// 必填，您的 AccessKey Secret
		AccessKeySecret: accessKeySecret,
	}
	// 访问的域名
	config.Endpoint = tea.String(endpoint)
	_result = &dysmsapi20170525.Client{}
	_result, _err = dysmsapi20170525.NewClient(config)
	return _result, _err
}

// code 必须是 string
// 如果是int，且发送的验证码为 0000，收到的是 0
func SendSMSVerificationCode(phoneNumber int, code, tenantId string) (err error) {

	codeMap := make(map[string]string)
	codeMap["code"] = code

	codeStr, _ := json.Marshal(codeMap)
	phoneNumberStr := strconv.Itoa(phoneNumber)

	sendSmsRequest := &dysmsapi20170525.SendSmsRequest{
		PhoneNumbers:  tea.String(phoneNumberStr),
		SignName:      tea.String("ThingsPanel"),
		TemplateCode:  tea.String("SMS_98355081"),
		TemplateParam: tea.String(string(codeStr)),
	}

	err = SendSMS(*sendSmsRequest, &util.RuntimeOptions{}, tenantId)

	if err != nil {
		log.Println(err)
	}

	return err
}

func SendSMS_SMS_461790263(phoneNumber int, level, name, time, tenantId string) (err error) {

	codeMap := make(map[string]string)
	codeMap["level"] = level
	codeMap["name"] = name
	codeMap["time"] = time

	codeStr, _ := json.Marshal(codeMap)
	phoneNumberStr := strconv.Itoa(phoneNumber)

	sendSmsRequest := &dysmsapi20170525.SendSmsRequest{
		PhoneNumbers:  tea.String(phoneNumberStr),
		SignName:      tea.String("极益科技"),
		TemplateCode:  tea.String("SMS_461790263"),
		TemplateParam: tea.String(string(codeStr)),
	}

	err = SendSMS(*sendSmsRequest, &util.RuntimeOptions{}, tenantId)

	if err != nil {
		log.Println(err)
	}

	return err
}

func SendSMS(request dysmsapi20170525.SendSmsRequest, runtime *util.RuntimeOptions, tenantId string) (err error) {

	// 查找当前开启的SMS服务配置
	c, err := models.NotificationConfigByNoticeTypeAndStatus(models.NotificationConfigType_Message, models.NotificationSwitch_Open)
	if err != nil {
		return err
	}

	var aliConfig models.CloudServicesConfig_Ali

	if err == nil {
		json.Unmarshal([]byte(c.Config), &aliConfig)
	}

	client, err := CreateClient(tea.String(aliConfig.AccessKeyId),
		tea.String(aliConfig.AccessKeySecret), aliConfig.Endpoint)
	if err != nil {
		fmt.Println(err.Error())
	}

	sendRes, err := client.SendSmsWithOptions(&request, &util.RuntimeOptions{})
	if err != nil {
		models.SaveNotificationHistory(utils.GetUuid(), *request.TemplateParam, *request.PhoneNumbers, models.NotificationSendFail, models.NotificationConfigType_Message, tenantId)
	} else {
		models.SaveNotificationHistory(utils.GetUuid(), *request.TemplateParam, *request.PhoneNumbers, models.NotificationSendSuccess, models.NotificationConfigType_Message, tenantId)
	}
	// 记录数据库
	log.Println(sendRes.Body)
	return err
}

// 发送告警信息
func SendWarningMessage(message string, username string) {
	if username != "" {
		name := []string{username}
		subject := "IOT告警信息"
		SendEmailMessage(message, subject, "", name...)
	}
}
