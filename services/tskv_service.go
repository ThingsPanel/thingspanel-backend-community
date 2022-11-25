package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/zenghouchao/timeHelper"
	"gorm.io/gorm"
)

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

// 获取全部TSKV
func (*TSKVService) All() ([]models.TSKV, int64) {
	var tskvs []models.TSKV
	var count int64
	result := psql.Mydb.Model(&tskvs).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(tskvs) == 0 {
		tskvs = []models.TSKV{}
	}
	return tskvs, count
}

// 接收硬件消息(设备在线离线)
func (*TSKVService) MsgStatus(body []byte) bool {
	logs.Info("-------------------------------")
	logs.Info(string(body))
	logs.Info("-------------------------------")
	payload, err := verifyPayload(body)
	if err != nil {
		logs.Error(err.Error())
		return false
	}
	var device_data = make(map[string]interface{})
	if err := json.Unmarshal(payload.Values, &device_data); err != nil {
		logs.Error("解析消息失败:", err)
		return false
	}
	device_id := redis.GetStr("token" + payload.Token)
	d := models.TSKVLatest{
		EntityType: "DEVICE",
		EntityID:   device_id,
		Key:        "SYS_ONLINE",
		TS:         time.Now().UnixMicro(),
		StrV:       fmt.Sprint(device_data["SYS_ONLINE"]),
	}
	rtsl := psql.Mydb.Save(&d)
	if rtsl.Error != nil {
		log.Println(rtsl.Error)
	}
	return true
}

// 接收网关消息
func (*TSKVService) GatewayMsgProc(body []byte, topic string) bool {
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
				var TSKVService TSKVService
				TSKVService.MsgProc(sub_payload_bytes, topic)
			}
		}
	}
	return true
}

// 接收硬件消息
func (*TSKVService) MsgProc(body []byte, topic string) bool {
	logs.Info("-------------------------------")
	logs.Info("来自直连设备/网关解析后的子设备的消息：")
	logs.Info(string(body))
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
	logs.Info("转码后:", string(req))
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
	if redis.GetStr("warning"+device.ID) != "1" {
		var WarningConfigService WarningConfigService
		WarningConfigService.WarningConfigCheck(device.ID, payload_map)
	}
	// 设备触发自动化
	var ConditionsService ConditionsService
	ConditionsService.ConditionsConfigCheck(device.ID, payload_map)
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
			}
		case string:
			d = models.TSKV{
				EntityType: "DEVICE",
				EntityID:   device.ID,
				Key:        k,
				TS:         ts,
				StrV:       value,
			}
		case bool:
			d = models.TSKV{
				EntityType: "DEVICE",
				EntityID:   device.ID,
				Key:        k,
				TS:         ts,
				BoolV:      strconv.FormatBool(value),
			}
		case float64:
			d = models.TSKV{
				EntityType: "DEVICE",
				EntityID:   device.ID,
				Key:        k,
				TS:         ts,
				DblV:       value,
			}
		default:
			d = models.TSKV{
				EntityType: "DEVICE",
				EntityID:   device.ID,
				Key:        k,
				TS:         ts,
				StrV:       fmt.Sprint(value),
			}
		}
		// 更新当前值表
		l := models.TSKVLatest{}
		utils.StructAssign(&l, &d)
		var latestCount int64
		psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ?", l.EntityType, l.EntityID, l.Key).Count(&latestCount)
		if latestCount <= 0 {
			rtsl := psql.Mydb.Create(&l)
			if rtsl.Error != nil {
				log.Println(rtsl.Error)
			}
		} else {
			rtsl := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ?", l.EntityType, l.EntityID,
				l.Key).Updates(map[string]interface{}{"entity_type": l.EntityType, "entity_id": l.EntityID, "key": l.Key, "ts": l.TS, "bool_v": l.BoolV, "long_v": l.LongV, "str_v": l.StrV, "dbl_v": l.DblV})
			if rtsl.Error != nil {
				log.Println(rtsl.Error)
			}
		}
		// ts_kv入库
		logs.Debug("tskv入库数据：", d)
		rts := psql.Mydb.Create(&d)
		if rts.Error != nil {
			log.Println(rts.Error)
			return false
		}
	}
	return true
}

// 分页查询数据
func (*TSKVService) Paginate(business_id, asset_id, token string, t_type int64, start_time string, end_time string, limit int, offset int, key string, device_name string) ([]models.TSKVDblV, int64) {
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
		SQLWhere = SQLWhere + " and key = '" + key + "'"
	}
	if device_name != "" { //key
		SQLWhere = SQLWhere + ` and device."name" like '%` + device_name + "%'"
	}
	SQLWhere = SQLWhere + " and key != 'systime'"
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
	SQL := `select business.name bname,d."name" as gateway_name,ts_kv.*,asset.name asset_name,
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

	for _, v := range tSKVs {
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

func (*TSKVService) GetAllByCondition(entity_id string, t int64, start_time string, end_time string) ([]models.TSKV, int64) {
	var tSKVs []models.TSKV
	var count int64
	result := psql.Mydb.Model(&models.TSKV{})
	result2 := psql.Mydb.Model(&models.TSKV{})
	if entity_id != "" {
		result = result.Where("entity_id = ?", entity_id)
		result2 = result2.Where("entity_id = ?", entity_id)
	}
	if t == 1 {
		today_start, today_end := timeHelper.Today()
		result = result.Where("ts between ? AND ?", today_start*1000, today_end*1000)
		result2 = result2.Where("ts between ? AND ?", today_start*1000, today_end*1000)
	} else if t == 2 {
		week_start, week_end := timeHelper.Week()
		result = result.Where("ts between ? AND ?", week_start*1000, week_end*1000)
		result2 = result2.Where("ts between ? AND ?", week_start*1000, week_end*1000)
	} else if t == 3 {
		month_start, month_end := timeHelper.Month()
		result = result.Where("ts between ? AND ?", month_start*1000, month_end*1000)
		result2 = result2.Where("ts between ? AND ?", month_start*1000, month_end*1000)
	} else if t == 4 {
		timeTemplate := "2006-01-02 15:04:05"
		start_date, _ := time.ParseInLocation(timeTemplate, start_time, time.Local)
		end_date, _ := time.ParseInLocation(timeTemplate, end_time, time.Local)
		start := start_date.Unix()
		end := end_date.Unix()
		result = result.Where("ts between ? AND ?", start*1000, end*1000)
		result2 = result2.Where("ts between ? AND ?", start*1000, end*1000)
	}
	result = result.Order("ts desc").Find(&tSKVs)
	result2.Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(tSKVs) == 0 {
		tSKVs = []models.TSKV{}
	}
	return tSKVs, count
}

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
func (*TSKVService) GetHistoryData(device_id string, attributes []string, startTs int64, endTs int64, rate string) map[string][]interface{} {
	var ts_kvs []models.TSKV
	var result *gorm.DB
	var rsp_map = make(map[string][]interface{})
	if rate == "" {
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where(" ts >= ? AND ts <= ? AND entity_id = ? AND key in ?", startTs*1000, endTs*1000, device_id, attributes).Order("ts asc").Find(&ts_kvs)
	} else {
		result = psql.Mydb.Raw("select key, bool_v, str_v, long_v, dbl_v, ts from (select row_number() over "+
			"(partition by (times,key)) as seq,* from (select tk.ts/"+rate+" as times ,* from ts_kv tk where"+
			" ts >= ? AND ts <= ? AND entity_id =? AND key in ?) as tks) as group_tk where seq = 1", startTs*1000, endTs*1000, device_id, attributes).Find(&ts_kvs)
	}
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return rsp_map
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
	return rsp_map
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

// 通过设备ID获取设备当前值
func (*TSKVService) GetCurrentData(device_id string, attributes []string) []map[string]interface{} {
	var fields []map[string]interface{}
	var ts_kvs []models.TSKVLatest
	device := make(map[string]interface{})
	var result *gorm.DB
	if attributes == nil {
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts asc").Find(&ts_kvs)
	} else {
		//给返回加上systime
		flag := true
		for _, attribute := range attributes {
			if attribute == "systime" {
				flag = false
			}
		}
		if flag {
			attributes = append(attributes, "systime")
		}
		result = psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ? AND key in ?", device_id, attributes).Order("ts asc").Find(&ts_kvs)
	}
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return fields
	}
	if len(ts_kvs) > 0 {
		//var i int64 = 0
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
		field_from := ""
		c := len(ts_kvs)
		for k, v := range ts_kvs {
			if v.Key == "" {
				continue
			}
			field_from = v.Key
			// if i != v.TS {
			// 	if i != 0 {
			// 		fields = append(fields, field)
			// 	}
			// 	field = make(map[string]interface{})
			// 	//field["status"] = state
			// 	if fmt.Sprint(v.BoolV) != "" {
			// 		field[field_from] = v.BoolV
			// 	} else if v.StrV != "" {
			// 		field[field_from] = v.StrV
			// 	} else if v.LongV != 0 {
			// 		field[field_from] = v.LongV
			// 	} else if v.DblV != 0 {
			// 		field[field_from] = v.DblV
			// 	} else {
			// 		field[field_from] = 0
			// 	}
			// 	i = v.TS
			// } else {
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
			// }
		}
	}
	if len(fields) == 0 {
		device["fields"] = make([]string, 0)
		device["latest"] = make([]string, 0)
	} else {
		device["fields"] = fields
		device["latest"] = fields[len(fields)-1]
	}
	return fields
}

//根据业务id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）
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
			fields := TSKVService.GetCurrentData(device.ID, nil)
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
				deviceData["values"] = fields[0]
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

//根据设备分组id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）
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
			fields := TSKVService.GetCurrentData(device.ID, nil)
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
				deviceData["values"] = fields[0]
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

//根据设备分组id查询所有设备和设备当前值（包含设备状态）（在线数量?，离线数量?）app展示接口
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
			fields := TSKVService.GetCurrentData(device.ID, nil)
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
						currentData["value"] = fields[0][wv.Key]
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

// 根据设id分页查询设备kv，以{k:v,k:v...}方式返回
func (*TSKVService) DeviceHistoryData(device_id string, current int, size int) ([]map[string]interface{}, int64) {
	var ts_kvs []models.TSKV
	var count int64
	result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts desc").Limit(size).Offset((current - 1) * size).Find(&ts_kvs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	psql.Mydb.Model(&models.TSKV{}).Where("entity_id = ?", device_id).Count(&count)
	var fields []map[string]interface{}
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

//删除当前值根据设备id
func (*TSKVService) DeleteCurrentDataByDeviceId(deviceId string) {
	rtsl := psql.Mydb.Where("entity_id = ?", deviceId).Delete(&models.TSKVLatest{})
	if rtsl.Error != nil {
		log.Println(rtsl.Error)
	}
}
