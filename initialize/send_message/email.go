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
	Email *EmailClient `yaml:"email"`
}
type EmailClient struct {
	IsOpen             int    `yaml:"isOpen"`
	ServerHost         string `yaml:"serverHost"`
	ServerPort         int    `yaml:"serverPort"`
	FromPasswd         string `yaml:"fromPasswd"`
	FromEmail          string `yaml:"fromEmail"`
	InsecureSkipVerify bool   `yaml:"insecureSkipVerify"`
}

var emailDialer *gomail.Dialer
var messageSend *gomail.Message

func init() {
	log.Println("启动邮件服务")
	InitConfigByViper()
	if MessageConfig.Email.IsOpen == 1 {
		InitServer()
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
			fmt.Println("发送失败...")
		}
	}
}
