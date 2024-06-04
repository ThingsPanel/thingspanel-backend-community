package utils

import (
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-basic/uuid"
)

// mosquitto_pub -h xx.xx.xx.xx -p 1883 -t "devices/telemetry" -m "{\"tems\":112}" -u "xxxxx" -P "xxxxx" -i "0"
func BuildMosquittoPubCommand(host string, port string, username string, password string, topic string, payload string, clientId string) string {
	var sb strings.Builder
	sb.WriteString("mosquitto_pub")
	sb.WriteString(fmt.Sprintf(" -h %s", host))
	sb.WriteString(fmt.Sprintf(" -p %s", port))

	if topic != "" {
		sb.WriteString(fmt.Sprintf(" -t \"%s\"", topic))
	}
	if payload != "" {
		sb.WriteString(fmt.Sprintf(" -m \"%s\"", payload))
	}
	if username != "" {
		sb.WriteString(fmt.Sprintf(" -u \"%s\"", username))

	}
	if password != "" {
		sb.WriteString(fmt.Sprintf(" -P \"%s\"", password))
	}
	if clientId != "" {
		sb.WriteString(fmt.Sprintf(" -i \"%s\"", clientId))
	}
	return sb.String()
}

type MQTTParams struct {
	Host     string
	Port     string
	Username string
	Password string
	Topic    string
	Payload  string
	ClientId string
}

// 解析mosquitto_pub命令
// mosquitto_pub -h xx.xx.xx.xx -p 1883 -t "devices/telemetry" -m "{\"tems\":112}" -u "xxxxx" -P "xxxxx" -i "0"
func ParseMosquittoPubCommand(command string) (*MQTTParams, error) {
	args := strings.Split(command, " ")

	// 检查命令是否为 "mosquitto_pub"
	if args[0] != "mosquitto_pub" {
		return nil, fmt.Errorf("invalid command: %s", args[0])
	}

	// 去掉 "mosquitto_pub"
	args = args[1:]

	f := flag.NewFlagSet("mqtt", flag.ContinueOnError)

	host := f.String("h", "localhost", "MQTT 服务器地址")
	port := f.String("p", "1883", "MQTT 服务器端口")
	user := f.String("u", "", "用户名")
	password := f.String("P", "", "密码")
	topic := f.String("t", "", "MQTT 主题")
	message := f.String("m", "", "要发布的消息内容")
	clientId := f.String("i", "", "客户端ID")

	if *clientId == "" || *clientId == "0" {
		c := "mosquitto_pub_" + uuid.New()[0:8]
		clientId = &c
	}
	err := f.Parse(args)
	if err != nil {
		return nil, err
	}
	// 手动去除参数值两侧的引号和一层转义符
	*host = strings.Trim(*host, "\"")
	*port = strings.Trim(*port, "\"")
	*user = strings.Trim(*user, "\"")
	*password = strings.Trim(*password, "\"")
	*topic = strings.Trim(*topic, "\"")
	*message, err = strconv.Unquote("\"" + strings.Trim(*message, "\"") + "\"")
	if err != nil {
		return nil, err
	}
	*clientId = strings.Trim(*clientId, "\"")

	params := &MQTTParams{
		Host:     *host,
		Port:     *port,
		Username: *user,
		Password: *password,
		Topic:    *topic,
		Payload:  *message,
		ClientId: *clientId,
	}

	return params, nil
}
