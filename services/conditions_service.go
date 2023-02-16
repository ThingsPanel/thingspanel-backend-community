package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
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

//自动化策略检查
func (*ConditionsService) AutomationConditionCheck(deviceId string, values map[string]interface{}) {
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("condition_type = '1' and device_condition_type = '1' device_id = ? ", deviceId).Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return
	}
	// 每一条自动化策略；
	var passedAutomationList []string
	for _, automationCondition := range automationConditions {
		if utils.In(automationCondition.AutomationId, passedAutomationList) {
			// 一次上报中，已触发的自动化不能再触发
			continue
		}
		//获取且小组的数据
		var conditionGroups []models.TpAutomationCondition
		result := psql.Mydb.Model(&models.TpAutomationCondition{}).Where("automation_id = ? and group_number = ?", automationCondition.AutomationId, automationCondition.GroupNumber).Find(&conditionGroups)
		if result.Error != nil {
			logs.Error(result.Error.Error())
			continue
		}
		isPass := false
		isThisDevice := false
		// 判断每个条件是否通过
		for _, conditionData := range conditionGroups {
			// 设备条件
			if conditionData.ConditionType == "1" {
				// 设备属性
				if conditionData.DeviceConditionType == "1" {
					if conditionData.DeviceId == deviceId {
						//是本次推送设备的属性
						if value, ok := values[conditionData.V1].(string); ok {
							isThisDevice = true
							isPass = utils.Check(value, conditionData.V2, conditionData.V3)
						} else {
							var tskvLatest models.TSKVLatest
							//如果不是本次设备推送的数据，需要查询设备当前值
							result := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = ?", deviceId, conditionData.V1).First(&tskvLatest)
							if result.Error != nil {
								if errors.Is(result.Error, gorm.ErrRecordNotFound) {
									isPass = false
									break
								}
								logs.Error(result.Error.Error())
								isPass = false
								break
							}
							if tskvLatest.StrV != "" {
								isPass = utils.Check(tskvLatest.StrV, conditionData.V2, conditionData.V3)
							} else {
								isPass = utils.Check(tskvLatest.DblV, conditionData.V2, conditionData.V3)
							}
						}
					} else {
						//其他设备属性
						var tskvLatest models.TSKVLatest
						//查询设备当前值
						result := psql.Mydb.Model(&models.TSKVLatest{}).Where("entity_id = ? and key = ?", conditionData.DeviceId, conditionData.V1).First(&tskvLatest)
						if result.Error != nil {
							if errors.Is(result.Error, gorm.ErrRecordNotFound) {
								isPass = false
								break
							}
							logs.Error(result.Error.Error())
							isPass = false
							break
						}
						if tskvLatest.StrV != "" {
							isPass = utils.Check(tskvLatest.StrV, conditionData.V2, conditionData.V3)
						} else {
							isPass = utils.Check(tskvLatest.DblV, conditionData.V2, conditionData.V3)
						}
					}
				} else {
					//不是设备属性的都不通过
					isPass = false
					break
				}
			} else if conditionData.ConditionType == "2" {
				if conditionData.TimeConditionType == "0" {
					//时间范围
					isPass = utils.CheckTime(conditionData.V1, conditionData.V2)
				} else {
					//非时间范围不通过
					isPass = false
					break
				}
			}
		}
		if !isThisDevice {
			//非本次推送的属性
			continue
		}
		if isPass {
			passedAutomationList = append(passedAutomationList, automationCondition.AutomationId)
			logs.Info("成功触发自动化")
			var conditionsService ConditionsService
			message, err := conditionsService.ExecuteAutomationAction(automationCondition.AutomationId)
			if err != nil {
				//执行失败，记录日志
				logs.Error(err.Error())
			} else {
				//执行成功，记录日志
				logs.Info(message)
			}
		} else {
			logs.Info("未触发自动化")
		}

	}
}

// 执行自动化动作
func (*ConditionsService) ExecuteAutomationAction(AutomationId string) (string, error) {
	var automationActions []models.TpAutomationAction
	result := psql.Mydb.Model(&models.TpAutomationAction{}).Where("automation_id = ?", AutomationId).Find(&automationActions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return "", result.Error
	}
	for _, automationAction := range automationActions {
		if automationAction.ActionType == "1" {
			//设备输出
			res, err := simplejson.NewJson([]byte(automationAction.AdditionalInfo))
			if err != nil {
				return "", err
			}
			deviceModel := res.Get("device_model").MustString()
			if deviceModel == "1" {
				//属性
				instructString := res.Get("instruct").MustString()
				instructMap := make(map[string]interface{})
				err = json.Unmarshal([]byte(instructString), &instructMap)
				if err != nil {
					return "", err
				}
				for k, v := range instructMap {
					var DeviceService DeviceService
					err := DeviceService.OperatingDevice(automationAction.DeviceId, k, v)
					if err == nil {
						return "成功发送控制，指令为:" + instructString, nil
					} else {
						return "", nil
					}
				}
			} else if deviceModel == "2" {
				return "暂不支持调动服务", nil
			} else {
				return "deviceModel错误", nil
			}

		} else if automationAction.ActionType == "2" {
			//触发告警-？？？
			return "触发告警-此处未开发完", nil
		} else if automationAction.ActionType == "3" {
			//触发场景-？？？
			return "触发场景-此处未开发完", nil
		}
	}
	return "", nil
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
				DeviceService.ApplyControl(res, "", "3")
			}
		}
	}
}

// 手动触发控制指令集
func (*ConditionsService) ManualTrigger(conditions_id string) error {
	var conditionConfig models.Condition
	result := psql.Mydb.First(&conditionConfig, "id = ?", conditions_id)
	if result.Error != nil {
		return result.Error
	}
	res, err := simplejson.NewJson([]byte(conditionConfig.Config))
	if err != nil {
		logs.Error(err.Error())
	}
	var DeviceService DeviceService
	DeviceService.ApplyControl(res, "", "2")
	return nil
}

// 根据业务id获取策略下拉
func (*ConditionsService) ConditionsPullDownList(params valid.ConditionsPullDownListValidate) ([]map[string]interface{}, error) {
	sqlWhere := "business_id = '" + params.BusinessId + "'"
	if params.Status != "" {
		sqlWhere += " and status = '" + params.Status + "'"
	}
	if params.ConditionsType != "" {
		sqlWhere += " and type = '" + params.ConditionsType + "'"
	}
	if params.Issued != "" {
		sqlWhere += " and issued = '" + params.Issued + "'"
	}
	var conditionConfig []map[string]interface{}
	result := psql.Mydb.Model(&models.Condition{}).Select("id,name as policy_name,describe").Where(sqlWhere).Order("sort ASC").Find(&conditionConfig)
	if result.Error != nil {
		return nil, result.Error
	}
	return conditionConfig, nil
}
