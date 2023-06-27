package sendmessage

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi20170525 "github.com/alibabacloud-go/dysmsapi-20170525/v3/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
)

var AlibabacloudServer *dysmsapi20170525.Client

func initAlibabaCloud() {
	log.Println("启动短信服务")
	initAlibabacloudMessageServer()

}

func initAlibabacloudMessageServer() {
	client, err := CreateClient(tea.String(MessageConfig.Alibabacloud.AccessKeyId),
		tea.String(MessageConfig.Alibabacloud.AccessKeySecret))
	if err != nil {
		fmt.Println(err.Error())
	}
	AlibabacloudServer = client
}

func CreateClient(accessKeyId *string, accessKeySecret *string) (_result *dysmsapi20170525.Client, _err error) {
	config := &openapi.Config{
		// 必填，您的 AccessKey ID
		AccessKeyId: accessKeyId,
		// 必填，您的 AccessKey Secret
		AccessKeySecret: accessKeySecret,
	}
	// 访问的域名
	config.Endpoint = tea.String(MessageConfig.Alibabacloud.Endpoint)
	_result = &dysmsapi20170525.Client{}
	_result, _err = dysmsapi20170525.NewClient(config)
	return _result, _err
}

// code 必须是 string
// 如果是int，且发送的验证码为 0000，收到的是 0
func SendSMSVerificationCode(phoneNumber int, code string) (err error) {

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
	sendRes, err := AlibabacloudServer.SendSmsWithOptions(sendSmsRequest, &util.RuntimeOptions{})
	if err != nil {
		log.Println(err)
	}

	log.Println(sendRes.Body)

	return err
}

// 发送告警信息
func SendWarningMessage(message string, username string) {
	if username != "" {
		name := []string{username}
		subject := "IOT告警信息"
		SendEmailMessage(message, subject, name...)
	}
}
