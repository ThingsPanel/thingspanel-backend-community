package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/logs"
	simplejson "github.com/bitly/go-simplejson"
	"github.com/spf13/cast"
	"gorm.io/gorm"
)

type ConditionsService struct {
}

// 获取全部策略
func (*ConditionsService) All() ([]models.Condition, int64) {
	var conditions []models.Condition
	result := psql.Mydb.Find(&conditions)
	if len(conditions) == 0 {
		conditions = []models.Condition{}
	}
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return conditions, 0
		}
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return conditions, result.RowsAffected
}

// 获取策略
// func (*ConditionsService) GetConditionByID(id string) (*models.Condition, int64) {
// 	var condition models.Condition
// 	result := psql.Mydb.Where("id = ?", id).First(&condition)
// 	if result.Error != nil {
// 		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
// 			return &condition, 0
// 		}
// 		logs.Error(result.Error.Error())
// 		return nil, 0
// 	}
// 	return &condition, result.RowsAffected
// }

// 上下线触发检查1-上线 2-下线
func (*ConditionsService) OnlineAndOfflineCheck(deviceId string, flag string) error {
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Table("tp_automation").
		Select("tp_automation_condition.*").
		Joins("left join tp_automation_condition on tp_automation.id = tp_automation_condition.automation_id").
		Where("tp_automation.enabled = '1' and tp_automation_condition.condition_type = '1' and tp_automation_condition.device_condition_type = '3' and tp_automation_condition.device_id = ? and tp_automation_condition.v2 in ( ? ,'3')", deviceId, flag).
		Order("tp_automation.priority asc").
		Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return nil
	}
	var logMessage string
	if flag == "1" {
		logMessage = "设备上线；"
	} else if flag == "2" {
		logMessage = "设备下线；"
	}
	logs.Info("自动化-设备条件：", automationConditions)
	for _, automationCondition := range automationConditions {
		var conditionsService ConditionsService
		err := conditionsService.WriteLogAndExecuteActionFunc(automationCondition.AutomationId, logMessage)
		if err != nil {
			logs.Error(err.Error())
		}

	}
	return nil
}

// 自动化策略检查
func (*ConditionsService) AutomationConditionCheck(deviceId string, values map[string]interface{}) {
	var automationConditions []models.TpAutomationCondition
	result := psql.Mydb.Table("tp_automation").
		Select("tp_automation_condition.*").
		Joins("left join tp_automation_condition on tp_automation.id = tp_automation_condition.automation_id").
		Where("tp_automation.enabled = '1' and tp_automation_condition.condition_type = '1' and tp_automation_condition.device_condition_type = '1' and tp_automation_condition.device_id = ? ", deviceId).
		Order("tp_automation.priority asc").
		Find(&automationConditions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return
	}
	logs.Info("自动化-设备条件：", automationConditions)
	//logs.Info("自动化-map:", values)
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
		logMessage := ""
		// 判断每个条件是否通过
		for _, conditionData := range conditionGroups {
			// 设备条件，并且v4是属性1-属性
			//if conditionData.ConditionType == "1" && conditionData.V4 == "1" {
			if conditionData.ConditionType == "1" {
				// 设备属性
				if conditionData.DeviceConditionType == "1" {
					//是否本次设备的属性
					if conditionData.DeviceId == deviceId {
						// 本次上报属性的map中有没有当前判断的属性
						if value, ok := values[conditionData.V1]; ok {
							isThisDevice = true
							isSuccess, _ := utils.Check(value, conditionData.V2, conditionData.V3)
							logs.Error("check:", isSuccess)
							isPass = isSuccess
							if isPass {
								logMessage += "设备上报的属性" + conditionData.V1 + ":" + cast.ToString(values[conditionData.V1]) + conditionData.V2 + cast.ToString(conditionData.V3) + "通过；"
							}

						} else { //如果不是本次设备推送的数据，需要查询设备当前值
							var tskvLatest models.TSKVLatest

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
							// 是否是字符串
							if tskvLatest.StrV != "" {
								isSuccess, _ := utils.Check(tskvLatest.StrV, conditionData.V2, conditionData.V3)
								isPass = isSuccess
								if isPass {
									logMessage += "设备的属性(非本次上报)" + conditionData.V1 + ":" + cast.ToString(tskvLatest.StrV) + conditionData.V2 + cast.ToString(conditionData.V3) + "通过；"
								}
							} else {
								//是float64
								isSuccess, _ := utils.Check(tskvLatest.DblV, conditionData.V2, conditionData.V3)
								isPass = isSuccess
								if isPass {
									logMessage += "设备的属性(非本次上报)" + conditionData.V1 + ":" + cast.ToString(tskvLatest.DblV) + conditionData.V2 + cast.ToString(conditionData.V3) + "通过；"
								}
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
							b, _ := utils.Check(tskvLatest.StrV, conditionData.V2, conditionData.V3)
							isPass = b
							if isPass {
								logMessage += "其他设备的属性" + conditionData.V1 + ":" + cast.ToString(tskvLatest.StrV) + conditionData.V2 + cast.ToString(conditionData.V3) + "通过；"
							}
						} else {
							b, _ := utils.Check(tskvLatest.DblV, conditionData.V2, conditionData.V3)
							isPass = b
							if isPass {
								logMessage += "其他设备的属性" + conditionData.V1 + ":" + cast.ToString(tskvLatest.DblV) + conditionData.V2 + cast.ToString(conditionData.V3) + "通过；"
							}
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
					isSuccess, _ := utils.CheckTime(conditionData.V1, conditionData.V2)
					isPass = isSuccess
					logMessage += "当前时间：" + time.Now().Format("2006/01/02 15:04:05") + "，在" + conditionData.V1 + "和" + conditionData.V2 + "内；"
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
		logs.Error("自动化条件是否通过？", isPass)
		if isPass {
			passedAutomationList = append(passedAutomationList, automationCondition.AutomationId)
			var conditionsService ConditionsService
			err := conditionsService.WriteLogAndExecuteActionFunc(automationCondition.AutomationId, logMessage)
			if err != nil {
				logs.Error(err.Error())
			}
		} else {
			logs.Info("未触发自动化")
		}

	}
}

// 记录日志，调用执行action的函数
func (*ConditionsService) WriteLogAndExecuteActionFunc(automationId string, logMessage string) error {
	logMessage += "条件通过；"
	logs.Info("成功触发自动化")
	//登记日志
	var automationLogMap = make(map[string]interface{})
	var sutomationLogService TpAutomationLogService
	var automationLog models.TpAutomationLog
	automationLog.AutomationId = automationId
	automationLog.ProcessDescription = logMessage
	automationLog.TriggerTime = time.Now().Format("2006/01/02 15:04:05")
	automationLog.ProcessResult = "2"
	automationLog, err := sutomationLogService.AddTpAutomationLog(automationLog)
	if err != nil {
		logs.Error(err.Error())
	} else {
		automationLogMap["Id"] = automationLog.Id
		var conditionsService ConditionsService
		msg, err := conditionsService.ExecuteAutomationAction(automationId, automationLog.Id)
		if err != nil {
			//执行失败，记录日志
			logs.Error(err.Error())
			automationLogMap["ProcessDescription"] = logMessage + "|" + err.Error()

		} else {
			//执行成功，记录日志
			logs.Info(logMessage)
			automationLogMap["ProcessDescription"] = logMessage + "|" + msg
			automationLogMap["ProcessResult"] = "1"

		}
		err = sutomationLogService.UpdateTpAutomationLog(automationLogMap)
		if err != nil {
			logs.Error(err.Error())
		}
	}
	return nil
}

// 执行自动化动作
func (*ConditionsService) ExecuteAutomationAction(automationId string, automationLogId string) (string, error) {
	var automationActions []models.TpAutomationAction
	var logMessage string
	result := psql.Mydb.Model(&models.TpAutomationAction{}).Where("automation_id = ?", automationId).Find(&automationActions)
	if result.Error != nil {
		logs.Error(result.Error.Error())
		return "", result.Error
	}
	for _, automationAction := range automationActions {
		var automationLogDetail models.TpAutomationLogDetail
		if automationAction.ActionType == "1" {
			automationLogDetail.ActionType = "1"
			automationLogDetail.TargetId = automationAction.DeviceId
			//设备输出
			res, err := simplejson.NewJson([]byte(automationAction.AdditionalInfo))
			if err != nil {
				logs.Error(err.Error())
				automationLogDetail.ProcessDescription = "additional_info:" + err.Error()
				automationLogDetail.ProcessResult = "2"
			} else {
				deviceModel := res.Get("device_model").MustString()
				if deviceModel == "1" {
					//属性
					// instructString := res.Get("instruct").MustString()
					// instructMap := make(map[string]interface{})
					// err = json.Unmarshal([]byte(instructString), &instructMap)
					// if err != nil {
					// 	logs.Error(err.Error())
					// 	automationLogDetail.ProcessDescription = "instruct:" + err.Error()
					// 	automationLogDetail.ProcessResult = "2"
					// } else {
					instructMap := res.Get("instruct").MustMap()
					instructByte, err := json.Marshal(instructMap)
					instructString := string(instructByte)
					if err != nil {
						logs.Error(err.Error())
						automationLogDetail.ProcessDescription = "instruct:" + err.Error()
						automationLogDetail.ProcessResult = "2"
					} else {
						for k, v := range instructMap {
							var DeviceService DeviceService
							var conditionsLog models.ConditionsLog
							err := DeviceService.OperatingDevice(automationAction.DeviceId, k, v)
							if err == nil {
								conditionsLog.SendResult = "1"
								automationLogDetail.ProcessResult = "1"
								automationLogDetail.ProcessDescription = "指令为:" + instructString
							} else {
								logs.Error(err.Error())
								conditionsLog.SendResult = "2"
								automationLogDetail.ProcessResult = "2"
								automationLogDetail.ProcessDescription = err.Error()
							}
							//记录发送指令日志
							var conditionsLogService ConditionsLogService
							conditionsLog.DeviceId = automationAction.DeviceId
							conditionsLog.OperationType = "3"
							conditionsLog.ProtocolType = "mqtt"
							conditionsLog.Instruct = instructString
							//根据设备id获取租户id
							tenantId, _ := DeviceService.GetTenantIdByDeviceId(automationAction.DeviceId)
							conditionsLog.TenantId = tenantId
							conditionsLogService.Insert(&conditionsLog)
						}
					}

				} else if deviceModel == "2" {
					automationLogDetail.ProcessDescription = "暂不支持调动服务;"
					automationLogDetail.ProcessResult = "2"
				} else {
					automationLogDetail.ProcessDescription = "deviceModel错误;"
					automationLogDetail.ProcessResult = "2"
				}

			}

		} else if automationAction.ActionType == "2" { //告警
			automationLogDetail.ActionType = "2"
			automationLogDetail.TargetId = automationAction.WarningStrategyId
			//触发告警
			var warningStrategy models.TpWarningStrategy
			result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", automationAction.WarningStrategyId).First(&warningStrategy)
			if result.Error != nil {
				logs.Error(result.Error.Error())
				automationLogDetail.ProcessDescription = result.Error.Error()
				automationLogDetail.ProcessResult = "2"
			} else {
				if warningStrategy.RepeatCount+1 >= warningStrategy.TriggerCount {
					//触发告警,记录告警信息;triggerCount清零
					result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", automationAction.WarningStrategyId).Update("trigger_count", 0)
					if result.Error != nil {
						logs.Error(result.Error.Error())
						automationLogDetail.ProcessDescription = result.Error.Error()
						automationLogDetail.ProcessResult = "2"
					} else {
						var tpautomation TpAutomationService
						//根据设备id获取租户id
						tenantId, _ := tpautomation.GetTpAutomationTenantId(automationAction.AutomationId)
						var warningInformation models.TpWarningInformation
						warningInformation.ProcessingInstructions = ""
						warningInformation.WarningName = warningStrategy.WarningStrategyName
						warningInformation.ProcessingResult = "0"
						warningInformation.WarningDescription = warningStrategy.WarningDescription
						warningInformation.WarningLevel = warningStrategy.WarningLevel
						warningInformation.TenantId = tenantId
						var automationLog models.TpAutomationLog
						result := psql.Mydb.Model(&models.TpAutomationLog{}).Where("id = ?", automationLogId).First(&automationLog)
						if result.Error != nil {
							logs.Error(result.Error.Error())
						} else {
							warningInformation.WarningContent = strings.Split(automationLog.ProcessDescription, "|")[0]
						}
						//记录告警日志
						var warningInformationService TpWarningInformationService
						warningInformation, err := warningInformationService.AddTpWarningInformation(warningInformation)
						if err != nil {
							logs.Error(err.Error())
							automationLogDetail.ProcessDescription = err.Error()
							automationLogDetail.ProcessResult = "2"
						} else {
							logs.Info("成功触发告警")
							automationLogDetail.ProcessDescription = "成功触发告警"
							automationLogDetail.ProcessResult = "1"
						}

						// 通知告警组
						var notification TpNotificationService
						notification.ExecuteNotification(automationAction.WarningStrategyId, tenantId, "自动化告警", warningStrategy.WarningDescription)

					}

				} else {
					//重复次数计数+1
					result := psql.Mydb.Model(&models.TpWarningStrategy{}).Where("id = ?", automationAction.WarningStrategyId).Update("trigger_count", warningStrategy.TriggerCount+1)
					if result.Error != nil {
						automationLogDetail.ProcessDescription = result.Error.Error()
						automationLogDetail.ProcessResult = "2"
					} else {
						automationLogDetail.ProcessDescription = "告警当前次数" + cast.ToString(warningStrategy.TriggerCount+1) + ",未达到设定次数" + cast.ToString(warningStrategy.RepeatCount) + ",不触发告警"
						automationLogDetail.ProcessResult = "1"
					}
				}
			}

		} else if automationAction.ActionType == "3" { //场景激活
			automationLogDetail.ActionType = "3"
			automationLogDetail.TargetId = automationAction.ScenarioStrategyId
			//触发场景
			var scenarioActionService TpScenarioActionService
			err := scenarioActionService.ExecuteScenarioAction(automationAction.ScenarioStrategyId)
			if err != nil {
				logMessage = "触发场景失败：" + err.Error()
				automationLogDetail.ProcessDescription = "触发场景失败：" + err.Error()
				automationLogDetail.ProcessResult = "2"
			} else {
				automationLogDetail.ProcessDescription = "触发场景成功;"
				automationLogDetail.ProcessResult = "1"
			}

		}
		var automationLogDetailService TpAutomationLogDetailService
		automationLogDetail.Id = utils.GetUuid()
		automationLogDetail.AutomationLogId = automationLogId
		_, err := automationLogDetailService.AddTpAutomationLogDetail(automationLogDetail)
		if err != nil {
			logs.Error(err.Error())
			logMessage += err.Error()
		}
	}
	if logMessage == "" {
		logMessage = " 执行成功，执行过程请查看详情。"
	}
	return logMessage, nil
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
		// errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
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
// func (*ConditionsService) ManualTrigger(conditions_id string) error {
// 	var conditionConfig models.Condition
// 	result := psql.Mydb.First(&conditionConfig, "id = ?", conditions_id)
// 	if result.Error != nil {
// 		return result.Error
// 	}
// 	res, err := simplejson.NewJson([]byte(conditionConfig.Config))
// 	if err != nil {
// 		logs.Error(err.Error())
// 	}
// 	var DeviceService DeviceService
// 	DeviceService.ApplyControl(res, "", "2")
// 	return nil
// }

// 根据业务id获取策略下拉
// func (*ConditionsService) ConditionsPullDownList(params valid.ConditionsPullDownListValidate) ([]map[string]interface{}, error) {
// 	var values []interface{}
// 	values = append(values, params.BusinessId)
// 	sqlWhere := "business_id = ?"
// 	if params.Status != "" {
// 		values = append(values, params.Status)
// 		sqlWhere += " and status = ?"
// 	}
// 	if params.ConditionsType != "" {
// 		values = append(values, params.ConditionsType)
// 		sqlWhere += " and type = ?"
// 	}
// 	if params.Issued != "" {
// 		values = append(values, params.Issued)
// 		sqlWhere += " and issued = ?"
// 	}
// 	var conditionConfig []map[string]interface{}
// 	result := psql.Mydb.Model(&models.Condition{}).Select("id,name as policy_name,describe").Where(sqlWhere, values...).Order("sort ASC").Find(&conditionConfig)
// 	if result.Error != nil {
// 		return nil, result.Error
// 	}
// 	return conditionConfig, nil
// }
