package dataService

import (
	"flag"
	"fmt"
	"log"
	"os"

	cm "ThingsPanel-Go/modules/dataService/mqtt"
	"ThingsPanel-Go/modules/dataService/tcp"
	"ThingsPanel-Go/services"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/panjf2000/ants/v2"
	"github.com/spf13/viper"
)

func init() {

	loadConfig()
	listenMQTT()
	listenTCP()
}

func loadConfig() {
	log.Println("read config")
	var err error
	envConfigFile := flag.String("config", "./modules/dataService/config.yml", "path of configuration file")
	flag.Parse()
	viper.SetConfigFile(*envConfigFile)
	if err = viper.ReadInConfig(); err != nil {
		fmt.Println("FAILURE", err)
		return
	}
	return
}

func listenMQTT() {
	var TSKVS services.TSKVService
	mqttHost := os.Getenv("TP_MQTT_HOST")
	if mqttHost == "" {
		mqttHost = viper.GetString("mqtt.broker")
	}
	broker := mqttHost
	clientid := viper.GetString("mqtt.clientid")
	user := viper.GetString("mqtt.user")
	pass := viper.GetString("mqtt.pass")
	p, _ := ants.NewPool(1000)

	cm.Listen(broker, user, pass, clientid, func(m mqtt.Message) {
		_ = p.Submit(func() {
			TSKVS.MsgProc(m.Payload())
		})
	})
}

func listenTCP() {
	tcpPort := viper.GetString("tcp.port")
	log.Printf("config of tcp port -- %s", tcpPort)
	tcp.Listen(tcpPort)
}
