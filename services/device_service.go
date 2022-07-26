package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	cm "ThingsPanel-Go/modules/dataService/mqtt"
	uuid "ThingsPanel-Go/utils"
	"encoding/json"
	"errors"
	"log"
	"time"

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
	var values []interface{}
	if business_id != "" {
		values = append(values, business_id)
		sqlWhere += " and b.id = ?"
	}
	if asset_id != "" {
		values = append(values, asset_id)
		sqlWhere += " and a.id = ?"
	}
	if device_id != "" {
		values = append(values, device_id)
		sqlWhere += " and d.id = ?"
	}
	if device_type != "" {
		values = append(values, device_type)
		sqlWhere += " and d.type = ?"
	}
	if token != "" {
		values = append(values, token)
		sqlWhere += " and d.token = ?"
	}
	if name != "" {
		sqlWhere += " and d.name like '%" + name + "%'"
	}
	var count int64
	result := psql.Mydb.Raw(sqlWhere, values...).Count(&count)
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
	redis.DelKey("token" + device.Token)
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

// 根据ID编辑Device的Token
func (*DeviceService) Edit(id string, token string, protocol string, port string, publish string, subscribe string, username string, password string) bool {
	var device models.Device
	psql.Mydb.Where("id = ?", id).First(&device)
	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Updates(map[string]interface{}{
		"token":     token,
		"protocol":  protocol,
		"port":      port,
		"publish":   publish,
		"subscribe": subscribe,
		"username":  username,
		"password":  password,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	redis.DelKey("token" + device.Token)
	redis.SetStr("token"+token, id, 3600*time.Second)
	return true
}

func (*DeviceService) Add(token string, protocol string, port string, publish string, subscribe string, username string, password string) (bool, string) {
	var uuid = uuid.GetUuid()
	device := models.Device{
		ID:        uuid,
		Token:     token,
		Protocol:  protocol,
		Port:      port,
		Publish:   publish,
		Subscribe: subscribe,
		Username:  username,
		Password:  password,
	}
	result := psql.Mydb.Create(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	redis.SetStr("token"+token, uuid, 3600*time.Second)
	return true, uuid
}

// 向mqtt发送控制指令
func (*DeviceService) OperatingDevice(deviceId string, field string, value interface{}) bool {
	reqMap := make(map[string]interface{})
	valueMap := make(map[string]interface{})
	logs.Info("通过设备id获取设备token")
	var DeviceService DeviceService
	device, _ := DeviceService.Token(deviceId)
	if device != nil {
		reqMap["token"] = device.Token
		logs.Info("token-%s", device.Token)
	} else {
		logs.Info("没有匹配的token")
		return false
	}
	logs.Info("把field字段映射回设备端字段")
	var fieldMappingService FieldMappingService
	deviceField := fieldMappingService.TransformByDeviceid(deviceId, field)
	if deviceField != "" {
		valueMap[deviceField] = value
	}
	reqMap["values"] = valueMap
	logs.Info("将map转json")
	mjson, _ := json.Marshal(reqMap)
	logs.Info("json-%s", string(mjson))
	err := cm.Send(mjson)
	if err == nil {
		logs.Info("发送到mqtt成功")
		return true
	} else {
		logs.Info(err.Error())
		return false
	}
}

//自动化发送控制
func (*DeviceService) ApplyControl(res *simplejson.Json) {
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
				}
				var DeviceService DeviceService
				reqFlag := DeviceService.OperatingDevice(applyMap["device_id"].(string), applyMap["field"].(string), applyMap["value"])
				if reqFlag {
					logs.Info("成功发送控制")
					ConditionsLog.SendResult = "1"
				} else {
					logs.Info("成功发送失败")
					ConditionsLog.SendResult = "2"
				}
				// 记录日志
				var ConditionsLogService ConditionsLogService
				ConditionsLogService.Insert(&ConditionsLog)
			}
		}
	}
}
