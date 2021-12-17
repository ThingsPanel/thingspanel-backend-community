package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
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
	result := psql.Mydb.Find(&tskvs)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(tskvs) == 0 {
		tskvs = []models.TSKV{}
	}
	return tskvs, result.RowsAffected
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
	result := psql.Mydb.Where("token = ?", payload.Token).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if result.RowsAffected > 0 {
		// 查询警告
		var WarningConfigService WarningConfigService
		WarningConfigService.WarningConfigCheck(device.ID, payload.Values)
		ts := time.Now().UnixMicro()
		for k, v := range payload.Values {
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

			rts := psql.Mydb.Create(&d)
			if rts.Error != nil {
				log.Println(rts.Error)
				return false
			}
		}
		return true
	}
	fmt.Println("token not matched")
	return false
}

func (*TSKVService) Paginate(business_id string, t int64, start_time string, end_time string, offset int, pageSize int) ([]models.TSKV, int64) {
	var tSKVs []models.TSKV
	var count int64
	result := psql.Mydb.Model(&models.TSKV{})
	result2 := psql.Mydb.Model(&models.TSKV{})
	if business_id != "" {
		var AssetService AssetService
		var DeviceService DeviceService
		var asset_ids []string
		var device_ids []string
		bl, bc := AssetService.GetAssetDataByBusinessId(business_id)
		if bc > 0 {
			for _, v := range bl {
				asset_ids = append(asset_ids, v.ID)
			}
		}

		if len(asset_ids) > 0 {
			dl, dc := DeviceService.GetDevicesByAssetIDs(asset_ids)
			if dc > 0 {
				for _, v := range dl {
					device_ids = append(device_ids, v.ID)
				}
			}
		}
		fmt.Println(device_ids)
		if len(device_ids) > 0 {
			result = result.Where("entity_id IN ?", device_ids)
			result2 = result2.Where("entity_id IN ?", device_ids)
		}
	}
	if t == 1 {
		today_start, today_end := timeHelper.Today()
		result = result.Where("ts between ? AND ?", today_start*1000000, today_end*1000000)
		result2 = result2.Where("ts between ? AND ?", today_start*1000000, today_end*1000000)
	} else if t == 2 {
		week_start, week_end := timeHelper.Week()
		result = result.Where("ts between ? AND ?", week_start*1000000, week_end*1000000)
		result2 = result2.Where("ts between ? AND ?", week_start*1000000, week_end*1000000)
	} else if t == 3 {
		month_start, month_end := timeHelper.Month()
		result = result.Where("ts between ? AND ?", month_start*1000000, month_end*1000000)
		result2 = result2.Where("ts between ? AND ?", month_start*1000000, month_end*1000000)
	} else if t == 4 {
		timeTemplate := "2006-01-02 15:04:05"
		start_date, _ := time.ParseInLocation(timeTemplate, start_time, time.Local)
		end_date, _ := time.ParseInLocation(timeTemplate, end_time, time.Local)
		start := start_date.Unix()
		end := end_date.Unix()
		result = result.Where("ts between ? AND ?", start*1000000, end*1000000)
		result2 = result2.Where("ts between ? AND ?", start*1000000, end*1000000)
	}
	result = result.Order("ts desc").Limit(offset).Offset(pageSize).Find(&tSKVs)
	result2.Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(tSKVs) == 0 {
		tSKVs = []models.TSKV{}
	}
	return tSKVs, count
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

func (*TSKVService) GetTelemetry(device_ids []string, startTs int64, endTs int64) []interface{} {
	var ts_kvs []models.TSKV
	var devices []interface{}
	var FieldMappingService FieldMappingService
	if len(device_ids) > 0 {
		for _, d := range device_ids {
			device := make(map[string]interface{})
			if startTs == 0 && endTs == 0 {
				result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("entity_id = ?", d).Order("ts asc").Find(&ts_kvs)
				if result.Error != nil {
					errors.Is(result.Error, gorm.ErrRecordNotFound)
				}
				var fields []map[string]interface{}
				if result.RowsAffected > 0 {
					var i int64 = 0
					var field map[string]interface{}
					field_from := ""
					c := result.RowsAffected
					for k, v := range ts_kvs {
						if field_from != v.Key {
							field_from = FieldMappingService.TransformByDeviceid(d, v.Key)
							if field_from == "" {
								field_from = v.Key
							}
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
							if c == int64(k+1) {
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
			} else {
				result := psql.Mydb.Select("key, bool_v, str_v, long_v, dbl_v, ts").Where("ts >= ? AND ts <= ? AND entity_id = ?", startTs*1000, endTs*1000, d).Order("ts asc").Find(&ts_kvs)
				if result.Error != nil {
					errors.Is(result.Error, gorm.ErrRecordNotFound)
				}
				var fields []map[string]interface{}
				if result.RowsAffected > 0 {
					var i int64 = 0
					var field map[string]interface{}
					field_from := ""
					c := result.RowsAffected
					for k, v := range ts_kvs {
						if field_from != v.Key {
							field_from = FieldMappingService.TransformByDeviceid(d, v.Key)
							if field_from == "" {
								field_from = v.Key
							}
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
							if c == int64(k+1) {
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
		}
	} else {
		fmt.Println("device_ids不能为空")
	}
	if len(devices) == 0 {
		devices = make([]interface{}, 0)
	}
	return devices
}

func (*TSKVService) Status(device_id string) (*models.TSKV, int64) {
	var tskv models.TSKV
	result := psql.Mydb.Where("entity_id = ?", device_id).Order("ts desc").First(&tskv)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &tskv, result.RowsAffected
}
