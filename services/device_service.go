package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	sendmqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"
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
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return &device, result.RowsAffected
}

// 根据租户id和设备id判断设备是否存在
func (*DeviceService) IsDeviceExistByTenantIdAndDeviceId(tenantId string, deviceId string) bool {
	var device models.Device
	result := psql.Mydb.Where("tenant_id = ? and id = ?", tenantId, deviceId).First(&device)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// GetSubDeviceCount 获取子设备数量
func (*DeviceService) GetSubDeviceCount(parentId string) (int64, error) {
	var count int64
	result := psql.Mydb.Model(models.Device{}).Where("parent_id = ?", parentId).Count(&count)
	return count, result.Error
}

// GetDevicesByAssetID 获取设备列表
func (*DeviceService) GetDevicesByAssetID(asset_id string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	db := psql.Mydb.Model(&models.Device{}).Where("asset_id = ?", asset_id)
	result := db.Find(&devices)
	db.Count(&count)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return devices, 0
		}
		logs.Error(result.Error.Error())
		return nil, 0
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
	result := psql.Mydb.Raw(sqlWhereCount, values...).Scan(&count)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
	}
	var offset int = (current - 1) * pageSize
	var limit int = pageSize
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		//errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		logs.Error(dataResult.Error.Error())
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
		as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type as device_type,d.current_version as current_version,d.parent_id as parent_id,d.protocol_config as protocol_config,
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
	result := psql.Mydb.Raw(sqlWhereCount, values...).Scan(&count)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
	}
	var offset int = (req.CurrentPage - 1) * req.PerPage
	var limit int = req.PerPage
	sqlWhere += " order by d.created_at desc offset ? limit ?"
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
					as asset_name,b.id as business_id ,b."name" as business_name,d.d_id,d.location,a.id as asset_id ,d.id as device ,d."name" as device_name,d.device_type  as device_type,d.current_version as current_version,d.parent_id as parent_id,d.protocol_config as protocol_config,d.sub_device_addr as sub_device_addr,
					d.additional_info as additional_info,d."token" as device_token,d."type" as "type",d.protocol as protocol ,dm.model_name as plugin_name,(select ts from ts_kv_latest tkl where tkl.entity_id = d.id order by ts desc limit 1) as latest_ts
					   from device d left join asset a on d.asset_id =  a.id left join business b on b.id = a.business_id LEFT JOIN device_model dm ON d.type = dm.id where 1=1  and d.device_type = '3' and d.parent_id = '` + device["device"].(string) + "' order by d.created_at desc"
				result := psql.Mydb.Raw(sql).Scan(&subDeviceList)
				if result.Error != nil {
					//errors.Is(result.Error, gorm.ErrRecordNotFound)
					logs.Error(result.Error.Error())
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
	result := psql.Mydb.Raw(sqlWhereCount, values...).Scan(&count)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
	}
	var offset int = (req.CurrentPage - 1) * req.PerPage
	var limit int = req.PerPage
	sqlWhere += " offset ? limit ?"
	values = append(values, offset, limit)
	var deviceList []map[string]interface{}
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		//errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		logs.Error(dataResult.Error.Error())
	} else {
		var TSKVService TSKVService
		for _, deviceData := range deviceList {
			fields, _ := TSKVService.GetCurrentData(deviceData["device_id"].(string), nil)
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
				deviceData["values"] = fields
			}
		}
	}
	return deviceList, count
}

// 获取租户下所有设备列表,数据网关相关
func (*DeviceService) AllDeviceListByTenantId(req valid.OpenApiDeviceListValidate, tenantId string) ([]map[string]interface{}, int64, error) {
	var deviceList []map[string]interface{}
	var count int64
	var sqlWhere string
	var sqlHead string = "select d.id as device_id,d.name as device_name,b.name as business_name,a.name as asset_name"
	var sqlHeadCount string = "select count(1)"
	// 相关的表有business,asset,device,tp_r_openapi_auth_device
	// 如果isAdd是1，表示查询已添加的设备，如果是0，表示查询未添加的设备
	if req.IsAdd == 1 {
		sqlWhere = ` from tp_r_openapi_auth_device r left join device d on r.device_id = d.id left join asset a on d.asset_id = a.id left join business b on a.business_id = b.id where d.tenant_id = ? and r.tp_openapi_auth_id = ?`
	} else {
		sqlWhere = ` from device d left join asset a on d.asset_id = a.id left join business b on a.business_id = b.id where d.tenant_id = ? and d.id not in (select device_id from tp_r_openapi_auth_device where tp_openapi_auth_id = ?)`
	}
	var values []interface{}
	values = append(values, tenantId, req.TpOpenapiAuthId)
	if req.BusinessId != "" {
		sqlWhere += " and b.id = ?"
		values = append(values, req.BusinessId)
	}
	if req.AssetId != "" {
		sqlWhere += " and a.id = ?"
		values = append(values, req.AssetId)
	}
	// 获取总数
	countResult := psql.Mydb.Raw(sqlHeadCount+sqlWhere, values...).Count(&count)
	if countResult.Error != nil {
		logs.Error(countResult.Error.Error())
		return deviceList, 0, countResult.Error
	}
	// 设置分页
	var offset int = (req.CurrentPage - 1) * req.PerPage
	var limit int = req.PerPage
	sqlWhere += " order by d.created_at desc offset " + cast.ToString(offset) + " limit " + cast.ToString(limit)
	// 执行sql语句
	result := psql.Mydb.Raw(sqlHead+sqlWhere, values...).Scan(&deviceList)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return deviceList, count, result.Error
	}
	return deviceList, count, nil
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
	SQL := `select device.id,device.token,device.product_id,device.asset_id ,device.current_version ,device.additional_info,device."type" ,device."location",device."d_id",device."name",device."label",device.protocol from device where product_id =?`
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
	result := psql.Mydb.Raw(sqlWhereCount, values...).Scan(&count)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return false, deviceList, 0
	}
	var offset int = (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	var limit int = PaginationValidate.PerPage
	sqlWhere += "order by d.created_at desc offset ? limit ?"
	values = append(values, offset, limit)
	dataResult := psql.Mydb.Raw(sqlWhere, values...).Scan(&deviceList)
	if dataResult.Error != nil {
		//errors.Is(dataResult.Error, gorm.ErrRecordNotFound)
		logs.Error(dataResult.Error.Error())
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
// func (*DeviceService) GetAllDeviceByID(id string) ([]models.Device, int64) {
// 	var devices []models.Device
// 	var count int64
// 	result := psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Find(&devices)
// 	psql.Mydb.Model(&models.Device{}).Where("id = ?", id).Count(&count)
// 	if result.Error != nil {
// 		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
// 			return devices, 0
// 		}
// 		return nil, 0
// 	}
// 	if len(devices) == 0 {
// 		devices = []models.Device{}
// 	}
// 	return devices, count
// }

// GetDevicesByID 获取设备
func (*DeviceService) GetDeviceByID(id string) (*models.Device, int64) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		return nil, 0
	}
	return &device, result.RowsAffected
}

// 根据设备ID获取租户ID
func (*DeviceService) GetTenantIdByDeviceId(id string) (tenantId string, err error) {
	var device models.Device
	result := psql.Mydb.Where("id = ?", id).First(&device)
	if result.Error != nil {
		return "", result.Error
	}
	return device.TenantId, nil
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
		psql.Mydb.Raw("select count(1) from device where parent_id = ?", device.ID).Scan(&count)
		if count > int64(0) {
			return errors.New("请先删除网关设备下的子设备")
		}
	}
	result = psql.Mydb.Where("id = ? and tenant_id = ?", id, tenantId).Delete(&models.Device{})
	if result.Error != nil {
		// 如果错误信息中包含字符tp_automation则提示用户先接触此设备与自动化任务的关联
		if strings.Contains(result.Error.Error(), "tp_automation") {
			return errors.New("请先删除设备与自动化任务的关联")
		}
		return result.Error
	}

	// 删除设备后，清理redis中的数据
	redis.DelKey(device.ID)
	redis.DelKey("status" + device.ID)
	if device.Token != "" {
		redis.DelKey("token" + device.Token)
		redis.DelKey(device.Token)

		// 判断是否是gmqtt
		if viper.GetString("mqtt_server") == "gmqtt" {
			// 删除mqtt的认证信息
			mqttHttpHost := viper.GetString("api.http_host")
			_, err := tphttp.Delete("http://"+mqttHttpHost+"/v1/accounts/"+device.Token, "{}")
			if err != nil {
				logs.Warn(err.Error())
			}
		}
	}
	return nil
}

// 获取全部Device
func (*DeviceService) All(tenantId string) ([]models.Device, int64) {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&devices).Where("tenant_id = ?", tenantId).Count(&count)
	if len(devices) == 0 {
		devices = []models.Device{}
	}
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return devices, 0
		}
		return nil, 0
	}
	return devices, count
}

// 判断token是否存在
func (*DeviceService) IsToken(token string) bool {
	var devices []models.Device
	var count int64
	result := psql.Mydb.Model(&devices).Where("token = ?", token).Count(&count)
	if result.Error != nil {
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
		//判断是否gmqtt
		if viper.GetString("mqtt_server") == "gmqtt" {
			if token != "" {
				MqttHttpHost := os.Getenv("MQTT_HTTP_HOST")
				if MqttHttpHost == "" {
					MqttHttpHost = viper.GetString("api.http_host")
				}
				// mqtt密码制空
				tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+token, "{\"password\":\"\"}")
			}
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
	// 判断是否是device_type变更
	if deviceModel.DeviceType != "" && deviceModel.DeviceType != device.DeviceType {
		// 清理设备id关联的ts_kv_latest数据
		err := psql.Mydb.Delete(&models.TSKVLatest{}, "entity_id = ?", deviceModel.ID).Error
		if err != nil {
			return err
		}
	}
	if deviceModel.Token != "" && deviceModel.Token != device.Token {
		MqttHttpHost := viper.GetString("api.http_host")
		logs.Info("token不为空")
		// 原token不为空的时候，删除原token
		if device.Token != "" {
			redis.DelKey("token" + device.Token)
			// 判断是否是gmqtt
			if viper.GetString("mqtt.type") == "gmqtt" {
				tphttp.Delete("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{}")
			}
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
		if viper.GetString("mqtt_server") == "gmqtt" {
			_, err := tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\""+password+"\"}")
			if err != nil {
				return err
			}
		}

	}
	// 如果修改了密码，需要认证到gmqtt
	logs.Info("判断是否修改了密码")
	if deviceModel.Password != "" {
		if viper.GetString("mqtt_server") == "gmqtt" {
			MqttHttpHost := viper.GetString("api.http_host")
			var token string
			if deviceModel.Token == "" {
				token = device.Token
			} else {
				token = deviceModel.Token
			}
			_, err := tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+token, "{\"password\":\""+deviceModel.Password+"\"}")
			if err != nil {
				return err
			}
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
	return result.Error
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
		//判断是否是gmqtt
		if viper.GetString("mqtt_server") == "gmqtt" {
			tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\"\"}")
		}
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
		//判断是否是gmqtt
		if viper.GetString("mqtt_server") == "gmqtt" {
			tphttp.Post("http://"+MqttHttpHost+"/v1/accounts/"+device.Token, "{\"password\":\"\"}")
		}
	}
	return device, nil
}

// 向mqtt发送控制指令
func (*DeviceService) OperatingDevice(deviceId string, field string, value interface{}) error {
	//reqMap := make(map[string]interface{})
	valueMap := make(map[string]interface{})
	logs.Info("通过设备id获取设备token")
	var DeviceService DeviceService
	device, i := DeviceService.Token(deviceId)
	// 此处如果用device == nil 判断，会产生错误，因为device是一个空结构体，即使没有查到数据，也不会返回nil
	//if device == nil
	if i == 0 {
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

// 发送消息 device为子设备
func (*DeviceService) SendMessage(msg []byte, device *models.Device) error {
	var err error
	// 转发消息 - 属性下发 - MessageType 2
	CheckAndTranspondData(device.ID, msg, DeviceMessageTypeAttributeSend, device.Token)
	if device.DeviceType == "1" { // 直连设备
		// 通过脚本
		msg, err = scriptDealB(device.ScriptId, msg, viper.GetString("mqtt.topicToPublish")+"/"+device.Token)
		if err != nil {
			return err
		}
		if device.Protocol == "mqtt" {
			logs.Info("直连设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))

			err = sendmqtt.Send(msg, device.Token)
		} else { // 协议设备
			logs.Info("直连协议设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
			//获取协议插件订阅topic
			var TpProtocolPluginService TpProtocolPluginService
			pp := TpProtocolPluginService.GetByProtocolType(device.Protocol, "1")
			var topic = pp.SubTopicPrefix + device.Token
			err = sendmqtt.SendPlugin(msg, topic)
		}

	} else if device.DeviceType == "3" && device.Protocol != "MQTT" { // 协议插件子设备
		//其他协议插件
		var gatewayDevice *models.Device
		result := psql.Mydb.Where("id = ?", device.ParentId).First(&gatewayDevice) // 检测网关token是否存在
		if result.Error == nil {
			//获取协议插件订阅topic
			var TpProtocolPluginService TpProtocolPluginService
			pp := TpProtocolPluginService.GetByProtocolType(device.Protocol, "2")
			// 前缀+网关设备token
			var topic1 = pp.SubTopicPrefix + gatewayDevice.Token
			var topic2 = pp.SubTopicPrefix + gatewayDevice.ID
			//包装子设备地址
			var msgMapValues = make(map[string]interface{})
			json.Unmarshal(msg, &msgMapValues)
			var subMap = make(map[string]interface{})
			subMap[device.SubDeviceAddr] = msgMapValues
			msgBytes, _ := json.Marshal(subMap)
			// 通过脚本
			msg, err = scriptDealB(device.ScriptId, msgBytes, topic1)
			if err != nil {
				return err
			}
			logs.Info("网关设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
			err = sendmqtt.SendPlugin(msgBytes, topic1)
			if err != nil {
				logs.Error(err.Error())
			}
			err = sendmqtt.SendPlugin(msgBytes, topic2)
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
				msg, err = scriptDealB(gatewayDevice.ScriptId, msgBytes, viper.GetString("mqtt.gateway_topic")+"/"+device.Token)
				if err != nil {
					return err
				}
				logs.Info("网关设备下行脚本处理后：", utils.ReplaceUserInput(string(msg)))
				err = sendmqtt.SendGateWay(msg, gatewayDevice.Token, gatewayDevice.Protocol)
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
				//根据设备id获取租户id
				var deviceService DeviceService
				tenantId, _ := deviceService.GetTenantIdByDeviceId(applyDeviceId)
				ConditionsLog := models.ConditionsLog{
					DeviceId:      applyDeviceId,
					OperationType: "3",
					Instruct:      applyField + ":" + s,
					ProtocolType:  "mqtt",
					CteateTime:    time.Now().Format("2006-01-02 15:04:05"),
					Remark:        rule_id,
					TenantId:      tenantId,
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

// GetConfigByToken 根据提供的 token 或 deviceID 获取设备配置。如果是网关设备，则返回其所有子设备的列表。
func (*DeviceService) GetConfigByToken(token string, deviceId string) (map[string]interface{}, error) {
	type SubDeviceConfig struct {
		AccessToken   string                 `json:"AccessToken"`
		DeviceID      string                 `json:"DeviceId"`
		SubDeviceAddr string                 `json:"SubDeviceAddr"`
		Config        map[string]interface{} `json:"Config"` // 表单配置
	}
	type DeviceConfig struct {
		ProtocolType string                 `json:"ProtocolType"`
		AccessToken  string                 `json:"AccessToken"`
		DeviceType   string                 `json:"DeviceType"`
		ID           string                 `json:"Id"`
		DeviceConfig map[string]interface{} `json:"DeviceConfig,omitempty"` // 表单配置
		SubDevices   []SubDeviceConfig      `json:"SubDevices,omitempty"`
	}

	var device models.Device
	var result *gorm.DB
	// 根据deviceId或token查询设备
	if deviceId != "" {
		result = psql.Mydb.First(&device, "id = ?", deviceId)
	} else {
		result = psql.Mydb.First(&device, "token = ?", token)
	}

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("device not found")
		}
		return nil, result.Error
	}

	var protocolConfig map[string]interface{}
	if err := json.Unmarshal([]byte(device.ProtocolConfig), &protocolConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal protocol config: %w", err)
	}

	config := DeviceConfig{
		ProtocolType: device.Protocol,
		AccessToken:  device.Token,
		DeviceType:   device.DeviceType,
		ID:           device.ID,
		DeviceConfig: protocolConfig,
	}

	if device.DeviceType == "2" {
		var subDevices []models.Device
		subResult := psql.Mydb.Find(&subDevices, "parent_id = ?", device.ID)

		if subResult.Error != nil {
			if errors.Is(subResult.Error, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, subResult.Error
		}

		config.SubDevices = make([]SubDeviceConfig, len(subDevices))

		for i, subDevice := range subDevices {
			var subDeviceConfig map[string]interface{}
			if err := json.Unmarshal([]byte(subDevice.ProtocolConfig), &subDeviceConfig); err != nil {
				return nil, fmt.Errorf("failed to unmarshal subdevice protocol config: %w", err)
			}

			config.SubDevices[i] = SubDeviceConfig{
				AccessToken:   subDevice.Token,
				DeviceID:      subDevice.ID,
				SubDeviceAddr: subDevice.SubDeviceAddr,
				Config:        subDeviceConfig,
			}
		}
	}
	// struct转map
	jsonData, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	var mapResult map[string]interface{}
	if err := json.Unmarshal(jsonData, &mapResult); err != nil {
		return nil, err
	}
	return mapResult, nil
}

func (*DeviceService) GetConfigByProtocolAndDeviceType(protocol string, deviceType string) ([]map[string]interface{}, error) {
	type SubDevice struct {
		AccessToken   string                 `json:"AccessToken"`
		DeviceId      string                 `json:"DeviceId"`
		SubDeviceAddr string                 `json:"SubDeviceAddr"`
		DeviceConfig  map[string]interface{} `json:"DeviceConfig"`
	}
	type DeviceConfig struct {
		ProtocolType string                 `json:"ProtocolType"`
		AccessToken  string                 `json:"AccessToken"`
		DeviceType   string                 `json:"DeviceType"`
		ID           string                 `json:"Id"`
		DeviceConfig map[string]interface{} `json:"DeviceConfig,omitempty"`
		SubDevice    []SubDevice            `json:"SubDevice,omitempty"`
	}

	var allConfig []DeviceConfig
	var deviceList []models.Device

	result := psql.Mydb.Find(&deviceList, "protocol = ? and device_type = ?", protocol, deviceType)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		logs.Error(result.Error)
		return nil, result.Error
	}

	allConfig = make([]DeviceConfig, 0, len(deviceList))

	for _, device := range deviceList {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(device.ProtocolConfig), &m); err != nil {
			logs.Error("Unmarshal failed:", err)
			continue
		}
		var config DeviceConfig
		config.ProtocolType = device.Protocol
		config.AccessToken = device.Token
		config.DeviceType = device.DeviceType
		config.ID = device.ID
		// 网关设备
		if device.DeviceType == "2" {
			var subDevices []models.Device
			subResult := psql.Mydb.Find(&subDevices, "parent_id = ?", device.ID)
			if subResult.Error != nil && !errors.Is(subResult.Error, gorm.ErrRecordNotFound) {
				logs.Error(subResult.Error)
				continue
			}
			for _, subDevice := range subDevices {
				sub := SubDevice{
					AccessToken:   subDevice.Token,
					DeviceId:      subDevice.ID,
					SubDeviceAddr: subDevice.SubDeviceAddr,
				}

				if err := json.Unmarshal([]byte(subDevice.ProtocolConfig), &m); err != nil {
					logs.Error("Unmarshal failed:", err)
					continue
				}
				sub.DeviceConfig = m
				config.SubDevice = append(config.SubDevice, sub)
			}
		}

		allConfig = append(allConfig, config)
	}

	// Convert the struct slice to a slice of map[string]interface{}
	var allConfigMap []map[string]interface{}
	for _, config := range allConfig {
		var configMap map[string]interface{}
		bytes, err := json.Marshal(config)
		if err != nil {
			logs.Error("Marshal failed:", err)
			continue
		}
		if err := json.Unmarshal(bytes, &configMap); err != nil {
			logs.Error("Unmarshal failed:", err)
			continue
		}
		allConfigMap = append(allConfigMap, configMap)
	}

	return allConfigMap, nil
}

// 修改所有子设备分组
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
func (*DeviceService) DeviceMapList(req valid.DeviceMapValidate, tenantId string) ([]map[string]interface{}, error) {
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
	var where = "and d.tenant_id = ?"
	values = append(values, tenantId)
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

// 获取设备列表设备在线离线状态
func (*DeviceService) GetDeviceOnlineStatus(deviceIdList valid.DeviceIdListValidate) (map[string]interface{}, error) {
	var deviceOnlineStatus = make(map[string]interface{})
	for _, deviceId := range deviceIdList.DeviceIdList {
		var device models.Device
		//根据阈值判断设备是否在线
		result := psql.Mydb.Where("id = ?", deviceId).First(&device)
		if result.Error != nil {
			logs.Error(result.Error)
			if result.Error == gorm.ErrRecordNotFound {
				deviceOnlineStatus[deviceId] = "0"
				continue
			}
		}

		// 检查是否设置了在线离线阈值
		if device.AdditionalInfo != "" {
			aJson, err := simplejson.NewJson([]byte(device.AdditionalInfo))
			if err == nil {
				thresholdTime, err := aJson.Get("runningInfo").Get("thresholdTime").Int64()
				if err == nil && thresholdTime != 0 {
					//获取最新的数据时间
					var latest_ts int64
					result = psql.Mydb.Model(&models.TSKVLatest{}).Select("max(ts) as ts").Where("entity_id = ? ", deviceId).Group("entity_type").First(&latest_ts)
					if result.Error != nil {
						logs.Error(result.Error)
					}
					if latest_ts != 0 {
						if time.Now().UnixMicro()-latest_ts >= int64(thresholdTime*1e6) {
							deviceOnlineStatus[deviceId] = "0"
						} else {
							deviceOnlineStatus[deviceId] = "1"
						}
						continue
					}
				}
			}
		}

		//原流程
		var tskvLatest models.TSKVLatest
		result = psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = 'SYS_ONLINE'", deviceId).First(&tskvLatest)
		if result.Error != nil {
			logs.Warn(result.Error)
			deviceOnlineStatus[deviceId] = "0"
		} else {
			deviceOnlineStatus[deviceId] = tskvLatest.StrV
		}
	}
	return deviceOnlineStatus, nil
}

// 设备是否在线
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

// 根据wvp设备编号获取设备数量
func (*DeviceService) GetWvpDeviceCount(did string) (int64, error) {
	result := psql.Mydb.Where("device_type = '2' and did = ?", did).Find(&models.Device{})
	return result.RowsAffected, result.Error
}

func (*DeviceService) SendCommandToDevice(
	targetDevice *models.Device,
	originalDeviceId string,
	commandIdentifier string,
	commandData []byte,
	commandName string,
	commandDesc string,
	userID string,
) error {

	// 格式化内容：
	var sendStruct struct {
		Method string      `json:"method"`
		Params interface{} `json:"params"`
	}

	commandDataMap := make(map[string]interface{})
	err := json.Unmarshal(commandData, &commandDataMap)
	if err != nil {
		return err
	}
	sendStruct.Method = commandIdentifier
	sendStruct.Params = commandDataMap
	msg, err := json.Marshal(sendStruct)
	if err != nil {
		return err
	}

	sendRes := 2
	switch targetDevice.DeviceType {

	case models.DeviceTypeDirect:
		// 直连设备
		topic := sendmqtt.Topic_DeviceCommand + "/"
		topic += targetDevice.Token

		// 协议设备topic
		if targetDevice.Protocol != "mqtt" && targetDevice.Protocol != "MQTT" {
			var tpProtocolPluginService TpProtocolPluginService
			pp := tpProtocolPluginService.GetByProtocolType(targetDevice.Protocol, targetDevice.DeviceType)
			topic = pp.SubTopicPrefix + "command/" + targetDevice.Token
		}
		// 通过脚本
		msg, err := scriptDealB(targetDevice.ScriptId, msg, topic)
		if err != nil {
			return err
		}

		if sendmqtt.SendMQTT(msg, topic, 1) == nil {
			sendRes = 1
		}

		saveCommandSendHistory(
			userID,
			targetDevice.ID,
			commandIdentifier,
			commandName,
			commandDesc,
			string(msg),
			sendRes,
		)
	case models.DeviceTypeGatway:
		// 网关
		topic := sendmqtt.Topic_GatewayCommand + "/"
		topic += targetDevice.Token

		if targetDevice.Protocol != "mqtt" && targetDevice.Protocol != "MQTT" {
			var tpProtocolPluginService TpProtocolPluginService
			pp := tpProtocolPluginService.GetByProtocolType(targetDevice.Protocol, targetDevice.DeviceType)
			topic = pp.SubTopicPrefix + "command/" + targetDevice.Token
		}
		// 通过脚本
		msg, err := scriptDealB(targetDevice.ScriptId, msg, topic)
		if err != nil {
			return err
		}

		if sendmqtt.SendMQTT(msg, topic, 1) == nil {
			sendRes = 1
		}

		saveCommandSendHistory(
			userID,
			targetDevice.ID,
			commandIdentifier,
			commandName,
			commandDesc,
			string(msg),
			sendRes)

	case models.DeviceTypeSubGatway:
		// 子网关，给网关发
		topic := sendmqtt.Topic_GatewayCommand + "/"
		if len(targetDevice.ParentId) != 0 {
			var gatewayDevice *models.Device
			result := psql.Mydb.Where("id = ?", targetDevice.ParentId).First(&gatewayDevice) // 检测网关token是否存在
			if result.Error != nil {
				return result.Error
			}
			topic += gatewayDevice.Token
			// 协议设备topic
			if gatewayDevice.Protocol != "mqtt" && gatewayDevice.Protocol != "MQTT" {
				var tpProtocolPluginService TpProtocolPluginService
				pp := tpProtocolPluginService.GetByProtocolType(gatewayDevice.Protocol, gatewayDevice.DeviceType)
				topic = pp.SubTopicPrefix + "command/" + gatewayDevice.Token
			}

			if gatewayDevice.Protocol == "MQTT" {
				// 查找子设备的SubDeviceAddr
				var subDevice *models.Device
				result2 := psql.Mydb.Where("id = ?", originalDeviceId).First(&subDevice)
				if result2.Error != nil {
					return result2.Error
				}
				// 格式组装：{"A0001":{"method":"reset","params":{"rs":1}}}
				data := make(map[string]interface{})
				data[subDevice.SubDeviceAddr] = sendStruct
				msg, err = json.Marshal(data)
				if err != nil {
					return err
				}
			}

			msg, err := scriptDealB(gatewayDevice.ScriptId, msg, topic)
			if err != nil {
				return err
			}
			// 通过脚本
			if sendmqtt.SendMQTT(msg, topic, 1) == nil {
				sendRes = 1
			}

			saveCommandSendHistory(
				userID,
				originalDeviceId,
				commandIdentifier,
				commandName,
				commandDesc,
				string(msg),
				sendRes)
		}

	default:
		break
	}
	return nil
}

// 存储来自设备上报的事件
func (*DeviceService) SubscribeDeviceEvent(body []byte, topic string) bool {
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	// 根据token查找设备ID
	var deviceid string
	result := psql.Mydb.Model(models.Device{}).Select("id").Where("token = ?", payload.Token).First(&deviceid)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false
	} else if result.RowsAffected <= int64(0) {
		logs.Error("no device")
		return false
	}
	// 判断mqtt服务是否为vernemq，如果是不需要转发,主要服务ws接口
	if viper.GetString("mqtt_server") == "gmqtt" {
		// 发送数据到mqtt服务
		topic := viper.GetString("mqtt.topicToEvent") + "/" + deviceid
		sendmqtt.SendMQTT(body, topic, 0)
	}
	var payLoadData struct {
		Method string                 `json:"method"`
		Params map[string]interface{} `json:"params"`
	}

	e := json.Unmarshal(payload.Values, &payLoadData)
	if e != nil {
		return false
	}

	datastr, _ := json.Marshal(payLoadData.Params)

	// 存储//
	m := models.DeviceEvnetHistory{
		ID:            utils.GetUuid(),
		DeviceId:      deviceid,
		EventIdentify: payLoadData.Method,
		Data:          string(datastr),
		EventName:     "",
		Desc:          "",
		ReportTime:    time.Now().Unix(),
	}

	_ = psql.Mydb.Create(&m)

	payloadMap := make(map[string]interface{})

	json.Unmarshal(payload.Values, &payloadMap)

	var ConditionsService ConditionsService
	go ConditionsService.AutomationConditionCheck(deviceid, payloadMap)

	return true
}

type SubDevice struct {
	Method string                 `json:"method"`
	Params map[string]interface{} `json:"params"`
}

// 订阅来自网关的事件上报
func (*DeviceService) SubscribeGatwayEvent(body []byte, topic string) bool {
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 根据token查找设备ID
	// var deviceid string
	// result := psql.Mydb.Model(models.Device{}).Select("id").Where("token = ?", payload.Token).First(&deviceid)
	// if result.Error != nil {
	// 	logs.Error(result.Error, gorm.ErrRecordNotFound)
	// 	return false
	// } else if result.RowsAffected <= int64(0) {
	// 	logs.Error("no device")
	// 	return false
	// }

	data := map[string]SubDevice{}
	e := json.Unmarshal(payload.Values, &data)
	if e != nil {
		return false
	}

	for subDeviceAddr, subData := range data {
		var subDeviceInfo models.Device
		result := psql.Mydb.Where("sub_device_addr = ?", subDeviceAddr).Find(&subDeviceInfo)
		if result.Error != nil {
			logs.Error(result.Error)
			if result.Error == gorm.ErrRecordNotFound {
				return false
			}
			return false
		}

		// 通过subaddr 查询设备的id
		datastr, _ := json.Marshal(subData.Params)
		m := models.DeviceEvnetHistory{
			ID:            utils.GetUuid(),
			DeviceId:      subDeviceInfo.ID,
			EventIdentify: subData.Method,
			Data:          string(datastr),
			EventName:     "",
			Desc:          "",
			ReportTime:    time.Now().Unix(),
		}

		_ = psql.Mydb.Create(&m)
	}

	return true
}

// 记录发送日志
func saveCommandSendHistory(
	userId, deviceId, identify, name, desc, data string,
	sendStatus int,
) {
	m := models.DeviceCommandHistory{
		ID:              utils.GetUuid(),
		DeviceId:        deviceId,
		CommandIdentify: identify,
		Data:            data,
		Desc:            desc,
		CommandName:     name,
		SendTime:        time.Now().Unix(),
		SendStatus:      int64(sendStatus),
		UserId:          userId,
	}
	err := psql.Mydb.Create(&m)
	if err.Error != nil {
		errors.Is(err.Error, gorm.ErrRecordNotFound)
	}
}

// 获取租户设备数
// 获取全部Device
func (*DeviceService) GetTenantDeviceCount(tenantId string, deviceType string) map[string]int64 {
	var count int64
	switch {
	case deviceType == "0": //全部设备不包含子设备
		result := psql.Mydb.Model(&models.Device{}).Where("tenant_id = ? and parent_id=''", tenantId).Count(&count)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return map[string]int64{"all": 0}
			}
		}
		return map[string]int64{"all": count}
	case deviceType == "1": //在线设备总数
		sql := `select count(distinct entity_id) from ts_kv_latest where tenant_id = ? and key='SYS_ONLINE' and str_v='1' `
		result := psql.Mydb.Raw(sql, tenantId).Count(&count)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				return map[string]int64{"online": 0}
			}
		}
		return map[string]int64{"online": count}
	default: //默认全部
		result := psql.Mydb.Model(&models.Device{}).Where("tenant_id = ? and parent_id=''", tenantId).Count(&count)
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				count = 0
			}
		}
		var oncount int64
		sql := `select count(distinct entity_id) from ts_kv_latest where tenant_id = ? and key='SYS_ONLINE' and str_v='1' `
		result2 := psql.Mydb.Raw(sql, tenantId).Count(&oncount)
		if result2.Error != nil {
			if errors.Is(result2.Error, gorm.ErrRecordNotFound) {
				oncount = 0
			}
		}
		return map[string]int64{"all": count, "online": oncount}

	}
}

func (*DeviceService) OperateDeviceStatus(deviceId, deviceStatus string) error {
	if deviceStatus != "0" && deviceStatus != "1" {
		return fmt.Errorf("设备状态必须为0或1,当前设置为:%s", deviceStatus)
	}
	// 修改数据库中的设备状态
	result := psql.Mydb.Model(&models.TSKVLatest{}).
		Where("entity_id = ? and key = 'SYS_ONLINE'", deviceId).
		Updates(models.TSKVLatest{StrV: deviceStatus, TS: utils.GetMicrosecondTimestamp()})
	if result.Error != nil {
		logs.Error(result.Error)
		return result.Error
	}

	err := redis.SetStr("status"+deviceId, deviceStatus, 0)
	if err != nil {
		return err
	}
	return nil
}
