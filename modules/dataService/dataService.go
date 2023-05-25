package dataService

import (
	"ThingsPanel-Go/modules/dataService/mqtt"
	"ThingsPanel-Go/modules/dataService/tcp"
	tphttp "ThingsPanel-Go/others/http"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/spf13/viper"
)

func init() {
	loadConfig()
	log.Println("注册mqtt用户...")
	reg_mqtt_root()
	log.Println("注册mqtt用户完成")
	log.Println("链接mqtt服务...")
	listenMQTTNew()
	log.Println("链接mqtt服务完成")
	// listenTCP()
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
}

func listenMQTTNew() {
	mqttHost := os.Getenv("TP_MQTT_HOST")
	if mqttHost == "" {
		mqttHost = viper.GetString("mqtt.broker")
	}
	broker := mqttHost
	user := viper.GetString("mqtt.user")
	pass := viper.GetString("mqtt.pass")
	mqtt.ListenNew(broker, user, pass)
}

// 废弃
func ListenTCP() {
	tcpPort := viper.GetString("tcp.port")
	log.Printf("config of tcp port -- %s", tcpPort)
	tcp.Listen(tcpPort)
}
func reg_mqtt_root() {
	MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
	if MqttHttpHost == "" {
		MqttHttpHost = viper.GetString("api.http_host")
	}
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
