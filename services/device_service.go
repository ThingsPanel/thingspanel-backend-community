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
	"reflect"
	"strings"
	"time"

	"github.com/spf13/cast"
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

// Token 获取设备token
func (*DeviceService) GetSubDeviceCount(parentId string) (int64, error) {
	var count int64
	result := psql.Mydb.Model(models.Device{}).Where("parent_id = ?", parentId).Count(&count)
	return count, result.Error
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
		values = append(values, fmt.Sprintf("%%%s%%", name))
		where += " and d.name like ?"
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
func (*DeviceService) PageGetDevicesByAssetIDTree(req valid.DevicePageListValidate, tenantId string) ([]map[string]interface{}, int64) {
	sqlWhere := `select (with RECURSIVE ast as 
		( 
		(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select  name from ast where parent_id='0' limit 1) 
		as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type as device_type,d.parent_id as parent_id,d.protocol_config as protocol_config,
		d.additional_info as additional_info,d.sub_device_addr as sub_device_addr,d."token" as device_token,d."type" as "type",d.protocol as protocol ,dm.model_name as plugin_name,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
		   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id LEFT JOIN device_model dm ON d.type = dm.id where 1=1  and d.device_type != '3'`
	sqlWhereCount := `select count(1) from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where d.device_type != '3'`
	var values []interface{}
	var where = ""
	// 增加租户id查询条件
	values = append(values, tenantId)
	where += " and d.tenant_id = ?"

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
		values = append(values, fmt.Sprintf("%%%s%%", req.Name))
		where += " and d.name like ?"
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
	sqlWhere += "order by d.created_at desc offset ? limit ?"
	values = append(values, offset, limit)
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		logs.Error(dataResult.Error.Error())
	} else {
		for _, device := range deviceList {
			if device["type"].(string) != "" {
				var deviceModelService DeviceModelService
				chartNameList, err := deviceModelService.GetChartNameListByPluginId(device["type"].(string))
				if err == nil {
					device["chart_names"] = chartNameList
				} else {
					logs.Error(err.Error())
				}
			}

			//在线离线状态
			// if device["device_type"].(string) != "3" {
			// 	var interval int64
			// 	if a, ok := device["additional_info"].(string); ok {
			// 		aJson, err := simplejson.NewJson([]byte(a))
			// 		if err == nil {
			// 			thresholdTime, err := aJson.Get("runningInfo").Get("thresholdTime").Int64()

			// 			if err == nil {
			// 				interval = thresholdTime
			// 			}
			// 		}
			// 	}
			// 	var TSKVService TSKVService
			// 	state, err := TSKVService.DeviceOnline(device["device"].(string), interval)
			// 	if err != nil {
			// 		logs.Error(err.Error())
			// 	}
			// 	device["device_state"] = state
			// }
			if device["device_type"].(string) == "2" { // 网关设备需要查询子设备
				var subDeviceList []map[string]interface{}
				sql := `select (with RECURSIVE ast as 
					( 
					(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
					union  
					(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
					)select  name from ast where parent_id='0' limit 1) 
					as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type  as device_type,d.parent_id as parent_id,d.protocol_config as protocol_config,d.sub_device_addr as sub_device_addr,
					d.additional_info as additional_info,d."token" as device_token,d."type" as "type",d.protocol as protocol ,dm.model_name as plugin_name,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
					   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id LEFT JOIN device_model dm ON d.type = dm.id where 1=1  and d.device_type = '3' and d.parent_id = '` + device["device"].(string) + "' order by d.created_at desc"
				result := psql.Mydb.Raw(sql).Scan(&subDeviceList)
				if result.Error != nil {
					errors.Is(result.Error, gorm.ErrRecordNotFound)
				} else {
					for _, subDevice := range subDeviceList {
						if subDevice["type"].(string) != "" {
							var deviceModelService DeviceModelService
							chartNameList, err := deviceModelService.GetChartNameListByPluginId(subDevice["type"].(string))
							if err == nil {
								subDevice["chart_names"] = chartNameList
							} else {
								logs.Error(err.Error())
							}
						}
					}

					device["children"] = subDeviceList
				}
			}
		}
	}
	return deviceList, count
}

// GetDevicesByAssetID 获取设备列表(business_id string, device_id string, asset_id string, current int, pageSize int,device_type string)
func (*DeviceService) AllDeviceList(req valid.DevicePageListValidate) ([]map[string]interface{}, int64) {
	sqlWhere := `select (with RECURSIVE ast as 
		( 
		(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select  name from ast where parent_id='0' limit 1) 
		as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device_id ,d."name" as device_name,d.device_type as device_type,d.parent_id as parent_id,d.protocol_config as protocol_config,
		   d."token" as access_token,d."type" as "type",d.protocol as protocol ,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts,
		   (select name from device dd where dd.device_type = '2' and dd.parent_id = d.id limit 1) as gateway_name
		   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1`
	sqlWhereCount := `select count(1) from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1`
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
		where += " and d.device_type = ?"
	}
	if req.Token != "" {
		values = append(values, req.Token)
		where += " and d.token = ?"
	}
	if req.Name != "" {
		values = append(values, fmt.Sprintf("%%%s%%", req.Name))
		where += " and d.name like ?"
	}
	if req.NotGateway == 1 {
		where += " and d.device_type !='2'"
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
		var TSKVService TSKVService
		for _, deviceData := range deviceList {
			fields := TSKVService.GetCurrentData(deviceData["device_id"].(string), nil)
			if len(fields) == 0 {
				deviceData["values"] = make(map[string]interface{}, 0)
				//deviceData["status"] = "0"
			} else {
				// 0-带接入 1-正常 2-异常
				// var state string
				// tsl, tsc := TSKVService.Status(deviceData["device_id"].(string))
				// if tsc == 0 {
				// 	state = "0"
				// } else {
				// 	ts := time.Now().UnixMicro()
				// 	//300000000
				// 	if (ts - tsl.TS) > 300000000 {
				// 		state = "2"
				// 	} else {
				// 		state = "1"
				// 	}
				// }
				//deviceData["status"] = state
				deviceData["values"] = fields[0]
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

// GetDevicesByProductID 根据产品ID获取设备列表
// return []设备,设备数量
// 2023-03-14新增
func (*DeviceService) GetDevicesByProductID(product_id string) ([]models.Device, int64) {
	var devices []models.Device
	SQL := `select device.id,device.token,device.product_id,device.asset_id ,device.additional_info,device."type" ,device."location",device."d_id",device."name",device."label",device.protocol from device where product_id =?`
	if err := psql.Mydb.Raw(SQL, product_id).Scan(&devices).Error; err != nil {
		log.Println(err.Error())
	}
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	return devices, int64(len(devices))
}

// GetDevicesByProductID 根据产品ID获取设备列表
// return []设备,设备数量
// 2023-03-14新增
func (*DeviceService) DeviceListByProductId(PaginationValidate valid.DevicePaginationValidate) (bool, []map[string]interface{}, int64) {
	sqlWhere := `select d.id as id,d.name as name,d.product_id as product_id,d.current_version as current_version,td.device_code as device_code from device d left join tp_generate_device td on td.device_id=d.id where d.product_id =?`
	sqlWhereCount := `select count(1) from device where product_id =?`
	var values []interface{}
	var where = ""
	values = append(values, PaginationValidate.ProductId)
	if PaginationValidate.CurrentVersion != "" {
		values = append(values, "%"+PaginationValidate.CurrentVersion+"%")
		where += " and d.current_version like ?"
	}
	if PaginationValidate.Name != "" {
		values = append(values, "%"+PaginationValidate.Name+"%")
		where += " and d.name like ?"
	}
	sqlWhere += where
	sqlWhereCount += where
	var deviceList []map[string]interface{}
	var count int64
	result := psql.Mydb.Raw(sqlWhereCount, values...).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, deviceList, 0
	}
	var offset int = (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	var limit int = PaginationValidate.PerPage
	sqlWhere += "order by d.created_at desc offset ? limit ?"
	values = append(values, offset, limit)
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		return false, deviceList, 0
	}
	return true, deviceList, count
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
func (*DeviceService) Delete(id, tenantId string) error {
	var device models.Device
	result := psql.Mydb.Where("id = ? and tenant_id = ?", id, tenantId).First(&device)
	if result.Error != nil {
		return result.Error
	}
	// 如果网关下有子设备，必须先删除子设备
	if device.DeviceType == "2" {
		var count int64
		psql.Mydb.Raw("select count(1) from device where parent_id = ?", device.ID).Count(&count)
		if count > int64(0) {
			return errors.New("请先删除网关设备下的子设备")
		}
	}
	result = psql.Mydb.Where("id = ? and tenant_id = ?", id, tenantId).Delete(&models.Device{})
	if result.Error != nil {
		return result.Error
	}
	if device.Token != "" {
		redis.DelKey("token" + device.Token)
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
		tphttp.Delete("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{}")
	}
	return nil
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

// 根据网关token和子设备地址查询子设备信息
func (*DeviceService) GetDeviceDetailsByParentTokenAndSubDeviceAddr(token string, sub_device_addr string) (models.Device, error) {
	var device models.Device
	result := psql.Mydb.Model(&device).
		Select("d.id, d.token,d.sub_device_addr,d.protocol,d.device_type,d.protocol_config").
		Joins("left join device d on d.parent_id = device.id").
		Where("device.token = ? and d.sub_device_addr = ?", token, sub_device_addr).Scan(&device)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return device, result.Error
	}
	return device, nil
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
	logs.Info("进入设备更新")
	var device models.Device
	psql.Mydb.Where("id = ?", deviceModel.ID).First(&device)
	logs.Info("判断子设备地址")
	if deviceModel.DeviceType == "3" { //子设备
		if deviceModel.SubDeviceAddr != "" {
			var chack_device models.Device
			result := psql.Mydb.Where("parent_id = ? and id!= ?", device.ParentId, device.ID).First(&chack_device) // 检测网关token是否存在
			if result.Error != nil {
				if result.RowsAffected > int64(0) {
					return errors.New("同一个网关下子设备地址不能重复！")
				}
			}
		}
	}
	// 	add: http://127.0.0.1:8083/v1/accounts/
	//  delete: http://127.0.0.1:8083/v1/accounts/

	if deviceModel.Token != "" {
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
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
		_, err := tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+deviceModel.Token, "{\"password\":\""+password+"\"}")
		if err != nil {
			return err
		}

	}
	logs.Info("修改sql")
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
		Location:       deviceModel.Location,
		AdditionalInfo: deviceModel.AdditionalInfo,
		DId:            deviceModel.DId,
	})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return result.Error
	}

	return nil
}

func (*DeviceService) Add(device models.Device) (string, error) {

	var uuid = uuid.GetUuid()
	device.ID = uuid
	if device.ProtocolConfig == "" {
		device.ProtocolConfig = "{}"
	}
	if device.ChartOption == "" {
		device.ChartOption = "{}"
	}
	result := psql.Mydb.Create(&device)
	if result.Error != nil {
		return "", result.Error
	}
	if device.Token != "" && device.Protocol[0:4] != "WVP_" {
		logs.Info("添加gmqtt用户...")
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
		redis.SetStr("token"+device.Token, uuid, 3600*time.Second)
		tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\"\"}")
	}
	return uuid, nil
}

// add1
func (*DeviceService) Add1(device models.Device) (models.Device, error) {
	if device.ID == "" {
		var uuid = uuid.GetUuid()
		device.ID = uuid
	}
	result := psql.Mydb.Create(&device)
	if result.Error != nil {
		return device, result.Error
	}
	if device.Token != "" {
		MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
		if MqttHttpHost == "" {
			MqttHttpHost = viper.GetString("api.http_host")
		}
		redis.SetStr("token"+device.Token, device.ID, 3600*time.Second)
		tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\"\"}")
	}
	return device, nil
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

// 脚本处理
func scriptDealB(script_id string, device_data []byte, topic string) ([]byte, error) {
	if script_id == "" {
		logs.Info("脚本id不存在:", script_id)
		return device_data, nil
	}
	var tp_script models.TpScript
	result_b := psql.Mydb.Where("id = ?", script_id).First(&tp_script)
	if result_b.Error == nil {
		logs.Info("脚本信息存在")
		req_str, err_a := utils.ScriptDeal(tp_script.ScriptContentB, device_data, topic)
		if err_a != nil {
			return device_data, err_a
		} else {
			return []byte(req_str), nil
		}
	} else {
		logs.Info("脚本信息不存在")
		return device_data, nil
	}
}
func (*DeviceService) SendMessage(msg []byte, device *models.Device) error {
	var err error
	if device.DeviceType == "1" { // 直连设备
		// 通过脚本
		msg, err = scriptDealB(device.ScriptId, msg, viper.GetString("mqtt.topicToPublish")+"/"+device.Token)
		if err != nil {
			return err
		}
		if device.Protocol == "mqtt" {
			logs.Info("直连设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
			err = cm.Send(msg, device.Token)
		} else { // 协议设备
			logs.Info("直连协议设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
			//获取协议插件订阅topic
			var TpProtocolPluginService TpProtocolPluginService
			pp := TpProtocolPluginService.GetByProtocolType(device.Protocol, "1")
			var topic = pp.SubTopicPrefix + device.Token
			err = cm.SendPlugin(msg, topic)
		}

	} else if device.DeviceType == "3" && device.Protocol != "MQTT" { // 协议插件子设备
		//暂时对modbus插件做单独处理
		if device.Protocol == "MODBUS_RTU" || device.Protocol == "MODBUS_TCP" { //modbus
			var TpProtocolPluginService TpProtocolPluginService
			pp := TpProtocolPluginService.GetByProtocolType(device.Protocol, "2")
			var topic = pp.SubTopicPrefix + device.ID
			msg, err = scriptDealB(device.ScriptId, msg, topic)
			if err != nil {
				return err
			}
			err = cm.SendPlugin(msg, topic)
		} else { //其他协议插件
			var gatewayDevice *models.Device
			result := psql.Mydb.Where("id = ?", device.ParentId).First(&gatewayDevice) // 检测网关token是否存在
			if result.Error == nil {
				//获取协议插件订阅topic
				var TpProtocolPluginService TpProtocolPluginService
				pp := TpProtocolPluginService.GetByProtocolType(device.Protocol, "2")
				// 前缀+网关设备token
				var topic = pp.SubTopicPrefix + gatewayDevice.Token
				//包装子设备地址
				var msgMapValues = make(map[string]interface{})
				json.Unmarshal(msg, &msgMapValues)
				var subMap = make(map[string]interface{})
				subMap[device.SubDeviceAddr] = msgMapValues
				msgBytes, _ := json.Marshal(subMap)
				// 通过脚本
				msg, err = scriptDealB(device.ScriptId, msgBytes, topic)
				if err != nil {
					return err
				}
				logs.Info("网关设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
				err = cm.SendPlugin(msgBytes, topic)
			}
		}

	} else if device.Protocol == "MQTT" { // mqtt网关子设备
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
				// 通过脚本
				msg, err = scriptDealB(device.ScriptId, msgBytes, viper.GetString("mqtt.gateway_topic")+"/"+device.Token)
				if err != nil {
					return err
				}
				logs.Info("网关设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
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

// 执行控制指令
// res 指令
// rule_id(用来判断上次发送间隔) 指令id
// operation_type 控制类型11-定时触发 2-手动控制 3-自动控制
func (*DeviceService) ApplyControl(res *simplejson.Json, rule_id string, operation_type string) error {
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
				applyField := applyMap["field"].(string)
				applyDeviceId := applyMap["device_id"].(string)

				//根据物模型对值做转换
				var DeviceModelService DeviceModelService
				var applyValue interface{}
				applyValue = applyMap["value"]
				if plugin_id, ok := applyMap["plugin_id"].(string); ok {
					if attributeMap, err := DeviceModelService.GetTypeMapByPluginId(plugin_id); err == nil {
						if attributeMap[applyField] != nil && attributeMap[applyField] != "text" {
							if find := strings.Contains(s, "."); find {
								applyValue = cast.ToFloat64(s)
							} else {
								applyValue = cast.ToInt(s)
							}

						} else {
							s = `"` + s + `"`
						}
					} else {
						logs.Error(err.Error())
					}
				}
				logs.Error(reflect.TypeOf(applyValue))
				ConditionsLog := models.ConditionsLog{
					DeviceId:      applyDeviceId,
					OperationType: "3",
					Instruct:      applyField + ":" + s,
					ProtocolType:  "mqtt",
					CteateTime:    time.Now().Format("2006-01-02 15:04:05"),
					Remark:        rule_id,
				}
				//发送控制
				var DeviceService DeviceService
				err := DeviceService.OperatingDevice(applyDeviceId, applyField, applyValue)
				if err == nil {
					logs.Info("成功发送控制")
					ConditionsLog.SendResult = "1"
				} else {
					logs.Error("发送控制失败:", err)
					ConditionsLog.SendResult = "2"
				}
				// 记录日志
				var ConditionsLogService ConditionsLogService
				ConditionsLogService.Insert(&ConditionsLog)
			}
		} else {
			logs.Error("apply格式错误")
		}
	}
	return nil
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
	var ConfigMap = make(map[string]interface{})
	var device models.Device
	result := psql.Mydb.First(&device, "token = ?", token)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return ConfigMap
	}
	ConfigMap["ProtocolType"] = device.Protocol
	ConfigMap["AccessToken"] = token
	ConfigMap["DeviceType"] = device.DeviceType
	ConfigMap["Id"] = device.ID
	if device.DeviceType == "1" { //直连设备
		var m = make(map[string]interface{})
		err := json.Unmarshal([]byte(device.ProtocolConfig), &m)
		if err != nil {
			fmt.Println("Unmarshal failed:", err)
		}
		ConfigMap["DeviceConfig"] = m
	} else if device.DeviceType == "2" { //网关设备
		var sub_devices []models.Device
		sub_result := psql.Mydb.Find(&sub_devices, "parent_id = ?", device.ID)
		if sub_result.Error != nil {
			errors.Is(sub_result.Error, gorm.ErrRecordNotFound)
		} else {
			var sub_device_list []map[string]interface{}
			for _, sub_device := range sub_devices {
				var m = make(map[string]interface{})
				err := json.Unmarshal([]byte(sub_device.ProtocolConfig), &m)
				if err != nil {
					fmt.Println("Unmarshal failed:", err)
				}
				// 子设备表单中返回子设备token和子设备id
				m["AccessToken"] = sub_device.Token
				m["DeviceId"] = sub_device.ID
				m["SubDeviceAddr"] = sub_device.SubDeviceAddr
				sub_device_list = append(sub_device_list, m)
			}
			ConfigMap["SubDevice"] = sub_device_list
			return ConfigMap
		}
	}
	return ConfigMap
}

//修改所有子设备分组
func (*DeviceService) EditSubDeviceAsset(gateway_id string, asset_id string) error {
	var sub_devices []models.Device
	result := psql.Mydb.Raw("UPDATE device SET asset_id = ? WHERE parent_id = ? ", asset_id, gateway_id).Scan(&sub_devices)
	return result.Error
}

// 业务、分组、设备级联查询
func (*DeviceService) GetDeviceByCascade() ([]map[string]interface{}, error) {
	business_sql := `select b.id as business_id,b.name as business_name from business b order by created_at desc`
	group_sql := `select a.id as group_id,
		(with recursive ast as 
				( 
					(select
						aa.id,
						cast(CONCAT('/', aa.name) as varchar(255))as name,
						aa.parent_id
					from asset aa where id = a.id)
					union  
					(select
						tt.id,
						cast (CONCAT('/', kk.name, tt.name ) as varchar(255))as name ,
						kk.parent_id
					from ast tt inner join asset kk on kk.id = tt.parent_id )
				) select name from ast where parent_id = '0' limit 1
			) as group_name
		from
			asset a
		where
			business_id = ?
		order by
			group_name asc`
	device_sql := `select d.id as device_id,case when gd.name is null  then d.name else '('||gd.name||')'||d.name end as device_name,d.type as plugin_id
		from device d  left join device gd on d.parent_id = gd.id where d.asset_id = ? and d.device_type != '2' 
		order by d.created_at desc`
	var business_map []map[string]interface{}
	result := psql.Mydb.Raw(business_sql).Scan(&business_map)
	if result.Error != nil {
		return nil, result.Error
	}
	for _, business := range business_map {
		var group_map []map[string]interface{}
		result = psql.Mydb.Raw(group_sql, business["business_id"]).Scan(&group_map)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			continue
		}
		business["children"] = group_map
		for _, group := range group_map {
			var device_map []map[string]interface{}
			result = psql.Mydb.Raw(device_sql, group["group_id"]).Scan(&device_map)
			if result.Error != nil {
				logs.Error(result.Error.Error())
				continue
			}
			group["children"] = device_map
		}
	}
	return business_map, nil
}

// GetDevicesByAssetID 获取设备列表(business_id string, device_id string, asset_id string, current int, pageSize int,device_type string)
func (*DeviceService) DeviceMapList(req valid.DeviceMapValidate) ([]map[string]interface{}, error) {
	sqlWhere := `select (with RECURSIVE ast as 
		( 
		(select aa.id,cast(aa.name as varchar(255)),aa.parent_id  from asset aa where id=a.id) 
		union  
		(select tt.id,cast (kk.name||'/'||tt.name as varchar(255))as name ,kk.parent_id from ast tt inner join asset  kk on kk.id = tt.parent_id )
		)select  name from ast where parent_id='0' limit 1) 
		as group_name,b.id as business_id ,b."name" as business_name,d.location,a.id as group_id ,d.id as device_id ,d."name" as device_name,d.device_type as device_type,d.parent_id as parent_id,
		   d.protocol as protocol ,d.type as plugin_id,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts,
		   (select name from device dd where dd.device_type = '2' and dd.parent_id = d.id limit 1) as gateway_name
		   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id  where 1=1 and d.location !=''`
	var values []interface{}
	var where = ""
	if req.BusinessId != "" {
		values = append(values, req.BusinessId)
		where += " and b.id = ?"
	}
	if req.GroupId != "" {
		values = append(values, req.GroupId)
		where += " and a.id = ?"
	}
	if req.DeviceId != "" {
		values = append(values, req.DeviceId)
		where += " and d.id = ?"
	}
	if req.DeviceType != "" {
		values = append(values, req.DeviceType)
		where += " and d.device_type = ?"
	} else { // 直连设备和网关设备
		where += " and d.device_type !='3'"
	}
	if req.Name != "" {
		values = append(values, fmt.Sprintf("%%%s%%", req.Name))
		where += " and d.name like ?"
	}
	if req.DeviceModelId != "" {
		values = append(values, req.DeviceModelId)
		where += " and d.type = ?"
	}
	sqlWhere += where
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		logs.Info(dataResult.Error.Error())
		return deviceList, dataResult.Error
	}
	return deviceList, nil
}

//获取设备列表设备在线离线状态
func (*DeviceService) GetDeviceOnlineStatus(deviceIdList valid.DeviceIdListValidate) (map[string]interface{}, error) {
	var deviceOnlineStatus = make(map[string]interface{})
	for _, deviceId := range deviceIdList.DeviceIdList {
		var tskvLatest models.TSKVLatest
		result := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = 'SYS_ONLINE'", deviceId).First(&tskvLatest)
		logs.Info("------------------------------------------------ceshi")
		if result.Error != nil {
			logs.Error(result.Error)
			deviceOnlineStatus[deviceId] = "0"
		} else {
			deviceOnlineStatus[deviceId] = tskvLatest.StrV
		}
	}
	return deviceOnlineStatus, nil
}

//设备是否在线
func (*DeviceService) IsDeviceOnline(deviceId string) (bool, error) {
	var tskvLatest models.TSKVLatest
	result := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = 'SYS_ONLINE'", deviceId).First(&tskvLatest)
	if result.Error != nil {
		logs.Error(result.Error)
		if result.Error == gorm.ErrRecordNotFound {
			return false, nil
		}
		return false, result.Error
	}
	if tskvLatest.StrV == "1" {
		return true, nil
	}
	return false, nil
}

//根据wvp设备编号获取设备数量
func (*DeviceService) GetWvpDeviceCount(did string) (int64, error) {
	result := psql.Mydb.Where("device_type = '2' and did = ?", did).Find(&models.Device{})
	return result.RowsAffected, result.Error
}
