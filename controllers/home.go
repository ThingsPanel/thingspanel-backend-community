package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/spf13/viper"
)

type HomeController struct {
	beego.Controller
}

type HomeList struct {
	CpuUsage string `json:"cpu_usage"`
	MemUsage string `json:"mem_usage"`
	Device   int64  `json:"device"`
	Msg      int64  `json:"msg"`
}

type HomeDevice struct {
	Business   int64 `json:"business"`
	Assets     int64 `json:"assets"`
	Equipment  int64 `json:"equipment"`
	Dashboard  int64 `json:"dashboard"`
	Conditions int64 `json:"conditions"`
}

// 首页数据统计
func (this *HomeController) List() {
	var ResourcesService services.ResourcesService
	r := ResourcesService.GetNew()
	var DeviceService services.DeviceService
	_, dc := DeviceService.All()
	var TSKVService services.TSKVService
	_, tc := TSKVService.All()
	u := HomeList{
		CpuUsage: r.CPU,
		MemUsage: r.MEM,
		Device:   dc,
		Msg:      tc,
	}
	response.SuccessWithDetailed(200, "success", u, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 首页报表 chart
func (this *HomeController) Chart() {
	var ResourcesService services.ResourcesService
	nr := ResourcesService.GetNewResource("cpu")
	response.SuccessWithDetailed(200, "success", nr, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 首页展示设备 show
func (this *HomeController) Show() {
	mqttHost := os.Getenv("TP_MQTT_HOST")
	//验证设备ID
	homeShowValidate := valid.HomeShowValidate{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &homeShowValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(homeShowValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(homeShowValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	//通过id获取设备
	var DeviceService services.DeviceService
	d, _ := DeviceService.GetDeviceByID(homeShowValidate.ID)
	//读取配置参数
	if viper.GetString("mqtt.broker") == "" {
		var readErr error
		envConfigFile := flag.String("config", "./modules/dataService/config.yml", "path of configuration file")
		flag.Parse()
		viper.SetConfigFile(*envConfigFile)
		if readErr = viper.ReadInConfig(); readErr != nil {
			fmt.Println("FAILURE", err)
		} else {
			if d.Token == "" {
				d.Token = response.GetUuid()
			}
			d.Publish = viper.GetString("mqtt.topicToPublish")
			d.Subscribe = viper.GetString("mqtt.topicToSubscribe")
			if mqttHost == "" {
				d.Port = strings.Split(viper.GetString("mqtt.broker"), ":")[1]
			} else {
				d.Port = strings.Split(mqttHost, ":")[1]
			}
			d.Username = viper.GetString("mqtt.user")
			d.Password = viper.GetString("mqtt.pass")
		}
	} else {
		if d.Token == "" {
			d.Token = response.GetUuid()
		}
		d.Publish = viper.GetString("mqtt.topicToPublish")
		d.Subscribe = viper.GetString("mqtt.topicToSubscribe")
		if mqttHost == "" {
			d.Port = strings.Split(viper.GetString("mqtt.broker"), ":")[1]
		} else {
			d.Port = strings.Split(mqttHost, ":")[1]
		}
		d.Username = viper.GetString("mqtt.user")
		d.Password = viper.GetString("mqtt.pass")
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}

// 默认配置获取
func (HomeController *HomeController) GetDefaultSetting() {
	mqttHost := os.Getenv("TP_MQTT_HOST")
	//验证设备ID
	ProtocolValidate := valid.ProtocolValidate{}
	err := json.Unmarshal(HomeController.Ctx.Input.RequestBody, &ProtocolValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(ProtocolValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(ProtocolValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(HomeController.Ctx))
			break
		}
		return
	}
	//读取配置参数
	d := make(map[string]string)
	var port string
	if ProtocolValidate.Protocol == "mqtt" { //mqtt直连设备
		if mqttHost == "" {
			port = strings.Split(viper.GetString("mqtt.broker"), ":")[1]
		} else {
			port = strings.Split(mqttHost, ":")[1]
		}
		d["default_setting"] =
			"MQTT接入点: " + viper.GetString("url") + ":" + port +
				"$$  -设备发布主题: " + viper.GetString("mqtt.topicToSubscribe") +
				"$$  -设备订阅主题: " + viper.GetString("mqtt.topicToPublish") + "/{AccessToken}" +
				"$$  -其他主题参阅详细文档" +
				"$$  -MQTT用户名: AccessToken;必须使用用户名才可以连接成功" +
				"$$  -举例:" +
				"$$    -上报属性规范->{key1:value1, key2:value2 ...}" +
				"$$    -例如--------->{\"temp\":18.5, \"hum\":40}"
	} else if ProtocolValidate.Protocol == "tcp" {
		d["default_setting"] = "端口:" + strings.Split(viper.GetString("tcp.port"), ":")[1] + "$$协议:" + "https://forum.thingspanel.cn/assets/files/2022-06-21/1655774183-644926-thingspanel-tcpv114xlsx.zip"
	} else if ProtocolValidate.Protocol == "MODBUS_RTU" || ProtocolValidate.Protocol == "MODBUS_TCP" {
		d["default_setting"] = "协议端口:" + strings.Split(viper.GetString("plugin.http_host"), ":")[1] + "$$连接:建立tcp连接时，将AccessToken上送。"
	} else if ProtocolValidate.Protocol == "MQTT" { //mqtt网关设备
		d["default_setting"] =
			"MQTT接入点: " + viper.GetString("url") + ":" + port +
				"$$  -网关设备发布主题: " + viper.GetString("mqtt.gateway_topic") +
				"$$  -网关设备订阅主题: " + viper.GetString("mqtt.gateway_topic") + "/{Token}" +
				"$$  -其他主题参阅详细文档" +
				"$$  -MQTT用户名: AccessToken;必须使用用户名才可以连接成功" +
				"$$  -举例:" +
				"$$    -上报属性规范(sub_device_addr为子设备的设备地址)->{sub_device_addr:{key:value...},sub_device_addr:{key:value...}...}" +
				"$$    -例如(a2js34和csjs45为设备地址)------------------>{\"a2js34\":{\"temp\":18.5, \"hum\":40},\"csjs45\":{\"temp\":19.5, \"hum\":45}}"
	}
	d["Token"] = response.GetUuid()
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(HomeController.Ctx))
}

// Device
func (this *HomeController) Device() {
	var BusinessService services.BusinessService
	_, bc := BusinessService.All()
	var AssetService services.AssetService
	_, ac := AssetService.All()
	var DeviceService services.DeviceService
	_, dc := DeviceService.All()
	var DashBoardService services.DashBoardService
	_, dac := DashBoardService.All()
	var ConditionsService services.ConditionsService
	_, cc := ConditionsService.All()
	d := HomeDevice{
		Business:   bc,
		Assets:     ac,
		Equipment:  dc,
		Dashboard:  dac,
		Conditions: cc,
	}
	response.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
