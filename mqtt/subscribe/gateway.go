package subscribe

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants"
)

var pool *ants.Pool

type SubscribeTopic struct {
	Topic    string
	Qos      byte
	Callback mqtt.MessageHandler
}
