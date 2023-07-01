package sendmessage

import (
	"crypto/tls"
	"fmt"
	"log"

	"github.com/spf13/viper"
	"gopkg.in/gomail.v2"
)

var MessageConfig Config

type Config struct {
	Email        EmailConfig   `mapstructure:"email"`
	Alibabacloud AlibabaConfig `mapstructure:"aliyunmessage"`
}

type EmailConfig struct {
	IsOpen             int    `mapstructure:"isOpen"`
	ServerHost         string `mapstructure:"serverHost"`
	ServerPort         int    `mapstructure:"serverPort"`
	FromPasswd         string `mapstructure:"fromPasswd"`
	FromEmail          string `mapstructure:"fromEmail"`
	InsecureSkipVerify bool   `mapstructure:"insecureSkipVerify"`
}

type AlibabaConfig struct {
	IsOpen          int    `mapstructure:"isOpen"`
	AccessKeyId     string `mapstructure:"accessKeyId"`
	AccessKeySecret string `mapstructure:"accessKeySecret"`
	Endpoint        string `mapstructure:"endpoint"`
}

var emailDialer *gomail.Dialer
var messageSend *gomail.Message

func init() {

	InitConfigByViper()

	if MessageConfig.Email.IsOpen == 1 {
		log.Println("启动邮件服务")
		InitServer()
	}

	if MessageConfig.Alibabacloud.IsOpen == 1 {
		log.Println("启动短信服务")
		initAlibabaCloud()
	}

}

// 读取配置文件
func InitConfigByViper() {
	viper.SetConfigType("yaml")
	viper.SetConfigFile("./initialize/send_message/message.yml")
	err := viper.ReadInConfig()
	if err != nil {
		fmt.Println(err.Error())
	}
	err = viper.Unmarshal(&MessageConfig)
	if err != nil {
		fmt.Println(err.Error())
	}
}

func InitServer() {
	fmt.Println("邮箱服务地址：", MessageConfig.Email.ServerHost)
	d := gomail.NewDialer(MessageConfig.Email.ServerHost, MessageConfig.Email.ServerPort, MessageConfig.Email.FromEmail, MessageConfig.Email.FromPasswd)
	d.TLSConfig = &tls.Config{InsecureSkipVerify: MessageConfig.Email.InsecureSkipVerify}
	m := gomail.NewMessage()
	m.SetHeader("From", MessageConfig.Email.FromEmail)
	emailDialer = d
	messageSend = m
}

func SendEmailMessage(message string, subject string, to ...string) {
	if MessageConfig.Email.IsOpen == 1 {
		messageSend.SetHeader("To", to...)
		messageSend.SetBody("text/html", message)
		messageSend.SetHeader("Subject", subject)
		if err := emailDialer.DialAndSend(messageSend); err != nil {
			log.Println("邮件发送失败")
		} else {
			log.Println("邮件发送成功")
		}
	}
}
