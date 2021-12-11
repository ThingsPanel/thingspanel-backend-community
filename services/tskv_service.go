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
	Token  string        `json:"token"`
	Values []interface{} `json:"values"`
	Ts     int64         `json:"ts"`
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
	fmt.Println("bg")
	var device models.Device
	var tskv models.TSKV
	result := psql.Mydb.Where("token = ?", payload.Token).First(&device)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if result.RowsAffected > 0 {
		for k, v := range payload.Values {
			vint, ok := v.(float64)
			if ok {
				ts := payload.Ts
				if ts == 0 {
					ts = time.Now().UnixMicro()
				}
				rt := psql.Mydb.Where("entity_type = ? AND entity_id = ? AND key = ?", "DEVICE", device.ID, strconv.Itoa(k)).First(&tskv)
				if rt.Error != nil {
					errors.Is(rt.Error, gorm.ErrRecordNotFound)
				}
				fmt.Println(rt.RowsAffected)
				if rt.RowsAffected > 0 {
					// 更新
					rts := psql.Mydb.Model(&models.TSKV{}).Where("entity_type = ? AND entity_id = ? AND key = ?", "DEVICE", device.ID, string(k)).Updates(map[string]interface{}{
						"ts":    ts,
						"dbl_v": vint,
					})
					if rts.Error != nil {
						log.Println("Msg Consumer: Cannot insert into ts_kv")
						return false
					}
				} else {
					d := models.TSKV{
						EntityType: "DEVICE",
						EntityID:   device.ID,
						Key:        strconv.Itoa(k),
						TS:         ts,
						DoubleV:    vint,
					}
					rts := psql.Mydb.Create(&d)
					if rts.Error != nil {
						log.Println("Msg Consumer: Cannot insert into ts_kv")
						return false
					}
				}
			}
		}
		return true
	}
	fmt.Println("token not matched")
	return false
}

func (*TSKVService) Paginate(entity_id string, t int64, start_time string, end_time string, offset int, pageSize int) ([]models.TSKV, int64) {
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
