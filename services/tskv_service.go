package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

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
	Token  string                 `json:"token"`
	Values map[string]interface{} `json:"values"`
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

// 接收硬件消息
func (*TSKVService) MsgProc(body []byte) bool {
	payload := &mqttPayload{}
	if err := json.Unmarshal(body, payload); err != nil {
		fmt.Println("Msg Consumer: Cannot unmarshal msg payload to JSON:", err)
		return false
	}
	if len(payload.Token) == 0 {
		fmt.Println("Msg Consumer: Payload token missing")
		return false
	}
	if len(payload.Values) == 0 {
		fmt.Println("Msg Consumer: Payload values missing")
		return false
	}
	var device models.Device
	var d models.TSKV
	//查询token，验证token
	result := psql.Mydb.Where("token = ?", payload.Token).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if result.RowsAffected > 0 {
		// 查询警告
		var WarningConfigService WarningConfigService
		WarningConfigService.WarningConfigCheck(device.ID, payload.Values)
		// 设备触发自动化
		var ConditionsService ConditionsService
		ConditionsService.ConditionsConfigCheck(device.ID, payload.Values)

		ts := time.Now().UnixMicro()
		//查找field_mapping表替换value里面的字段
		var FieldMappingService FieldMappingService
		FieldMapping, num := FieldMappingService.GetByDeviceid(device.ID)
		if num <= 0 {
			return false
		}
		field_map := map[string]string{}
		for _, v := range FieldMapping {
			field_map[v.FieldFrom] = v.FieldTo
		}

		result := psql.Mydb.Where("token = ?", payload.Token).First(&device)
		if result.Error != nil {
			errors.Is(result.Error, gorm.ErrRecordNotFound)
		}

		for k, v := range payload.Values {

			key, ok := field_map[k]
			if !ok {
				continue
			}

			switch value := v.(type) {
			case int64:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        strings.ToUpper(key),
					TS:         ts,
					LongV:      value,
				}
			case string:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        strings.ToUpper(key),
					TS:         ts,
					StrV:       value,
				}
			case bool:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        strings.ToUpper(key),
					TS:         ts,
					BoolV:      strconv.FormatBool(value),
				}
			case float64:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        strings.ToUpper(key),
					TS:         ts,
					DblV:       value,
				}
			default:
				d = models.TSKV{
					EntityType: "DEVICE",
					EntityID:   device.ID,
					Key:        strings.ToUpper(key),
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
				rtsl := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ?", l.EntityType, l.EntityID, l.Key).Updates(&l)
				if rtsl.Error != nil {
					log.Println(rtsl.Error)
				}
			}
			rts := psql.Mydb.Create(&d)
			if rts.Error != nil {
				log.Println(rts.Error)
				return false
			}
		}
		//存入系统时间
		currentTime := fmt.Sprintf(time.Now().Format("2006-01-02 15:04:05"))
		d = models.TSKV{
			EntityType: "DEVICE",
			EntityID:   device.ID,
			Key:        strings.ToUpper("systime"),
			TS:         ts,
			StrV:       currentTime,
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
			rtsl := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_type = ? and entity_id = ? and key = ?", l.EntityType, l.EntityID, l.Key).Updates(&l)
			if rtsl.Error != nil {
				log.Println(rtsl.Error)
			}
		}
		// 存储数据
		rts := psql.Mydb.Create(&d)
		if rts.Error != nil {
			log.Println(rts.Error)
			return false
		}
		return true
	}
	fmt.Println("token not matched")
	return false
}

// 分页查询数据
func (*TSKVService) Paginate(business_id, asset_id, token string, t_type int64, start_time string, end_time string, limit int, offset int, key string) ([]models.TSKVDblV, int64) {
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
	SQL := "select business.name bname,ts_kv.*,concat_ws('-',asset.name,device.name) AS name,device.token FROM business LEFT JOIN asset ON business.id=asset.business_id LEFT JOIN device ON asset.id=device.asset_id LEFT JOIN ts_kv ON device.id=ts_kv.entity_id" + SQLWhere + " ORDER BY ts_kv.ts DESC"
	if limit > 0 && offset >= 0 {
		SQL = fmt.Sprintf("%s limit ? offset ? ", SQL)
		params = append(params, limit, offset)
	}
	if err := result.Raw(SQL, params...).Scan(&tSKVs).Error; err != nil {
		return tsk, 0
	}

	countsql := "SELECT Count(*) AS count FROM business LEFT JOIN asset ON business.id=asset.business_id LEFT JOIN device ON asset.id=device.asset_id LEFT JOIN ts_kv ON device.id=ts_kv.entity_id " + SQLWhere
	if err := result2.Raw(countsql, params...).Scan(&count).Error; err != nil {
		return tsk, 0
	}
	for _, v := range tSKVs {
		ts := models.TSKVDblV{
			EntityType: v.EntityType,
			EntityID:   v.EntityID,
			Key:        v.Key,
			TS:         v.TS,
			BoolV:      v.BoolV,
			StrV:       v.StrV,
			LongV:      v.LongV,
			Token:      v.Token,
			Bname:      v.Bname,
			Name:       v.Name,
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
func (*TSKVService) GetCurrentData(device_id string) []map[string]interface{} {
	var ts_kvs []models.TSKVLatest
	device := make(map[string]interface{})
	result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", device_id).Order("ts asc").Find(&ts_kvs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	var fields []map[string]interface{}
	if len(ts_kvs) > 0 {
		var i int64 = 0
		var field map[string]interface{}
		// 0-带接入 1-正常 2-异常
		var state string
		var TSKVService TSKVService
		tsl, tsc := TSKVService.Status(device_id)
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
				field["status"] = state
				if fmt.Sprint(v.BoolV) != "" {
					field[field_from] = v.BoolV
				} else if v.StrV != "" {
					field[field_from] = v.StrV
				} else if v.LongV != 0 {
					field[field_from] = v.LongV
				} else if v.DblV != 0 {
					field[field_from] = v.DblV
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
				}
				if c == k+1 {
					fields = append(fields, field)
				}
			}
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
			deviceData["extension"] = device.Extension
			deviceData["label"] = device.Label
			deviceData["name"] = device.Name
			deviceData["protocol"] = device.Protocol
			deviceData["publish"] = device.Publish
			deviceData["subscribe"] = device.Subscribe
			deviceData["type"] = device.Type
			deviceData["d_id"] = device.DId
			deviceData["location"] = device.Location
			var TSKVService TSKVService
			fields := TSKVService.GetCurrentData(device.ID)
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
				}
				if c == k+1 {
					fields = append(fields, field)
				}
			}
		}
	}
	return fields, count
}
