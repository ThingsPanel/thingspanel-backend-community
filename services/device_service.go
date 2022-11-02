package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	cm "ThingsPanel-Go/modules/dataService/mqtt"
	tphttp "ThingsPanel-Go/others/http"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/viper"

	"github.com/beego/beego/v2/core/logs"
	simplejson "github.com/bitly/go-simplejson"
	"gorm.io/gorm"
)

type DeviceService struct {
}

// Token 获取设备token
func (*DeviceService) Token(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &device, result.RowsAffected
}

// GetDevicesByAssetID 获取设备列表
func (*DeviceService) GetDevicesByAssetID(asset_id string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&models.Device{}).Where("asset_id = ?", asset_id).Find(&devices)
	psql.Mydb.Model(&models.Device{}).Where("asset_id = ?", asset_id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// GetDevicesByAssetID 获取设备列表(business_id string, device_id string, asset_id string, current int, pageSize int,device_type string)
func (*DeviceService) PageGetDevicesByAssetID(business_id string, asset_id string, device_id string, current int, pageSize int, device_type string, token string, name string) ([]map[string]interface{}, int64) {
	sqlWhere := `select (with RECURSIVE ast as 
		( 
		(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select  name from ast where parent_id='0' limit 1) 
		as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,
		   d."token" as device_token,d."type" as device_type,d.protocol as protocol ,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
		   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1 `
	sqlWhereCount := `select count(1) from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1`
	var values []interface{}
	var where = ""
	if business_id != "" {
		values = append(values, business_id)
		where += " and b.id = ?"
	}
	if asset_id != "" {
		values = append(values, asset_id)
		where += " and a.id = ?"
	}
	if device_id != "" {
		values = append(values, device_id)
		where += " and d.id = ?"
	}
	if device_type != "" {
		values = append(values, device_type)
		where += " and d.type = ?"
	}
	if token != "" {
		values = append(values, token)
		where += " and d.token = ?"
	}
	if name != "" {
		where += " and d.name like '%" + name + "%'"
	}
	sqlWhere += where
	sqlWhereCount += where
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	var offset int = (current - 1) * pageSize
	var limit int = pageSize
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	}
	return deviceList, count
}

// GetDevicesByAssetID 获取设备列表(business_id string, device_id string, asset_id string, current int, pageSize int,device_type string)
func (*DeviceService) PageGetDevicesByAssetIDTree(req valid.DevicePageListValidate) ([]map[string]interface{}, int64) {
	sqlWhere := `select (with RECURSIVE ast as 
		( 
		(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select  name from ast where parent_id='0' limit 1) 
		as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type as device_type,d.parent_id as parent_id,d.protocol_config as protocol_config,
		   d."token" as device_token,d."type" as "type",d.protocol as protocol ,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
		   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1  and d.device_type != '3'`
	sqlWhereCount := `select count(1) from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1 and d.device_type != '3'`
	var values []interface{}
	var where = ""
	if req.BusinessId != "" {
		values = append(values, req.BusinessId)
		where += " and b.id = ?"
	}
	if req.AssetId != "" {
		values = append(values, req.AssetId)
		where += " and a.id = ?"
	}
	if req.DeviceId != "" {
		values = append(values, req.DeviceId)
		where += " and d.id = ?"
	}
	if req.DeviceType != "" {
		values = append(values, req.DeviceType)
		where += " and d.type = ?"
	}
	if req.Token != "" {
		values = append(values, req.Token)
		where += " and d.token = ?"
	}
	if req.Name != "" {
		where += " and d.name like '%" + req.Name + "%'"
	}
	sqlWhere += where
	sqlWhereCount += where
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	var offset int = (req.CurrentPage - 1) * req.PerPage
	var limit int = req.PerPage
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
	} else {
		for _, device := range deviceList {
			fmt.Println("=====================================")
			fmt.Println(device)
			fmt.Println(device["device_type"])
			if device["device_type"].(string) == "2" { // 网关设备需要查询子设备
				var subDeviceList []map[string]interface{}
				sql := `select (with RECURSIVE ast as 
					( 
					(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
					union  
					(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
					)select  name from ast where parent_id='0' limit 1) 
					as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type  as device_type,d.parent_id as parent_id,d.protocol_config as protocol_config,
					   d."token" as device_token,d."type" as "type",d.protocol as protocol ,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
					   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1  and d.device_type = '3' and d.parent_id = '` + device["device"].(string) + `'`
				result := psql.Mydb.Raw(sql).Scan(&subDeviceList)
				if result.Error != nil {
					errors.Is(result.Error, gorm.ErrRecordNotFound)
				} else {
					device["children"] = subDeviceList
				}
			}
		}
	}
	return deviceList, count
}

// GetDevicesByBusinessID 根据业务ID获取设备列表
// return []设备,设备数量
// 2022-04-18新增
func (*DeviceService) GetDevicesByBusinessID(business_id string) ([]models.Device, int64) {
	var devices []models.Device
	SQL := `select device.id,device.asset_id ,device.additional_info,device."type" ,device."location",device."d_id",device."name",device."label",device.protocol from device left join asset on device.asset_id  = asset.id where asset.business_id =?`
	if err := psql.Mydb.Raw(SQL, business_id).Scan(&devices).Error; err != nil {
		log.Println(err.Error())
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, int64(len(devices))
}

// GetDevicesByBusinessID 根据业务ID获取设备列表
// return []设备,设备数量
// 2022-04-18新增
func (*DeviceService) GetDevicesInfoAndCurrentByAssetID(asset_id string) ([]models.Device, int64) {
	var devices []models.Device
	SQL := `select device.id,device.asset_id ,device.additional_info,device."type" ,device."location",device."d_id",device."name",device."label",device.protocol from device left join asset on device.asset_id  = asset.id where asset.id =?`
	if err := psql.Mydb.Raw(SQL, asset_id).Scan(&devices).Error; err != nil {
		log.Println(err.Error())
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, int64(len(devices))
}

// GetDevicesByAssetIDs 获取设备列表
func (*DeviceService) GetDevicesByAssetIDs(asset_ids []string) (devices []models.Device, err error) {
	err = psql.Mydb.Model(&models.Device{}).Where("asset_id IN ?", asset_ids).Find(&devices).Error
	if err != nil {
		return devices, err
	}
	return devices, nil
}

// GetAllDevicesByID 获取所有设备
func (*DeviceService) GetAllDeviceByID(id string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Find(&devices)
	psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// GetDevicesByID 获取设备
func (*DeviceService) GetDeviceByID(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &device, result.RowsAffected
}

// Delete 根据ID删除Device
func (*DeviceService) Delete(id string) bool {
	var device models.Device
	psql.Mydb.Where("id = ?", id).First(&device)
	result := psql.Mydb.Where("id = ?", id).Delete(&models.Device{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	if device.Token != "" {
		redis.DelKey("token" + device.Token)
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
		tphttp.Delete("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{}")
	}
	return true
}

// 获取全部Device
func (*DeviceService) All() ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&devices).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, count
}

// 判断token是否存在
func (*DeviceService) IsToken(token string) bool {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&devices).Where("token = ?", token).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return int(count) > 0
}

// 根据ID编辑Device
func (*DeviceService) ScriptIdEdit(deviceModel valid.EditDevice) error {
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceModel.ID).Update("script_id", "")
	return result.Error
}

// 根据ID编辑Device
func (*DeviceService) PasswordEdit(deviceModel valid.EditDevice, token string) error {
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceModel.ID).Update("password", "")
	if result.Error == nil {
		if token != "" {
			MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
			if MqttHttpHost == "" {
				MqttHttpHost = viper.GetString("api.http_host")
			}
			// mqtt密码制空
			tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+token, "{\"password\":\"\"}")
		}
	}
	return result.Error
}

// 根据ID编辑Device的Token
func (*DeviceService) Edit(deviceModel valid.EditDevice) error {
	var device models.Device
	psql.Mydb.Where("id = ?", deviceModel.ID).First(&device)
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", deviceModel.ID).Updates(models.Device{
		Token:          deviceModel.Token,
		Protocol:       deviceModel.Protocol,
		Port:           deviceModel.Port,
		Publish:        deviceModel.Publish,
		Subscribe:      deviceModel.Subscribe,
		Username:       deviceModel.Username,
		Password:       deviceModel.Password,
		AssetID:        deviceModel.AssetID,
		Type:           deviceModel.Type,
		Name:           deviceModel.Name,
		DeviceType:     deviceModel.DeviceType,
		ParentId:       deviceModel.ParentId,
		ProtocolConfig: deviceModel.ProtocolConfig,
		SubDeviceAddr:  deviceModel.SubDeviceAddr,
		ScriptId:       deviceModel.ScriptId,
		ChartOption:    deviceModel.ChartOption,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}
	if deviceModel.DeviceType == "3" { //子设备
		if deviceModel.SubDeviceAddr != "" {
			var chack_device models.Device
			result := psql.Mydb.Where("parent_id = ? and id!= ?", device.ParentId, device.ID).First(&chack_device) // 检测网关token是否存在
			if result != nil {
				if result.RowsAffected > int64(0) {
					return errors.New("同一个网关下子设备地址不能重复！")
				}
			}
		}
	}
	// 	add: http://127.0.0.1:8083/v1/accounts/
	//  delete: http://127.0.0.1:8083/v1/accounts/
	MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
	if MqttHttpHost == "" {
		MqttHttpHost = viper.GetString("api.http_host")
	}
	if deviceModel.Token != "" {
		logs.Info("token不为空")
		// 原token不为空的时候，删除原token
		if device.Token != "" {
			redis.DelKey("token" + device.Token)
			tphttp.Delete("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{}")
		}
		redis.SetStr("token"+deviceModel.Token, deviceModel.ID, 3600*time.Second)
		// 新增mqtt的token
		var password string
		// 新密码不为空时用新密码，否则用原密码（原密码可为空）
		if deviceModel.Password != "" {
			password = deviceModel.Password
		} else {
			password = device.Password
		}
		tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+deviceModel.Token, "{\"password\":\""+password+"\"}")
	}
	return nil
}

func (*DeviceService) Add(device models.Device) (bool, string) {

	var uuid = uuid.GetUuid()
	device.ID = uuid
	result := psql.Mydb.Create(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	if device.Token != "" {
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
		redis.SetStr("token"+device.Token, uuid, 3600*time.Second)
		tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\"\"}")
	}

	return true, uuid
}

// 向mqtt发送控制指令
func (*DeviceService) OperatingDevice(deviceId string, field string, value interface{}) error {
	//reqMap := make(map[string]interface{})
	valueMap := make(map[string]interface{})
	logs.Info("通过设备id获取设备token")
	var DeviceService DeviceService
	device, _ := DeviceService.Token(deviceId)
	if device == nil {
		logs.Info("没有匹配的token")
		return errors.New("没有匹配的设备")
	}
	valueMap[field] = value
	mjson, _ := json.Marshal(valueMap)
	err := DeviceService.SendMessage(mjson, device)
	return err

}
func (*DeviceService) SendMessage(msg []byte, device *models.Device) error {
	var err error
	if device.DeviceType == "1" { // 直连设备
		// 直连脚本
		if device.ScriptId != "" {
			var tp_script models.TpScript
			result_script := psql.Mydb.Where("id = ? and protocol_type = 'mqtt'", device.ScriptId).First(&tp_script)
			if result_script.Error == nil {
				req_str, err := utils.ScriptDeal(tp_script.ScriptContentB, string(msg), viper.GetString("mqtt.topicToPublish")+"/"+device.Token)
				if err == nil {
					var req_map map[string]interface{}
					err := json.Unmarshal([]byte(req_str), &req_map)
					if err == nil {
						logs.Info(req_map)
						req, err := json.Marshal(&req_map)
						msg = req
						if err != nil {
							return err
						}
					}
				}
			}
		}
		logs.Info("--------------准备发到设备", string(msg))
		err = cm.Send(msg, device.Token)
	} else if device.DeviceType == "3" { // 网关子设备
		if device.ParentId != "" && device.SubDeviceAddr != "" {
			var gatewayDevice *models.Device
			result := psql.Mydb.Where("id = ?", device.ParentId).First(&gatewayDevice) // 检测网关token是否存在
			if result.Error == nil {
				var msgMapValues = make(map[string]interface{})
				json.Unmarshal(msg, &msgMapValues)
				var subMap = make(map[string]interface{})
				subMap[device.SubDeviceAddr] = msgMapValues
				// var msgMap = make(map[string]interface{})
				// msgMap["token"] = gatewayDevice.Token
				// msgMap["values"] = subMap
				msgBytes, _ := json.Marshal(subMap)
				// 网关脚本
				logs.Info(device.ScriptId)
				if gatewayDevice.ScriptId != "" {
					var tp_script models.TpScript
					result_script := psql.Mydb.Where("id = ? and protocol_type = 'MQTT'", gatewayDevice.ScriptId).First(&tp_script)
					if result_script.Error == nil {
						logs.Info("存在网关脚本")
						req_str, err := utils.ScriptDeal(tp_script.ScriptContentB, string(msgBytes), viper.GetString("mqtt.gateway_topic")+"/"+device.Token)
						if err == nil {
							var req_map map[string]interface{}
							err := json.Unmarshal([]byte(req_str), &req_map)
							if err == nil {
								logs.Info(req_map)
								m_ytes, err := json.Marshal(&req_map)
								msgBytes = m_ytes
								if err != nil {
									logs.Info(err.Error)
								}
							} else {
								logs.Info(err.Error)
							}
						} else {
							logs.Info(err.Error)
						}
					} else {
						logs.Info(result_script.Error)
					}
				}
				logs.Info("----------------")
				logs.Info(string(msgBytes))
				err = cm.SendGateWay(msgBytes, gatewayDevice.Token, gatewayDevice.Protocol)
			}
		} else {
			return errors.New("子设备网关不存在或子设备地址为空！")
		}

	}
	if err == nil {
		logs.Info("发送到mqtt成功")
		return nil
	} else {
		logs.Info(err.Error())
		return err
	}
}

//自动化发送控制
func (*DeviceService) ApplyControl(res *simplejson.Json, rule_id string) {
	logs.Info("执行控制开始")
	//"apply":[{"asset_id":"xxx","field":"hum","device_id":"xxx","value":"1"}]}
	applyRows, _ := res.Get("apply").Array()
	logs.Info("applyRows-", applyRows)
	for _, applyRow := range applyRows {
		logs.Info("applyRow-", applyRow)
		if applyMap, ok := applyRow.(map[string]interface{}); ok {
			logs.Info(applyMap)
			// 如果有“或者，并且”操作符，就给code加上操作符
			if applyMap["field"] != nil && applyMap["value"] != nil {
				logs.Info("准备执行控制发送函数")
				var s = ""
				switch applyMap["value"].(type) {
				case string:
					s = applyMap["value"].(string)
				case json.Number:
					s = applyMap["value"].(json.Number).String()
				}
				ConditionsLog := models.ConditionsLog{
					DeviceId:      applyMap["device_id"].(string),
					OperationType: "3",
					Instruct:      applyMap["field"].(string) + ":" + s,
					ProtocolType:  "mqtt",
					CteateTime:    time.Now().Format("2006-01-02 15:04:05"),
					Remark:        rule_id,
				}
				var DeviceService DeviceService
				err := DeviceService.OperatingDevice(applyMap["device_id"].(string), applyMap["field"].(string), applyMap["value"])
				if err == nil {
					logs.Info("成功发送控制")
					ConditionsLog.SendResult = "1"
				} else {
					logs.Info("发送控制失败")
					ConditionsLog.SendResult = "2"
				}
				// 记录日志
				var ConditionsLogService ConditionsLogService
				ConditionsLogService.Insert(&ConditionsLog)
			}
		}
	}
}

// func (*DeviceService) ApplyControl(res *simplejson.Json) {
// 	logs.Info("执行控制开始")
// 	//"apply":[{"asset_id":"xxx","field":"hum","device_id":"xxx","value":"1"}]}
// 	applyRows, _ := res.Get("apply").Array()
// 	logs.Info("applyRows-", applyRows)
// 	for _, applyRow := range applyRows {
// 		logs.Info("applyRow-", applyRow)
// 		if applyMap, ok := applyRow.(map[string]interface{}); ok {
// 			logs.Info(applyMap)
// 			// 如果有“或者，并且”操作符，就给code加上操作符
// 			if applyMap["field"] != nil && applyMap["value"] != nil {
// 				logs.Info("准备执行控制发送函数")
// 				var s = ""
// 				switch applyMap["value"].(type) {
// 				case string:
// 					s = applyMap["value"].(string)
// 				case json.Number:
// 					s = applyMap["value"].(json.Number).String()
// 				}
// 				ConditionsLog := models.ConditionsLog{
// 					DeviceId:      applyMap["device_id"].(string),
// 					OperationType: "3",
// 					Instruct:      applyMap["field"].(string) + ":" + s,
// 					ProtocolType:  "mqtt",
// 					CteateTime:    time.Now().Format("2006-01-02 15:04:05"),
// 				}
// 				var DeviceService DeviceService
// 				reqFlag := DeviceService.OperatingDevice(applyMap["device_id"].(string), applyMap["field"].(string), applyMap["value"])
// 				if reqFlag {
// 					logs.Info("成功发送控制")
// 					ConditionsLog.SendResult = "1"
// 				} else {
// 					logs.Info("成功发送失败")
// 					ConditionsLog.SendResult = "2"
// 				}
// 				// 记录日志
// 				var ConditionsLogService ConditionsLogService
// 				ConditionsLogService.Insert(&ConditionsLog)
// 			}
// 		}
// 	}
// }

// 根据token获取网关设备和子设备的配置
func (*DeviceService) GetConfigByToken(token string) map[string]interface{} {
	var GatewayConfigMap = make(map[string]interface{})
	var device models.Device
	result := psql.Mydb.First(&device, "token = ?", token)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return GatewayConfigMap
	}
	var sub_devices []models.Device
	sub_result := psql.Mydb.Find(&sub_devices, "parent_id = ?", device.ID)
	if sub_result.Error != nil {
		errors.Is(sub_result.Error, gorm.ErrRecordNotFound)
	} else {

		GatewayConfigMap["GatewayId"] = device.ID
		GatewayConfigMap["ProtocolType"] = device.Protocol
		GatewayConfigMap["AccessToken"] = token
		var sub_device_list []map[string]interface{}
		for _, sub_device := range sub_devices {
			var m = make(map[string]interface{})
			err := json.Unmarshal([]byte(sub_device.ProtocolConfig), &m)
			if err != nil {
				fmt.Println("Unmarshal failed:", err)
			}
			sub_device_list = append(sub_device_list, m)
		}
		GatewayConfigMap["SubDevice"] = sub_device_list
		return GatewayConfigMap
	}
	return GatewayConfigMap
}

//修改所有子设备分组
func (*DeviceService) EditSubDeviceAsset(gateway_id string, asset_id string) error {
	var sub_devices []models.Device
	result := psql.Mydb.Raw("UPDATE device SET asset_id = ? WHERE parent_id = ? ", asset_id, gateway_id).Scan(&sub_devices)
	return result.Error
}
