package dataService

import (
	"flag"
	"fmt"
	"log"

	cm "ThingsPanel-Go/modules/dataService/mqtt"
	"ThingsPanel-Go/modules/dataService/tcp"
	"ThingsPanel-Go/services"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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
	broker := viper.GetString("mqtt.broker")
	clientid := viper.GetString("mqtt.clientid")
	user := viper.GetString("mqtt.user")
	pass := viper.GetString("mqtt.pass")
	cm.Listen(broker, user, pass, clientid, func(m mqtt.Message) {
		go TSKVS.MsgProc(m.Payload())
	})
}

func listenTCP() {
	tcpPort := viper.GetString("tcp.port")
	log.Printf("config of tcp port -- %s", tcpPort)
	tcp.Listen(tcpPort)
}
