package dataService

import (
	"ThingsPanel-Go/modules/dataService/mqtt"
	mqtts "ThingsPanel-Go/modules/dataService/mqtts/connect"
	tphttp "ThingsPanel-Go/others/http"
	"io/ioutil"
	"log"

	"github.com/spf13/viper"
)

func Init() {
	log.Println("注册mqtt用户...")
	//是否gmqtt
	if viper.GetString("mqtt_server") == "gmqtt" {
		reg_mqtt_root()
	}
	log.Println("注册mqtt用户完成")
	log.Println("链接mqtt服务...")
	listenMQTTNew()
	log.Println("链接mqtt服务完成")
	// log.Println("连接mqtt over tls服务...")
	// listenMQTTS()
	// log.Println("连接mqtt over tls完成...")
	// listenTCP()
}

func listenMQTTNew() {
	broker := viper.GetString("mqtt.broker")
	user := viper.GetString("mqtt.user")
	pass := viper.GetString("mqtt.pass")
	go mqtt.ListenNew(broker, user, pass)
}

func listenMQTTS() {
	broker := viper.GetString("mqtts.broker")
	user := viper.GetString("mqtts.user")
	pass := viper.GetString("mqtts.pass")
	caPath := viper.GetString("mqtts.caPath")
	crtPath := viper.GetString("mqtts.crtPath")
	keyPath := viper.GetString("mqtts.keyPath")
	mqtts.Connect(broker, user, pass, caPath, crtPath, keyPath)
}

// 废弃
func ListenTCP() {
	tcpPort := viper.GetString("tcp.port")
	log.Printf("config of tcp port -- %s", tcpPort)
	//tcp.Listen(tcpPort)
}
func reg_mqtt_root() {

	MqttHttpHost := viper.GetString("api.http_host")
	resps, errs := tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/root", "{\"password\":\""+viper.GetString("mqtt.pass")+"\"}")
	if errs != nil {
		log.Println("失败:", errs.Error())
	} else {
		defer resps.Body.Close()
		if resps.StatusCode == 200 {
			body, errs := ioutil.ReadAll(resps.Body)
			if errs != nil {
				log.Println("失败:", errs.Error())
			} else {
				log.Println("注册成功: ", string(body))
			}
		} else {
			log.Println("Get failed with error:" + resps.Status)
		}
	}
}
