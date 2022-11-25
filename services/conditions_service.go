package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/logs"
	simplejson "github.com/bitly/go-simplejson"
	"gorm.io/gorm"
)

type ConditionsService struct {
}

// 获取全部策略
func (*ConditionsService) All() ([]models.Condition, int64) {
	var conditions []models.Condition
	result := psql.Mydb.Find(&conditions)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if len(conditions) == 0 {
		conditions = []models.Condition{}
	}
	return conditions, result.RowsAffected
}

// 获取策略
func (*ConditionsService) GetConditionByID(id string) (*models.Condition, int64) {
	var condition models.Condition
	result := psql.Mydb.Where("id = ?", id).First(&condition)
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	return &condition, result.RowsAffected
}

// GetWarningConfigsByDeviceId 根据id获取多条warningConfig数据
func (*ConditionsService) ConditionsConfigCheck(deviceId string, values map[string]interface{}) {
	logs.Info("自动化控制检查")
	//deviceId为设备id
	var conditionConfigs []models.Condition
	var count int64
	//自动化策略配置
	//logs.Info("设备id-%s-设备条件类型->查询自动化配置", deviceId)
	result := psql.Mydb.Model(&models.Condition{}).Where("type = 1 and status = '1' and (config ::json->>'rules' like '%" + deviceId + "%') order by sort asc").Find(&conditionConfigs)
	//自动化策略数量
	count = result.RowsAffected
	if result.Error != nil {
		errors.Is(result.Error, gorm.ErrRecordNotFound)
	}
	if count > 0 {
		logs.Info("设备id-%s 存在自动化策略配置,条数-%d", deviceId, count)
		//original := ""
		code := ""
		c := make(map[string]string)
		m := make(map[string]string)
		// var FieldMappingService FieldMappingService
		for _, row := range conditionConfigs {
			code = ""
			res, err := simplejson.NewJson([]byte(row.Config))
			if err != nil {
				fmt.Println("解析出错", err)
			}
			//{"rules":[
			//{"asset_id":"xxx","field":"temp","device_id":"xxx","condition":"<","value":"20","duration":0},
			//{"asset_id":"xxx","field":"temp","device_id":"xxx","condition":">","value":"10","operator":"||",
			//"duration":0}],
			//"apply":[{"asset_id":"xxx","field":"hum","device_id":"xxx","value":"1"}]}
			rulesRows, _ := res.Get("rules").Array()
			for _, rulesRow := range rulesRows {
				if rulesMap, ok := rulesRow.(map[string]interface{}); ok {
					logs.Info(rulesMap)
					// 如果有“或者，并且”操作符，就给code加上操作符
					if rulesMap["operator"] != nil {
						code += fmt.Sprint(rulesMap["operator"])
					}
					// 如果有“字段”，就给code加上字段
					if rulesMap["field"] != nil {
						tmp := fmt.Sprint(rulesMap["field"])
						code += "${" + tmp + "}"
					}
					// 如果有“条件”，就给code加上条件
					if rulesMap["condition"] != nil {
						code += fmt.Sprint(rulesMap["condition"])
					}
					// 如果有“值”，就给code加上值
					if rulesMap["value"] != nil {
						code += fmt.Sprint(rulesMap["value"])
						c[fmt.Sprint(rulesMap["field"])] = fmt.Sprint(rulesMap["value"])
					}
				}
			}
			// original = code
			// logs.Info("原表达式-%s", original)
			// 通过设备id和设备端字段查询出映射字段，再替换变量
			// var flag string = "false"
			for k, v := range values {
				//field := FieldMappingService.GetFieldTo(deviceId, k)
				m["${"+k+"}"] = fmt.Sprint(v)
				code = strings.Replace(code, "${"+k+"}", fmt.Sprint(v), -1)

			}
			//判断表达式中的字段是否已经完整替换
			var flag string = "false"
			logs.Info("表达式-%s", code)
			if ok := strings.Contains(code, "${"); !ok {
				flag = utils.Eval(code)
			} else {
				logs.Info("表达式中存在未替换的字段，跳过本次循环")
				break
			}
			if flag == "true" {
				logs.Info("控制已触发，开始执行控制策略")
				//触发控制
				//"apply":[{"asset_id":"xxx","field":"hum","device_id":"xxx","value":"1"}]}
				var DeviceService DeviceService
				DeviceService.ApplyControl(res, "")
			}
		}
	}
}
