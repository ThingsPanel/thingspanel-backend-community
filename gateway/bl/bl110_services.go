package bl

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 取消订阅
func CancleSubscribe(topic string) {
	token_done := mqtt_client.Unsubscribe(topic)
	if token_done.Wait() && token_done.Error() != nil {
		fmt.Println("Unsubscribe error:", token_done.Error())
	}
}

// 新增订阅
func AddSubscribe(topic string, qos byte) {
	token_done := mqtt_client.Subscribe(topic, qos, subscribeMsgProc)
	if token_done.Wait() && token_done.Error() != nil {
		fmt.Println("Unsubscribe error:", token_done.Error())
	}
}

// 向bl发送消息
func Send(message string, topic string) {
	token := mqtt_client.Publish(topic, 1, false, message)
	if token.Error() != nil {
		fmt.Println(token.Error())
	} else {
		fmt.Println("发送到bl成功：", message)
	}
}

type MessagePayload struct {
	Values map[string]interface{} `json:"values"`
	Token  string                 `json:"Token"`
}

//处理控制
func SendMessage(payload []byte) {
	message := &MessagePayload{}
	if err := json.Unmarshal(payload, message); err != nil {
		fmt.Println("Msg Consumer: Cannot unmarshal msg payload to JSON:", err)
		return
	}
	subscribeList := strings.Split(Bl110_config.Mqtt.TopicToSubscribe, "||")
	topicToPublish := strings.Split(Bl110_config.Mqtt.TopicToPublish, "||")
	tokenList := strings.Split(message.Token, "/")
	if len(tokenList) > 0 {
		for i, subscribe := range subscribeList {
			flag := tokenList[len(tokenList)-1]

			if len(subscribe) <= len(message.Token)-len(flag) {
				ss := message.Token[0 : len(message.Token)-len(flag)-1]
				fmt.Println(ss)
				if subscribe == ss {
					fmt.Println("是BL110的规则")
					//是BL110的规则
					var payloadInterface = make(map[string]interface{})
					payloadInterface["down"] = "down"
					var dataMap [1]map[string]interface{}
					dataMap[0] = map[string]interface{}{"flag": flag}
					for key, value := range message.Values {
						dataMap[0][key] = value
					}
					payloadInterface["sensorDatas"] = dataMap
					topic := topicToPublish[i]
					newPayload, toErr := json.Marshal(payloadInterface)
					if toErr != nil {
						fmt.Println("JSON 编码失败：", toErr)
					}
					Send(string(newPayload), topic)

				}
			}
		}
	}
}
