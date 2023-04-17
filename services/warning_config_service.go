package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/initialize/redis"
	sendmessage "ThingsPanel-Go/initialize/send_message"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	simplejson "github.com/bitly/go-simplejson"
	"gorm.io/gorm"
)

type WarningConfigService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// GetWarningConfigById 根据id获取一条warningConfig数据
func (*WarningConfigService) GetWarningConfigById(id string) (*models.WarningConfig, int64) {
	var warningConfig models.WarningConfig
	result := psql.Mydb.Where("id = ?", id).First(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &warningConfig, result.RowsAffected
}

// Paginate 分页获取warningConfig数据
func (*WarningConfigService) Paginate(wid string, offset int, pageSize int) ([]models.WarningConfig, int64) {
	var warningConfigs []models.WarningConfig
	var count int64
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("wid = ?", wid).Limit(pageSize).Offset(pageSize * (offset - 1)).Find(&warningConfigs)
	psql.Mydb.Model(&models.WarningConfig{}).Where("wid = ?", wid).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(warningConfigs) == 0 {
		warningConfigs = []models.WarningConfig{}
	}
	return warningConfigs, count
}

// Add新增一条warningConfig数据
func (*WarningConfigService) Add(wid string, name string, describe string, config string, message string, bid string, sensor string, customer_id string, other_message string) (bool, string) {
	var uuid = uuid.GetUuid()
	warningConfig := models.WarningConfig{
		ID:           uuid,
		Wid:          wid,
		Name:         name,
		Describe:     describe,
		Config:       config,
		Message:      message,
		Bid:          bid,
		Sensor:       sensor,
		CustomerID:   customer_id,
		OtherMessage: other_message,
	}
	result := psql.Mydb.Create(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false, ""
	}
	// 告警缓存-删除标记
	redis.DelKey("warning" + bid)
	return true, uuid
}

// 根据ID编辑一条warningConfig数据
func (*WarningConfigService) Edit(id string, wid string, name string, describe string, config string, message string, bid string, sensor string, customer_id string, other_message string) bool {
	// updated_at
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("id = ?", id).Updates(map[string]interface{}{"wid": wid, "name": name, "describe": describe, "config": config, "message": message, "bid": bid, "sensor": sensor, "customer_id": customer_id, "other_message": other_message})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	// 告警缓存-删除标记
	redis.DelKey("warning" + bid)
	return true
}

// 根据ID删除一条warningConfig数据
func (*WarningConfigService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.WarningConfig{})
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
		return false
	}
	return true
}

// GetWarningConfigById 根据id获取一条warningConfig数据
func (*WarningConfigService) GetWarningConfigByWidAndBid(wid string, bid string) (*models.WarningConfig, int64) {
	var warningConfig models.WarningConfig
	result := psql.Mydb.Where("wid = ? AND bid = ?", wid, bid).First(&warningConfig)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &warningConfig, result.RowsAffected
}

// GetWarningConfigsByDeviceId 根据id获取多条warningConfig数据
func (*WarningConfigService) WarningConfigCheck(bid string, values map[string]interface{}) {
	//bid为设备id
	var warningConfigs []models.WarningConfig
	var count int64
	//告警策略配置
	result := psql.Mydb.Model(&models.WarningConfig{}).Where("bid = ?", bid).Find(&warningConfigs)
	//告警策略数量
	psql.Mydb.Model(&models.WarningConfig{}).Where("bid = ?", bid).Count(&count)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if count > int64(0) {
		logs.Info("device id %s have warning config", bid)
		original := ""
		code := ""
		c := make(map[string]string)
		m := make(map[string]string)
		var FieldMappingService FieldMappingService
		var BusinessService BusinessService
		var AssetService AssetService
		var WarningLogService WarningLogService
		var DeviceService DeviceService
		for _, wv := range warningConfigs {
			code = ""
			res, err := simplejson.NewJson([]byte(wv.Config))
			if err != nil {
				fmt.Println("解析出错", err)
			}
			//[{"field":"pm25","condition":">","value":"30"}]
			rows, _ := res.Array()
			for _, row := range rows {
				if each_map, ok := row.(map[string]interface{}); ok {
					logs.Info(each_map)
					if each_map["operator"] != nil {
						code += fmt.Sprint(each_map["operator"])
					}
					if each_map["field"] != nil {
						tmp := fmt.Sprint(each_map["field"])
						code += "${" + tmp + "}"
					}
					if each_map["condition"] != nil {
						code += fmt.Sprint(each_map["condition"])
					}
					if each_map["value"] != nil {
						code += fmt.Sprint(each_map["value"])
						c["${"+fmt.Sprint(each_map["field"])+"}"] = fmt.Sprint(each_map["value"])
					}
				}
			}
			original = code
			logs.Info(original)
			// 替换变量
			var flag string = "false"
			for k, v := range values {
				//field := FieldMappingService.GetFieldTo(bid, k)
				m["${"+k+"}"] = fmt.Sprint(v)
				code = strings.Replace(code, "${"+k+"}", fmt.Sprint(v), -1)
			}
			logs.Info("表达式：%s", utils.ReplaceUserInput(code))
			if ok := strings.Contains(code, "${"); !ok {
				flag = utils.Eval(code)
			} else {
				logs.Info("表达式中存在未替换的字段，跳过本次循环")
				break
			}
			if flag == "true" {
				message := ""
				businessName := ""
				assetName := ""
				deviceName := ""
				bl, bc, _ := BusinessService.GetBusinessById(wv.Wid)
				if bc > 0 {
					businessName = bl.Name
				}
				al, ac := AssetService.GetAssetById(wv.Sensor)
				if ac > 0 {
					assetName = al.Name
				}
				dl, dc := DeviceService.GetDeviceByID(wv.Sensor)
				if dc > 0 {
					deviceName = dl.Name
				}
				message = businessName + "业务中设备分组" + assetName + "下的设备" + deviceName
				if find := strings.Contains(original, "||"); find {
					countSplit := strings.Split(original, "||")
					for _, v1 := range countSplit {
						if find2 := strings.Contains(v1, "&&"); find2 {
							countSplit2 := strings.Split(v1, "&&")
							for _, v2 := range countSplit2 {
								fieldSplit := strings.Split(v2, "}")
								tmp := fieldSplit[0]
								filed_param := tmp[2:]
								code_param := strings.Replace(v2, "${"+filed_param+"}", m["${"+filed_param+"}"], -1)
								flag_param := utils.Eval(code_param)
								if flag_param == "true" {
									//指标co当前值为xx,预设值为xx
									symbol := FieldMappingService.GetSymbol(bid, filed_param)
									message += "指标" + filed_param + "当前值为" + m["${"+filed_param+"}"] + symbol + ",预设值为" + c["${"+filed_param+"}"] + symbol + ";"
								}
							}
						} else {
							fieldSplit := strings.Split(v1, "}")
							tmp := fieldSplit[0]
							filed_param := tmp[2:]
							code_param := strings.Replace(v1, "${"+filed_param+"}", m["${"+filed_param+"}"], -1)
							flag_param := utils.Eval(code_param)
							if flag_param == "true" {
								symbol := FieldMappingService.GetSymbol(bid, filed_param)
								message += "指标" + filed_param + "当前值为" + m["${"+filed_param+"}"] + symbol + ",预设值为" + c["${"+filed_param+"}"] + symbol + ";"
							}
						}
					}
				} else if find := strings.Contains(original, "&&"); find {
					countSplit := strings.Split(original, "&&")
					for _, v1 := range countSplit {
						if find2 := strings.Contains(v1, "||"); find2 {
							countSplit2 := strings.Split(v1, "||")
							for _, v2 := range countSplit2 {
								fieldSplit := strings.Split(v2, "}")
								tmp := fieldSplit[0]
								filed_param := tmp[2:]
								code_param := strings.Replace(v2, "${"+filed_param+"}", m["${"+filed_param+"}"], -1)
								flag_param := utils.Eval(code_param)
								if flag_param == "true" {
									symbol := FieldMappingService.GetSymbol(bid, filed_param)
									message += "指标" + filed_param + "当前值为" + m["${"+filed_param+"}"] + symbol + ",预设值为" + c["${"+filed_param+"}"] + symbol + ";"
								}
							}
						} else {
							fieldSplit := strings.Split(v1, "}")
							tmp := fieldSplit[0]
							filed_param := tmp[2:]
							code_param := strings.Replace(v1, "${"+filed_param+"}", m["${"+filed_param+"}"], -1)
							flag_param := utils.Eval(code_param)
							if flag_param == "true" {
								symbol := FieldMappingService.GetSymbol(bid, filed_param)
								message += "指标" + filed_param + "当前值为" + m["${"+filed_param+"}"] + symbol + ",预设值为" + c["${"+filed_param+"}"] + symbol + ";"
							}
						}
					}
				} else {
					fieldSplit := strings.Split(original, "}")
					tmp := fieldSplit[0]
					filed_param := tmp[2:]
					code_param := strings.Replace(original, "${"+filed_param+"}", m["${"+filed_param+"}"], -1)
					flag_param := utils.Eval(code_param)
					if flag_param == "true" {
						//指标co当前值为xx,预设值为xx
						symbol := FieldMappingService.GetSymbol(bid, filed_param)
						message += "指标" + filed_param + "当前值为" + m["${"+filed_param+"}"] + symbol + ",预设值为" + c["${"+filed_param+"}"] + symbol + ";"
					}
				}
				WarningLogService.Add("1", message, bid)
				sendmessage.SendWarningMessage(message, wv.OtherMessage)
			}
		}
	} else {
		// 告警缓存-没有策略就进行标记
		redis.SetStr("warning"+bid, "1", 3600*time.Second)
	}

}
