package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/server/web"
	"github.com/bitly/go-simplejson"
	"github.com/spf13/viper"

	//"github.com/zenghouchao/timeHelper"
	tptodb "ThingsPanel-Go/grpc/tptodb_client"
	pb "ThingsPanel-Go/grpc/tptodb_client/grpc_tptodb"

	sendMqtt "ThingsPanel-Go/modules/dataService/mqtt/sendMqtt"

	"gorm.io/gorm"
)

//var DeviceOnlineState = make(map[string]interface{})

type TSKVService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

type mqttPayload struct {
	Token  string `json:"token"`
	Values []byte `json:"values"`
}

// []byte转mqttPayload结构体，并做token和values验证
func verifyPayload(body []byte) (*mqttPayload, error) {
	payload := &mqttPayload{}
	if err := json.Unmarshal(body, payload); err != nil {
		logs.Error("解析消息失败:", err)
		return payload, err
	}
	if len(payload.Token) == 0 {
		return payload, errors.New("token不能为空:" + payload.Token)
	}
	if len(payload.Values) == 0 {
		return payload, errors.New("values消息内容不能为空")
	}
	return payload, nil
}

type mqttPayloadOther struct {
	AccessToken string      `json:"accessToken"`
	Values      interface{} `json:"values"`
}

// []byte转mqttPayload结构体，并做token和values验证
func verifyPayloadOther(body []byte) (*mqttPayloadOther, error) {
	payload := &mqttPayloadOther{}
	if err := json.Unmarshal(body, payload); err != nil {
		logs.Error("解析消息失败:", err)
		return payload, err
	}
	if len(payload.AccessToken) == 0 {
		return payload, errors.New("token不能为空:" + payload.AccessToken)
	}
	return payload, nil
}

// 脚本处理
func scriptDeal(script_id string, device_data []byte, topic string) ([]byte, error) {
	if script_id == "" {
		logs.Info("脚本id不存在:", script_id)
		return device_data, nil
	}
	var tp_script models.TpScript
	result_b := psql.Mydb.Where("id = ?", script_id).First(&tp_script)
	if result_b.Error == nil {
		logs.Info("脚本信息存在")
		req_str, err_a := utils.ScriptDeal(tp_script.ScriptContentA, device_data, topic)
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

// 获取TSKV总数，这里因为性能的问题做了缓存，限制10W以上数据10秒刷新一次
func (*TSKVService) All(tenantId string) (int64, error) {
	var count int64
	msgCount := redis.GetStr("MsgCount")
	if msgCount != "" {
		count, _ = strconv.ParseInt(msgCount, 10, 64)
		return count, nil
	}
	result := psql.Mydb.Model(&models.TSKV{}).Where("tenant_id = ?", tenantId).Count(&count)
	if result.Error != nil {
		return 0, result.Error
	}
	if count > int64(100000) {
		redis.SetStr("MsgCount", strconv.FormatInt(count, 10), 10*time.Second)
	}
	return count, nil
}

// 接收硬件其他消息（在线离线）
func (*TSKVService) MsgProcOther(body []byte, topic string) {
	logs.Info("-------------------------------")
	logs.Info(string(body))
	logs.Info("-------------------------------")
	payload, err := verifyPayloadOther(body)
	if err != nil {
		logs.Error(err.Error())
		return
	}
	if values, ok := payload.Values.(map[string]interface{}); ok {
		var device models.Device
		// 首先从redis中获设备id
		device.ID = redis.GetStr(payload.AccessToken)
		if device.ID == "" {
			// 从数据库中获取设备id
			result := psql.Mydb.Where("token = ?", payload.AccessToken).First(&device)
			if result.Error != nil {
				logs.Error(result.Error.Error())
				return
			}
			if device.ID == "" {
				return
			} else {
				// 存储24小时
				redis.SetStr(payload.AccessToken, device.ID, 24*time.Hour)
			}
		}

		//DeviceOnlineState[device.ID] = values["status"]
		// 如果mqtt_server为vernemq,则不需要更新ts_kv_latest表
		if viper.GetString("mqtt_server") != "vernemq" {
			d := models.TSKVLatest{
				EntityType: "DEVICE",
				EntityID:   device.ID,
				Key:        "SYS_ONLINE",
				TS:         time.Now().UnixMicro(),
				StrV:       fmt.Sprint(values["status"]),
			}
			result := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = 'SYS_ONLINE'", device.ID).Update("str_v", d.StrV)
			if result.Error != nil {
				logs.Error(result.Error.Error())
			} else {
				if result.RowsAffected == int64(0) {
					rtsl := psql.Mydb.Create(&d)
					if rtsl.Error != nil {
						logs.Error(rtsl.Error)
					}
				}
			}
		}
		// 设备上下线自动化检查
		flag := fmt.Sprint(values["status"])
		if flag == "0" {
			flag = "2"
		}
		var ConditionsService ConditionsService
		go ConditionsService.OnlineAndOfflineCheck(device.ID, flag)
	}
}

// 接收网关消息
func (*TSKVService) GatewayMsgProc(body []byte, topic string, messages chan map[string]interface{}) bool {
	logs.Info("------------------------------")
	logs.Info("来自网关设备的消息：")
	logs.Info(string(body))
	logs.Info("------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	// 通过token获取网关设备信息
	var device models.Device
	result_a := psql.Mydb.Where("token = ? and device_type = '2'", payload.Token).First(&device)
	if result_a.Error != nil {
		logs.Error(result_a.Error, gorm.ErrRecordNotFound)
		return false
	} else if result_a.RowsAffected <= int64(0) {
		logs.Error("根据token没查找到设备")
		return false
	}
	logs.Info("设备信息：", device)
	// 通过脚本执行器
	req, err := scriptDeal(device.ScriptId, payload.Values, topic)

	if err != nil {
		logs.Error(err.Error())
		return false
	}
	logs.Info("转码后:", string(req))
	//byte转map
	var payload_map = make(map[string]interface{})
	err = json.Unmarshal(req, &payload_map)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	// 子设备数组
	var sub_device_list []models.Device
	result := psql.Mydb.Where("parent_id = ? and device_type = '3'", device.ID).Find(&sub_device_list) // 查询网关下子设备
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return false
	}
	// 组合单设备消息
	for _, sub_device := range sub_device_list {
		if values, ok := payload_map[sub_device.SubDeviceAddr]; ok {
			var sub_device_map = make(map[string]interface{})
			sub_device_map["token"] = sub_device.Token
			values_bytes, err := json.Marshal(values)
			if err != nil {
				logs.Error(err.Error())
			}
			sub_device_map["values"] = values_bytes
			// 子设备payload转字节数组
			sub_payload_bytes, err := json.Marshal(sub_device_map)
			if err != nil {
				logs.Error(err.Error())
				return false
			} else {

				// 写入协程数
				// writeWorkers, _ := web.AppConfig.Int("write_workers")
				// for i := 0; i < writeWorkers; i++ {
				// 	var TSKVService TSKVService
				// 	go TSKVService.BatchWrite(messages)
				// }
				var TSKVService TSKVService
				TSKVService.MsgProc(messages, sub_payload_bytes, topic)
			}
		}
	}
	return true
}

// 接收硬件消息 ，数据转发切入点，（设备上报）
func (*TSKVService) MsgProc(messages chan<- map[string]interface{}, body []byte, topic string) bool {
	logs.Info("-------------------------------")
	logs.Info("来自直连设备/网关解析后的子设备的消息：")
	logs.Info(utils.ReplaceUserInput(string(body)))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}

	var d models.TSKV
	// 通过token获取设备信息
	var device models.Device
	result_a := psql.Mydb.Where("token = ? and device_type != '2'", payload.Token).First(&device)
	if result_a.Error != nil {
		logs.Error(result_a.Error, gorm.ErrRecordNotFound)
		return false
	} else if result_a.RowsAffected <= int64(0) {
		logs.Error("根据token没查找到设备")
		return false
	}
	// 通过脚本执行器
	req, err_a := scriptDeal(device.ScriptId, payload.Values, topic)
	if err_a != nil {
		logs.Error(err_a.Error())
		return false
	}

	// 上面脚本处理后转发
	go CheckAndTranspondData(device.ID, req, DeviceMessageTypeAttributeReport)

	logs.Info("转码后:", utils.ReplaceUserInput(string(req)))
	//byte转map
	var payload_map = make(map[string]interface{})
	err_b := json.Unmarshal(req, &payload_map)
	if err_b != nil {
		logs.Error(err_b.Error())
		return false
	}
	// 告警缓存，先查缓存，如果=1就跳过，没有就进入WarningConfigCheck
	// 进入没有就设置为1
	// 新增的时候删除
	// 修改的时候删除
	// 有效时间一小时
	// if redis.GetStr("warning"+device.ID) != "1" {
	// 	var WarningConfigService WarningConfigService
	// 	WarningConfigService.WarningConfigCheck(device.ID, payload_map)
	// }
	// 判断mqtt服务是否为vernemq，如果是不需要转发,主要服务ws接口
	if viper.GetString("mqtt_server") == "gmqtt" {

		// 发送数据到mqtt服务
		topic := viper.GetString("mqtt.topicToSubscribe") + "/" + device.ID
		sendMqtt.SendMQTT(body, topic, 0)
	}
	// 非系统数据库不需要入库
	dbType, _ := web.AppConfig.String("dbType")
	if dbType != "cassandra" {
		// 入库
		//存入系统时间
		ts := time.Now().UnixMicro()
		payload_map["systime"] = fmt.Sprint(time.Now().Format("2006-01-02 15:04:05"))
		for k, v := range payload_map {
			switch value := v.(type) {
			case int64:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        k,
					TS:         ts,
					LongV:      value,
					TenantID:   device.TenantId,
				}
			case string:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        k,
					TS:         ts,
					StrV:       value,
					TenantID:   device.TenantId,
				}
			case bool:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        k,
					TS:         ts,
					BoolV:      strconv.FormatBool(value),
					TenantID:   device.TenantId,
				}
			case float64:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        k,
					TS:         ts,
					DblV:       value,
					TenantID:   device.TenantId,
				}
			default:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        k,
					TS:         ts,
					StrV:       fmt.Sprint(value),
					TenantID:   device.TenantId,
				}
			}
			// ts_kv批量入库
			logs.Info("tskv入库数据：", d)
			messages <- map[string]interface{}{
				"tskv": d,
			}
			// 更新当前值表
			// l := models.TSKVLatest{}
			// utils.StructAssign(&l, &d)
			// var latestCount int64
			// psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ? and tenant_id = ?", l.EntityType, l.EntityID, l.Key, l.TenantID).Count(&latestCount)
			// if latestCount <= 0 {
			// 	rtsl := psql.Mydb.Create(&l)
			// 	if rtsl.Error != nil {
			// 		log.Println(rtsl.Error)
			// 	}
			// } else {
			// 	rtsl := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ? and tenant_id = ?", l.EntityType, l.EntityID,
			// 		l.Key, l.TenantID).Updates(map[string]interface{}{"entity_type": l.EntityType, "entity_id": l.EntityID, "key": l.Key, "ts": l.TS, "bool_v": l.BoolV, "long_v": l.LongV, "str_v": l.StrV, "dbl_v": l.DblV})
			// 	if rtsl.Error != nil {
			// 		log.Println(rtsl.Error)
			// 	}
			// }

			// rts := psql.Mydb.Create(&d)
			// if rts.Error != nil {
			// 	log.Println(rts.Error)
			// 	return false
			// }
		}
	}

	var ConditionsService ConditionsService
	go ConditionsService.AutomationConditionCheck(device.ID, payload_map)
	return true
}

// 批量写入
func (*TSKVService) BatchWrite(messages <-chan map[string]interface{}) error {
	logs.Info("批量写入协程启动")
	var tskvList []models.TSKV
	var tskvLatestList []models.TSKVLatest
	batchWaitTime, _ := web.AppConfig.Int("batch_wait_time")
	logs.Info("批量写入等待时间：", batchWaitTime)
	// 转time.Duration
	batchWaitTimeDuration := time.Duration(batchWaitTime) * time.Second
	batchSize, _ := web.AppConfig.Int("batch_size")
	logs.Warn("批量写入大小：", batchSize)
	for {

		// 如果超过1秒钟messages没有收到任何消息，则将批处理写入
		startTime := time.Now()
		for i := 0; i < batchSize; i++ {
			// 判断是否超时
			// 超时时间
			if time.Since(startTime) > batchWaitTimeDuration {
				break
			}
			// 判断管道是否有消息

			message, ok := <-messages
			if !ok {
				break
			}
			if tskv, ok := message["tskv"].(models.TSKV); ok {
				tskvList = append(tskvList, tskv)
				var tskvLatest models.TSKVLatest
				utils.StructAssign(&tskvLatest, &tskv)
				tskvLatestList = append(tskvLatestList, tskvLatest)
			}
		}
		if len(tskvList) > 0 {
			if err := psql.Mydb.Create(&tskvList).Error; err != nil {
				logs.Error(err.Error())
			}
			logs.Info("批量写入ts_kv：", len(tskvList))
			tskvList = []models.TSKV{}
		}
		// 更新ts_kv_latest
		if len(tskvLatestList) > 0 {
			// 创建事务
			for _, tskvLatest := range tskvLatestList {
				// 尝试更新记录
				result := psql.Mydb.Model(&models.TSKVLatest{}).Where(models.TSKVLatest{
					EntityType: tskvLatest.EntityType,
					EntityID:   tskvLatest.EntityID,
					Key:        tskvLatest.Key,
					TenantID:   tskvLatest.TenantID,
				}).Select("TS", "StrV", "DblV").Updates(models.TSKVLatest{
					TS:   tskvLatest.TS,
					StrV: tskvLatest.StrV,
					DblV: tskvLatest.DblV,
				})

				// 检查是否有记录被更新
				if result.RowsAffected == 0 {
					// 没有记录被更新，执行插入操作
					result = psql.Mydb.Debug().Create(&tskvLatest)
				}

				if result.Error != nil {
					logs.Error(result.Error)
				}
			}
			// 清空tskvLatestList
			tskvLatestList = []models.TSKVLatest{}
		}

	}
}

// 分页查询数据
func (*TSKVService) Paginate(business_id, asset_id, token string, t_type int64, start_time string, end_time string, limit int, offset int, key string, device_name string, tenant_id string) ([]models.TSKVDblV, int64) {
	tSKVs := []models.TSKVResult{}
	tsk := []models.TSKVDblV{}
	var count int64
	result := psql.Mydb
	result2 := psql.Mydb
	if limit <= 0 {
		limit = 1000000
	}
	if offset <= 0 {
		offset = 0
	}
	filters := map[string]interface{}{}
	if business_id != "" { //设备id
		filters["business_id"] = business_id
	}
	if asset_id != "" { //资产id
		filters["asset_id"] = asset_id
	}
	if token != "" { //资产id
		filters["token"] = token
	}
	if start_time != "" && end_time != "" {
		timeTemplate := "2006-01-02 15:04:05"
		start_date, _ := time.ParseInLocation(timeTemplate, start_time, time.Local)
		end_date, _ := time.ParseInLocation(timeTemplate, end_time, time.Local)
		start := start_date.UnixMicro()
		end := end_date.UnixMicro()
		filters["start_date"] = start
		filters["end_date"] = end
	}

	SQLWhere, params := utils.TsKvFilterToSql(filters)
	if key != "" { //key
		params = append(params, key)
		SQLWhere += " and key = ?"
	}
	if device_name != "" { //key
		params = append(params, fmt.Sprintf("%%%s%%", device_name))
		SQLWhere += ` and device."name" like ?`
	}
	SQLWhere = SQLWhere + " and key != 'systime'"
	// 多租户
	params = append(params, tenant_id)
	SQLWhere += ` and device.tenant_id = ?`

	countsql := "SELECT Count(*) AS count FROM business LEFT JOIN asset ON business.id=asset.business_id LEFT JOIN device ON asset.id=device.asset_id LEFT JOIN ts_kv ON device.id=ts_kv.entity_id " + SQLWhere
	if err := result2.Raw(countsql, params...).Count(&count).Error; err != nil {
		logs.Info(err.Error())
		return tsk, 0
	}
	//select business.name bname,ts_kv.*,concat_ws('-',asset.name,device.name) AS name,device.token
	//FROM ts_kv LEFT join device on device.id=ts_kv.entity_id
	//LEFT JOIN asset  ON asset.id=device.asset_id
	//LEFT JOIN business ON business.id=asset.business_id
	//WHERE 1=1  and ts_kv.ts >= 1654790400000000 and ts_kv.ts < 1655481599000000 ORDER BY ts_kv.ts DESC limit 10 offset 0
	SQL := `select business.name bname,d."name" as gateway_name,ts_kv.*,asset.name asset_name,device.type as plugin_id,
	device.name device_name,device.token FROM business 
	LEFT JOIN asset ON business.id=asset.business_id 
	LEFT JOIN device ON asset.id=device.asset_id 
	left join device d on device.parent_id = d.id 
	LEFT JOIN ts_kv ON device.id=ts_kv.entity_id` + SQLWhere + ` ORDER BY ts_kv.ts DESC`
	if limit > 0 && offset >= 0 {
		SQL = fmt.Sprintf("%s limit ? offset ? ", SQL)
		params = append(params, limit, offset)
	}
	if err := result.Raw(SQL, params...).Scan(&tSKVs).Error; err != nil {
		return tsk, 0
	}
	var deviceModelMap = make(map[string]string)
	var pluginId string
	for _, v := range tSKVs {
		// 从物模型中查找属性的映射
		if v.PluginId != "" {
			if v.PluginId != pluginId {
				deviceModelMap = make(map[string]string) //清空map
				pluginId = v.PluginId
				var DeviceModel DeviceModelService
				deviceModels := DeviceModel.GetDeviceModelDetail(v.PluginId)
				if len(deviceModels) != 0 {
					modelData, err := simplejson.NewJson([]byte(deviceModels[0].ChartData))
					if err != nil {
						logs.Error(err.Error())
					} else {
						propertiesList, err := modelData.Get("tsl").Get("properties").Array()
						logs.Info(propertiesList)
						if err != nil {
							logs.Error(err.Error())
						} else {
							for _, properties := range propertiesList {
								if rulesMap, ok := properties.(map[string]interface{}); ok {
									if name, ok := rulesMap["name"].(string); ok {
										if title, ok := rulesMap["title"].(string); ok {
											deviceModelMap[name] = title
										}
									}

								}
							}

						}
					}
				}
			}
		}
		logs.Info("映射map:", deviceModelMap)
		alias := deviceModelMap[v.Key]
		ts := models.TSKVDblV{
			EntityType:  v.EntityType,
			EntityID:    v.EntityID,
			Key:         v.Key,
			TS:          v.TS,
			BoolV:       v.BoolV,
			StrV:        v.StrV,
			LongV:       v.LongV,
			Token:       v.Token,
			Bname:       v.Bname,
			Name:        v.Name,
			GatewayName: v.GatewayName,
			AssetName:   v.AssetName,
			DeviceName:  v.DeviceName,
			Alias:       alias,
		}
		if v.Key == "TIME" {
			ts.DblV = v.StrV
		} else {
			ts.DblV = v.DblV
		}
		tsk = append(tsk, ts)
	}
	return tsk, count
}

// func (*TSKVService) GetAllByCondition(entity_id string, t int64, start_time string, end_time string) ([]models.TSKV, int64) {
// 	var tSKVs []models.TSKV
// 	var count int64
// 	result := psql.Mydb.Model(&models.TSKV{})
// 	result2 := psql.Mydb.Model(&models.TSKV{})
// 	if entity_id != "" {
// 		result = result.Where("entity_id = ?", entity_id)
// 		result2 = result2.Where("entity_id = ?", entity_id)
// 	}
// 	if t == 1 {
// 		today_start, today_end := timeHelper.Today()
// 		result = result.Where("ts between ? AND ?", today_start*1000, today_end*1000)
// 		result2 = result2.Where("ts between ? AND ?", today_start*1000, today_end*1000)
// 	} else if t == 2 {
// 		week_start, week_end := timeHelper.Week()
// 		result = result.Where("ts between ? AND ?", week_start*1000, week_end*1000)
// 		result2 = result2.Where("ts between ? AND ?", week_start*1000, week_end*1000)
// 	} else if t == 3 {
// 		month_start, month_end := timeHelper.Month()
// 		result = result.Where("ts between ? AND ?", month_start*1000, month_end*1000)
// 		result2 = result2.Where("ts between ? AND ?", month_start*1000, month_end*1000)
// 	} else if t == 4 {
// 		timeTemplate := "2006-01-02 15:04:05"
// 		start_date, _ := time.ParseInLocation(timeTemplate, start_time, time.Local)
// 		end_date, _ := time.ParseInLocation(timeTemplate, end_time, time.Local)
// 		start := start_date.Unix()
// 		end := end_date.Unix()
// 		result = result.Where("ts between ? AND ?", start*1000, end*1000)
// 		result2 = result2.Where("ts between ? AND ?", start*1000, end*1000)
// 	}
// 	result = result.Order("ts desc").Find(&tSKVs)
// 	result2.Count(&count)
// 	if result.Error != nil {
// 		errors.Is(result.Error, gorm.ErrRecordNotFound)
// 	}
// 	if len(tSKVs) == 0 {
// 		tSKVs = []models.TSKV{}
// 	}
// 	return tSKVs, count
// }

// 通过设备ID获取一段时间的数据
func (*TSKVService) GetTelemetry(device_ids []string, startTs int64, endTs int64, rate string) []interface{} {
	var ts_kvs []models.TSKV
	var devices []interface{}
	// var FieldMappingService FieldMappingService
	if len(device_ids) > 0 {
		for _, d := range device_ids {
			device := make(map[string]interface{})
			var result *gorm.DB
			if rate == "" {
				result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("ts >= ? AND ts <= ? AND entity_id = ?", startTs*1000, endTs*1000, d).Order("ts asc").Find(&ts_kvs)
			} else {
				result = psql.Mydb.Raw("select key, bool_v, str_v, long_v, dbl_v, ts from (select row_number() over "+
					"(partition by (times,key)) as seq,* from (select tk.ts/"+rate+" as times ,* from ts_kv tk where"+
					"ts >= ? AND ts <= ? AND entity_id =?) as tks) as group_tk where seq = 1", startTs*1000, endTs*1000, d).Find(&ts_kvs)
			}
			if result.Error != nil {
				errors.Is(result.Error, gorm.ErrRecordNotFound)
			}
			var fields []map[string]interface{}
			if len(ts_kvs) > 0 {
				var i int64 = 0
				var field map[string]interface{}
				field_from := ""
				c := len(ts_kvs)
				for k, v := range ts_kvs {
					// if field_from != v.Key {
					// 	field_from = FieldMappingService.TransformByDeviceid(d, v.Key)
					// 	if field_from == "" {
					// 		field_from = strings.ToLower(v.Key)
					// 	}
					// }
					if v.Key != "" {
						field_from = strings.ToLower(v.Key)
					}
					if i != v.TS {
						if i != 0 {
							fields = append(fields, field)
						}
						field = make(map[string]interface{})
						if fmt.Sprint(v.BoolV) != "" {
							field[field_from] = v.BoolV
						} else if v.StrV != "" {
							field[field_from] = v.StrV
						} else if v.LongV != 0 {
							field[field_from] = v.LongV
						} else if v.DblV != 0 {
							field[field_from] = v.DblV
						} else {
							field[field_from] = 0
						}
						i = v.TS
					} else {
						if fmt.Sprint(v.BoolV) != "" {
							field[field_from] = v.BoolV
						} else if v.StrV != "" {
							field[field_from] = v.StrV
						} else if v.LongV != 0 {
							field[field_from] = v.LongV
						} else if v.DblV != 0 {
							field[field_from] = v.DblV
						} else {
							field[field_from] = 0
						}
						if c == k+1 {
							fields = append(fields, field)
						}
					}
				}
			}
			device["device_id"] = d
			if len(fields) == 0 {
				device["fields"] = make([]string, 0)
				device["latest"] = make([]string, 0)
			} else {
				device["fields"] = fields
				device["latest"] = fields[len(fields)-1]
			}
			devices = append(devices, device)
		}
	} else {
		fmt.Println("device_ids不能为空")
	}
	if len(devices) == 0 {
		devices = make([]interface{}, 0)
	}
	return devices
}

// 通过设备ID获取一段时间的数据
func (*TSKVService) GetHistoryData(device_id string, attributes []string, startTs int64, endTs int64, rate string) (map[string][]interface{}, error) {
	var rsp_map = make(map[string][]interface{})
	dbType, _ := web.AppConfig.String("dbType")
	if dbType == "cassandra" {
		// 通过grpc获取数据
		request := &pb.GetDeviceAttributesHistoryRequest{
			DeviceId:  device_id,
			Attribute: attributes,
			StartTime: startTs,
			EndTime:   endTs,
		}
		r, err := tptodb.TptodbClient.GetDeviceAttributesHistory(context.Background(), request)
		if err != nil {
			logs.Error(err.Error())
			return rsp_map, err
		}
		// r.data为json字符串，转map
		var data map[string][]interface{}
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logs.Error(err.Error())
			return rsp_map, err
		}
		return data, nil
	}
	var ts_kvs []models.TSKV
	var result *gorm.DB

	if rate == "" {
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where(" ts >= ? AND ts <= ? AND entity_id = ? AND key in ?", startTs*1000, endTs*1000, device_id, attributes).Order("ts asc").Find(&ts_kvs)
	} else {
		int_rate, _ := strconv.ParseInt(rate, 10, 64)
		result = psql.Mydb.Raw("select key, bool_v, str_v, long_v, dbl_v, ts from (select row_number() over "+
			"(partition by (times,key) order by ts,key desc) as seq,* from (select tk.ts/? as times ,* from ts_kv tk where"+
			" ts >= ? AND ts <= ? AND entity_id =? AND key in ?) as tks) as group_tk where seq = 1", int_rate, startTs*1000, endTs*1000, device_id, attributes).Find(&ts_kvs)
	}
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return rsp_map, result.Error
	}
	// for _,attribute := range attributes{
	// 	rsp_map[attribute] = []interface{}{}
	// }
	var i int64 = 0
	var j int = -1
	for _, v := range ts_kvs {
		if i != v.TS {
			//第一条进来
			j++
			for _, attribute := range attributes {
				rsp_map[attribute] = append(rsp_map[attribute], nil)
			}
			if fmt.Sprint(v.BoolV) != "" {
				rsp_map[v.Key][j] = v.BoolV
			} else if v.StrV != "" {
				rsp_map[v.Key][j] = v.StrV
			} else if v.LongV != 0 {
				rsp_map[v.Key][j] = v.LongV
			} else if v.DblV != 0 {
				rsp_map[v.Key][j] = v.DblV
			} else {
				rsp_map[v.Key][j] = 0
			}
			i = v.TS
		} else {
			//后续的值
			if fmt.Sprint(v.BoolV) != "" {
				rsp_map[v.Key][j] = v.BoolV
			} else if v.StrV != "" {
				rsp_map[v.Key][j] = v.StrV
			} else if v.LongV != 0 {
				rsp_map[v.Key][j] = v.LongV
			} else if v.DblV != 0 {
				rsp_map[v.Key][j] = v.DblV
			} else {
				rsp_map[v.Key][j] = 0
			}
		}
	}
	return rsp_map, nil
}

// 通过设备ID删除历史数据
func (*TSKVService) DeleteHistoryData(tenantId string, deviceId string, attributes string) (bool, error) {
	var tsKvs []models.TSKV
	var tsKvLatest []models.TSKVLatest
	var device models.Device

	// 判断当前租户是否有权限删除
	result := psql.Mydb.Where("id = ? AND tenant_id = ?", deviceId, tenantId).First(&device)
	if result.Error != nil {
		return false, errors.New("没有权限删除该设备的数据")
	}

	// 使用事务同步删除历史数据和最新数据
	flag := psql.Mydb.Transaction(func(tx *gorm.DB) error {

		// 删除当前数据
		result = tx.Where(" entity_id = ? AND key = ?", deviceId, attributes).Delete(&tsKvLatest)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return result.Error
		}

		// 删除历史数据
		result = tx.Where(" entity_id = ? AND key = ?", deviceId, attributes).Delete(&tsKvs)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return result.Error
		}

		return nil
	})

	if flag != nil {
		return false, flag
	}
	return true, flag
}

// 返回最新一条的设备数据，用来判断设备状态（待接入，异常，正常）
func (*TSKVService) Status(device_id string) (*models.TSKVLatest, int64) {
	var TSKVLatest models.TSKVLatest
	result := psql.Mydb.Where("entity_id = ?", device_id).Order("ts desc").First(&TSKVLatest)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &TSKVLatest, result.RowsAffected
}

// GetCurrentData 通过设备ID和属性来获取数据
func (*TSKVService) GetCurrentData(device_id string, attributes []string) (map[string]interface{}, error) {
	dbType, _ := web.AppConfig.String("dbType")
	if dbType == "cassandra" {
		return fetchFromCassandra(device_id, attributes)
	}
	return fetchFromSQL(device_id, attributes)
}

// fetchFromCassandra 从Cassandra数据库中获取数据
func fetchFromCassandra(device_id string, attributes []string) (map[string]interface{}, error) {
	request := &pb.GetDeviceAttributesCurrentsRequest{
		DeviceId:  device_id,
		Attribute: attributes,
	}

	// 通过grpc获取数据
	r, err := tptodb.TptodbClient.GetDeviceAttributesCurrents(context.Background(), request)
	if err != nil {
		logs.Error(err.Error())
		return nil, err
	}

	var data map[string]interface{}
	if err = json.Unmarshal([]byte(r.Data), &data); err != nil {
		logs.Error(err.Error())
		return nil, err
	}
	return data, nil
}

// fetchFromSQL 从SQL数据库中获取数据
func fetchFromSQL(device_id string, attributes []string) (map[string]interface{}, error) {
	var ts_kvs []models.TSKVLatest
	var query string
	args := make([]interface{}, 0, len(attributes)+1)
	args = append(args, device_id)

	if len(attributes) == 0 {
		query = "SELECT key, bool_v, str_v, long_v, dbl_v, ts FROM ts_kv_latest WHERE entity_id = ?"
	} else {
		if !contains(attributes, "systime") {
			attributes = append(attributes, "systime")
		}
		placeholders := strings.Trim(strings.Repeat("?,", len(attributes)), ",")
		query = fmt.Sprintf("SELECT key, bool_v, str_v, long_v, dbl_v, ts FROM ts_kv_latest WHERE entity_id = ? AND key IN (%s)", placeholders)
		for _, attr := range attributes {
			args = append(args, attr)
		}
	}
	if err := psql.Mydb.Raw(query, args...).Scan(&ts_kvs).Error; err != nil {
		return nil, err
	}
	field := make(map[string]interface{}, len(ts_kvs))
	fmt.Println(ts_kvs)

	for _, v := range ts_kvs {
		if v.Key == "" {
			continue
		}
		field[v.Key] = getValue(v)
	}
	return field, nil
}

// contains 判断字符串切片中是否包含特定的字符串
func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
}

// getValue 根据TSKVLatest的值返回相应的数据类型的值
func getValue(v models.TSKVLatest) interface{} {
	if fmt.Sprint(v.BoolV) != "" {
		return v.BoolV
	}
	if v.StrV != "" {
		return v.StrV
	}
	if v.LongV != 0 {
		return v.LongV
	}
	if v.DblV != 0 {
		return v.DblV
	}
	return 0
}

// 根据业务id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）
func (*TSKVService) GetCurrentDataByBusiness(business string) map[string]interface{} {
	var DeviceService DeviceService
	deviceList, deviceCount := DeviceService.GetDevicesByBusinessID(business)
	log.Println(deviceList)
	log.Println(deviceCount)
	var devices []map[string]interface{}
	if len(deviceList) != 0 {
		for _, device := range deviceList {
			var deviceData = make(map[string]interface{})
			deviceData["device_id"] = device.ID
			deviceData["asset_id"] = device.AssetID
			deviceData["customer_id"] = device.CustomerID
			deviceData["additional_id"] = device.AdditionalInfo
			deviceData["chart_option"] = device.ChartOption
			deviceData["label"] = device.Label
			deviceData["name"] = device.Name
			deviceData["protocol"] = device.Protocol
			deviceData["publish"] = device.Publish
			deviceData["subscribe"] = device.Subscribe
			deviceData["type"] = device.Type
			deviceData["d_id"] = device.DId
			deviceData["location"] = device.Location
			var TSKVService TSKVService
			fields, _ := TSKVService.GetCurrentData(device.ID, nil)
			if len(fields) == 0 {
				deviceData["values"] = make(map[string]interface{}, 0)
				deviceData["status"] = "0"
			} else {
				// 0-带接入 1-正常 2-异常
				var state string
				tsl, tsc := TSKVService.Status(device.ID)
				if tsc == 0 {
					state = "0"
				} else {
					ts := time.Now().UnixMicro()
					//300000000
					if (ts - tsl.TS) > 300000000 {
						state = "2"
					} else {
						state = "1"
					}
				}
				deviceData["status"] = state
				deviceData["values"] = fields
			}
			devices = append(devices, deviceData)
		}
	} else {
		devices = make([]map[string]interface{}, 0)
	}
	var datas = make(map[string]interface{})
	datas["devices"] = devices
	datas["devicesTotal"] = deviceCount
	return datas
}

// 根据设备分组id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）
func (*TSKVService) GetCurrentDataByAsset(asset_id string) map[string]interface{} {
	var DeviceService DeviceService
	deviceList, deviceCount := DeviceService.GetDevicesInfoAndCurrentByAssetID(asset_id)
	log.Println(deviceList)
	log.Println(deviceCount)
	var devices []map[string]interface{}
	if len(deviceList) != 0 {
		for _, device := range deviceList {
			var deviceData = make(map[string]interface{})
			deviceData["device_id"] = device.ID
			deviceData["asset_id"] = device.AssetID
			deviceData["customer_id"] = device.CustomerID
			deviceData["additional_id"] = device.AdditionalInfo
			deviceData["chart_option"] = device.ChartOption
			deviceData["label"] = device.Label
			deviceData["name"] = device.Name
			deviceData["protocol"] = device.Protocol
			deviceData["publish"] = device.Publish
			deviceData["subscribe"] = device.Subscribe
			deviceData["type"] = device.Type
			deviceData["d_id"] = device.DId
			deviceData["location"] = device.Location
			var TSKVService TSKVService
			fields, _ := TSKVService.GetCurrentData(device.ID, nil)
			if len(fields) == 0 {
				deviceData["values"] = make(map[string]interface{}, 0)
				deviceData["status"] = "0"
			} else {
				// 0-带接入 1-正常 2-异常
				var state string
				tsl, tsc := TSKVService.Status(device.ID)
				if tsc == 0 {
					state = "0"
				} else {
					ts := time.Now().UnixMicro()
					//300000000
					if (ts - tsl.TS) > 300000000 {
						state = "2"
					} else {
						state = "1"
					}
				}
				deviceData["status"] = state
				deviceData["values"] = fields
			}
			devices = append(devices, deviceData)
		}
	} else {
		devices = make([]map[string]interface{}, 0)
	}
	var datas = make(map[string]interface{})
	datas["devices"] = devices
	datas["devicesTotal"] = deviceCount
	return datas
}

// 根据设备分组id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）app展示接口
func (*TSKVService) GetCurrentDataByAssetA(asset_id string) map[string]interface{} {
	var DeviceService DeviceService
	deviceList, deviceCount := DeviceService.GetDevicesInfoAndCurrentByAssetID(asset_id)
	log.Println(deviceList)
	log.Println(deviceCount)
	var devices []map[string]interface{}
	if len(deviceList) != 0 {
		for _, device := range deviceList {
			var deviceData = make(map[string]interface{})
			deviceData["device_id"] = device.ID
			deviceData["asset_id"] = device.AssetID
			deviceData["customer_id"] = device.CustomerID
			deviceData["additional_id"] = device.AdditionalInfo
			deviceData["chart_option"] = device.ChartOption
			deviceData["label"] = device.Label
			deviceData["name"] = device.Name
			deviceData["protocol"] = device.Protocol
			deviceData["publish"] = device.Publish
			deviceData["subscribe"] = device.Subscribe
			deviceData["type"] = device.Type
			deviceData["d_id"] = device.DId
			deviceData["location"] = device.Location

			var TSKVService TSKVService
			fields, _ := TSKVService.GetCurrentData(device.ID, nil)
			if len(fields) == 0 {
				deviceData["values"] = make(map[string]interface{}, 0)
				deviceData["status"] = "0"
			} else {
				// 0-带接入 1-正常 2-异常
				var state string
				tsl, tsc := TSKVService.Status(device.ID)
				if tsc == 0 {
					state = "0"
				} else {
					ts := time.Now().UnixMicro()
					//300000000
					if (ts - tsl.TS) > 300000000 {
						state = "2"
					} else {
						state = "1"
					}
				}
				deviceData["status"] = state
				//deviceData["values"] = fields[0]
				// current_data:[{},{}]
				var current_data []interface{}
				var AssetService AssetService
				dd := AssetService.ExtensionName(device.Type)
				if len(dd) > 0 {
					for _, wv := range dd[0].Field {
						var currentData = make(map[string]interface{})
						currentData["key"] = wv.Key
						currentData["name"] = wv.Name
						currentData["symbol"] = wv.Symbol
						currentData["type"] = wv.Type
						currentData["value"] = fields[wv.Key]
						current_data = append(current_data, currentData)
					}

				}
				deviceData["current_data"] = current_data
			}
			devices = append(devices, deviceData)
		}
	} else {
		devices = make([]map[string]interface{}, 0)
	}
	var datas = make(map[string]interface{})
	datas["devices"] = devices
	datas["devicesTotal"] = deviceCount
	return datas
}

// 根据设备id查询key的历史数据
func (*TSKVService) GetHistoryDataByKey(device_id string, key string, startTs int64, endTs int64, limit int64) (map[string]interface{}, error) {
	var rsp_map = make(map[string]interface{})
	dbType, _ := web.AppConfig.String("dbType")
	if dbType == "cassandra" {
		// 通过grpc获取数据
		request := &pb.GetDeviceHistoryRequest{
			DeviceId:  device_id,
			Key:       key,
			StartTime: startTs,
			EndTime:   endTs,
			Limit:     limit,
		}
		r, err := tptodb.TptodbClient.GetDeviceHistory(context.Background(), request)
		if err != nil {
			logs.Error(err.Error())
			return rsp_map, err
		}
		// r.data为json字符串，转map
		var data map[string]interface{}
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logs.Error(err.Error())
			return rsp_map, err
		}
		return data, nil
	}
	var ts_kvs []models.TSKV
	// 时间跨度不能超过100天，超过100天按100天算
	if (endTs - startTs) > 8640000000 {
		startTs = endTs - 8640000000
	}
	// 获取total
	var count int64
	result2 := psql.Mydb.Model(&models.TSKV{}).Where("ts >= ? AND ts <= ? AND entity_id = ? AND key = ?", startTs*1000, endTs*1000, device_id, key).Count(&count)
	if result2.Error != nil {
		errors.Is(result2.Error, gorm.ErrRecordNotFound)
		return rsp_map, result2.Error
	}
	result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("ts >= ? AND ts <= ? AND entity_id = ? AND key = ?", startTs*1000, endTs*1000, device_id, key).Order("ts desc").Limit(int(limit)).Find(&ts_kvs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return rsp_map, result.Error
	}
	var dataMap []map[string]interface{}
	for _, v := range ts_kvs {
		var data = make(map[string]interface{})
		data["ts"] = v.TS
		if fmt.Sprint(v.StrV) != "" {
			data["value"] = v.StrV
		} else if v.DblV != 0 {
			data["value"] = v.DblV
		}
		dataMap = append(dataMap, data)
	}
	rsp_map["data"] = dataMap
	rsp_map["total"] = count
	return rsp_map, nil

}

// 根据设id分页查询设备kv，以{k:v,k:v...}方式返回
func (*TSKVService) DeviceHistoryData(device_id string, current int, size int) ([]map[string]interface{}, int64) {
	var fields []map[string]interface{}
	var ts_kvs []models.TSKV
	var count int64
	result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts desc").Limit(size).Offset((current - 1) * size).Find(&ts_kvs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	psql.Mydb.Model(&models.TSKV{}).Where("entity_id = ?", device_id).Count(&count)

	if len(ts_kvs) > 0 {
		var i int64 = 0
		var field map[string]interface{}
		field_from := ""
		c := len(ts_kvs)
		for k, v := range ts_kvs {
			if v.Key != "" {
				field_from = strings.ToLower(v.Key)
			}
			if i != v.TS {
				if i != 0 {
					fields = append(fields, field)
				}
				field = make(map[string]interface{})
				if fmt.Sprint(v.BoolV) != "" {
					field[field_from] = v.BoolV
				} else if v.StrV != "" {
					field[field_from] = v.StrV
				} else if v.LongV != 0 {
					field[field_from] = v.LongV
				} else if v.DblV != 0 {
					field[field_from] = v.DblV
				} else {
					field[field_from] = 0
				}
				i = v.TS
			} else {
				if fmt.Sprint(v.BoolV) != "" {
					field[field_from] = v.BoolV
				} else if v.StrV != "" {
					field[field_from] = v.StrV
				} else if v.LongV != 0 {
					field[field_from] = v.LongV
				} else if v.DblV != 0 {
					field[field_from] = v.DblV
				} else {
					field[field_from] = 0
				}
				if c == k+1 {
					fields = append(fields, field)
				}
			}
		}
	}
	return fields, count
}

// 删除当前值根据设备id
func (*TSKVService) DeleteCurrentDataByDeviceId(deviceId string) {
	rtsl := psql.Mydb.Where("entity_id = ?", deviceId).Delete(&models.TSKVLatest{})
	if rtsl.Error != nil {
		log.Println(rtsl.Error)
	}
}

// 通过设备ID获取设备当前值和插件映射属性
func (*TSKVService) GetCurrentDataAndMap(device_id string, attributes []string) ([]map[string]interface{}, error) {

	logs.Info("**********************************************")
	var fields []map[string]interface{}
	dbType, _ := web.AppConfig.String("dbType")
	if dbType == "cassandra" {
		// 通过grpc获取数据
		request := &pb.GetDeviceAttributesCurrentsRequest{
			DeviceId:  device_id,
			Attribute: attributes,
		}
		r, err := tptodb.TptodbClient.GetDeviceAttributesCurrents(context.Background(), request)
		if err != nil {
			logs.Error(err.Error())
			return fields, err
		}
		// r.data为json字符串，转map
		var data map[string]interface{}
		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logs.Error(err.Error())
			return fields, err
		}
		fields = append(fields, data)
		// 查询物模型映射
		var DeviceService DeviceService
		device, _ := DeviceService.GetDeviceByID(device_id)
		var DeviceModelService DeviceModelService
		device_plugin := DeviceModelService.GetDeviceModelDetail(device.Type)
		logs.Info("设备插件", device_plugin)
		if len(device_plugin) == 0 {
			return fields, nil
		}
		type Properties struct {
			DataType  string `json:"dataType"`
			DataRange string `json:"dataRange"`
			Unit      string `json:"unit"`
			Title     string `json:"title"`
			Name      string `json:"name"`
		}
		type Tsl struct {
			Properties []Properties `json:"properties"`
		}
		type Data struct {
			Tsl Tsl `json:"tsl"`
		}
		//映射
		var device_attribute_map Data
		var properties_key = make(map[string]string)
		var properties_symbol = make(map[string]string)
		if err := json.Unmarshal([]byte(device_plugin[0].ChartData), &device_attribute_map); err != nil {
			logs.Info(err.Error())
		} else {
			for _, a_map := range device_attribute_map.Tsl.Properties {
				if a_map.Title != "" {
					properties_key[a_map.Name] = a_map.Title
				}
				if a_map.Unit != "-" && a_map.Unit != "" {
					properties_symbol[a_map.Name] = a_map.Unit
				}
			}
		}
		for k, v := range fields {
			for key, value := range v {
				if properties_key[key] != "" {
					fields[k][properties_key[key]] = value
					delete(fields[k], key)
				}
			}
		}
		return fields, nil
	}
	var ts_kvs []models.TSKVLatest
	var result *gorm.DB
	if attributes == nil {
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts asc").Find(&ts_kvs)
	} else {
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ? AND key in ?", device_id, attributes).Order("ts asc").Find(&ts_kvs)
	}
	if result.Error != nil {
		return fields, result.Error
	}
	if len(ts_kvs) > 0 {
		var field = make(map[string]interface{})
		// // 0-未接入 1-正常 2-异常
		// var state string
		// var TSKVService TSKVService
		// tsl, tsc := TSKVService.Status(device_id)
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
		//查询设备的插件id
		var DeviceService DeviceService
		device, _ := DeviceService.GetDeviceByID(device_id)
		var DeviceModelService DeviceModelService
		device_plugin := DeviceModelService.GetDeviceModelDetail(device.Type)
		logs.Info("设备插件", device_plugin)
		if len(device_plugin) == 0 {
			return fields, nil
		}
		type Properties struct {
			DataType  string `json:"dataType"`
			DataRange string `json:"dataRange"`
			Unit      string `json:"unit"`
			Title     string `json:"title"`
			Name      string `json:"name"`
		}
		type Tsl struct {
			Properties []Properties `json:"properties"`
		}
		type Data struct {
			Tsl Tsl `json:"tsl"`
		}
		//映射
		var device_attribute_map Data
		var properties_key = make(map[string]string)
		var properties_symbol = make(map[string]string)
		if err := json.Unmarshal([]byte(device_plugin[0].ChartData), &device_attribute_map); err != nil {
			logs.Info(err.Error())
		} else {
			for _, a_map := range device_attribute_map.Tsl.Properties {
				if a_map.Title != "" {
					properties_key[a_map.Name] = a_map.Title
				}
				if a_map.Unit != "-" && a_map.Unit != "" {
					properties_symbol[a_map.Name] = a_map.Unit
				}
			}
		}
		c := len(ts_kvs)
		for k, device_c := range ts_kvs {
			if device_c.Key == "" {
				continue
			}
			field_from := device_c.Key
			if properties_key[device_c.Key] != "" {
				field_from = properties_key[device_c.Key]
			}

			if fmt.Sprint(device_c.BoolV) != "" {
				field[field_from] = device_c.BoolV + properties_symbol[device_c.Key]
			} else if device_c.StrV != "" {
				field[field_from] = device_c.StrV + properties_symbol[device_c.Key]
			} else if device_c.LongV != 0 {
				field[field_from] = strconv.FormatInt(device_c.LongV, 10) + properties_symbol[device_c.Key]
			} else if device_c.DblV != 0 {
				var result = fmt.Sprintf("%f", device_c.DblV)
				for strings.HasSuffix(result, "0") {
					result = strings.TrimSuffix(result, "0")
				}
				result = strings.TrimSuffix(result, ".")
				field[field_from] = result + properties_symbol[device_c.Key]
			} else {
				field[field_from] = "0" + properties_symbol[device_c.Key]
			}
			if c == k+1 {
				fields = append(fields, field)
			}
		}
	}
	return fields, nil
}

// 设备在线离线判断
func (*TSKVService) DeviceOnline(device_id string, interval int64) (string, error) {
	var ts_kvs models.TSKVLatest
	result := psql.Mydb.Select("ts").Where("entity_id = ? AND key ='systime'", device_id).Order("ts asc").Find(&ts_kvs)
	if result.Error != nil {
		return "0", result.Error
	}
	ts := time.Now().UnixMicro()
	//300000000 300秒 5分钟
	logs.Info("判断时间阈值", interval)
	if interval == int64(0) {
		interval = 300
	} else {
		logs.Info("时间阈值：", interval)
	}
	var state string = "0"
	if (ts - ts_kvs.TS) > interval*1000000 {
		state = "0"
	} else {
		state = "1"
	}
	return state, nil
}

// 查询设备当前值，与物模型映射，返回map列表
func (*TSKVService) GetCurrentDataAndMapList(device_id string) ([]map[string]interface{}, error) {
	var fields []map[string]interface{}
	dbType, _ := web.AppConfig.String("dbType")
	if dbType == "cassandra" {
		return fields, errors.New("cassandra不支持此接口")
	}
	result := psql.Mydb.Model(&models.TSKVLatest{}).Select("key, str_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts desc").Find(&fields)
	if result.Error != nil {
		return fields, result.Error
	}
	if len(fields) > 0 {
		// 查询物模型映射
		var device models.Device
		result := psql.Mydb.Where("id = ?", device_id).First(&device)
		if result.Error != nil {
			return fields, result.Error
		}
		var DeviceModelService DeviceModelService
		attributeList, err := DeviceModelService.GetModelByPluginId(device.Type)
		if err != nil {
			logs.Error(err.Error())
			return fields, nil
		}
		var typeMap = make(map[string]string)
		for _, attribute := range attributeList {
			if attributeMap, ok := attribute.(map[string]interface{}); ok {
				if name, ok := attributeMap["name"].(string); ok {
					typeMap[name] = attributeMap["title"].(string)
				}
			}
		}
		for i, v := range fields {
			if typeMap[v["key"].(string)] != "" {
				fields[i]["name"] = typeMap[v["key"].(string)]
			}
		}
	}
	return fields, nil
}

// 获取不聚合的数据
func (*TSKVService) GetKVDataWithNoAggregate(deviceId, key string, sTime, eTime int64) ([]map[string]interface{}, error) {

	var fields []models.TSKV
	resultData := psql.Mydb.
		Select("ts, bool_v, str_v, long_v, dbl_v").
		Where("entity_id = ? and key = ? and ts >= ? and ts <= ?", deviceId, key, sTime, eTime).
		Find(&fields)
	if resultData.Error != nil {
		return nil, resultData.Error
	}
	timeSeries := make([]map[string]interface{}, resultData.RowsAffected)
	if resultData.RowsAffected > 0 {
		for i, v := range fields {
			tmpMap := make(map[string]interface{})
			tmpMap["x"] = v.TS // 横轴为时间
			// 处理横轴
			if fmt.Sprint(v.BoolV) != "" {
				tmpMap["y"] = v.BoolV
			} else if v.StrV != "" {
				tmpMap["y"] = v.StrV
			} else if v.LongV != 0 {
				tmpMap["y"] = v.LongV
			} else if v.DblV != 0 {
				tmpMap["y"] = v.DblV
			} else {
				tmpMap["y"] = 0
			}
			timeSeries[i] = tmpMap
		}
	}

	return timeSeries, nil
}
