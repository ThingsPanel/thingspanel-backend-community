package subscribe

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	initialize "project/initialize"
	dal "project/internal/dal"
	model "project/internal/model"
	service "project/internal/service"
	config "project/mqtt"
	"project/mqtt/publish"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// 对列处理，数据入库
func MessagesChanHandler(messages <-chan map[string]interface{}) {
	logrus.Println("批量写入协程启动")
	var telemetryList []*model.TelemetryData
	// 用于去重的 map，键为 device_id + key + timestamp，值为在 telemetryList 中的索引
	dedupeMap := make(map[string]int)

	batchSize := config.MqttConfig.Telemetry.BatchSize
	logrus.Println("每次最大写入条数：", batchSize)
	for {
		for i := 0; i < batchSize; i++ {
			// 获取消息
			// logrus.Debug("管道消息数量:", len(messages))
			message, ok := <-messages
			if !ok {
				break
			}

			// 如果配置了别的数据库，遥测数据不写入原来的库了
			dbType := viper.GetString("grpc.tptodb_type")
			if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
				continue
			}

			if tskv, ok := message["telemetryData"].(model.TelemetryData); ok {
				// 创建唯一键：device_id + key + timestamp
				uniqueKey := fmt.Sprintf("%s_%s_%d", tskv.DeviceID, tskv.Key, tskv.T)
				// 检查是否已存在相同的键
				if existingIndex, exists := dedupeMap[uniqueKey]; exists {
					logrus.Debugf("存在相同唯一键值：key:%+v, olddata:%+v, newdata:%+v", uniqueKey, telemetryList[existingIndex], tskv)
					existingData := telemetryList[existingIndex]
					if tskv.T >= existingData.T {
						telemetryList[existingIndex] = &tskv
					}
				} else {
					// 如果不存在，添加到列表并记录索引
					dedupeMap[uniqueKey] = len(telemetryList)
					telemetryList = append(telemetryList, &tskv)
				}
			} else {
				logrus.Error("管道消息格式错误")
			}
			// 如果管道没有消息，则检查入库
			if len(messages) > 0 {
				continue
			}
			break
		}

		// 如果tskvList有数据，则写入数据库
		if len(telemetryList) > 0 {
			logrus.Info("批量写入遥测数据表的条数:", len(telemetryList))
			err := dal.CreateTelemetrDataBatch(telemetryList)
			if err != nil {
				logrus.Error(err)
			}

			// 更新当前值表
			err = dal.UpdateTelemetrDataBatch(telemetryList)
			if err != nil {
				logrus.Error(err)
			}

			// 清空telemetryList和dedupeMap
			telemetryList = []*model.TelemetryData{}
			dedupeMap = make(map[string]int)
		}
	}
}

// 处理消息
func TelemetryMessages(payload []byte, topic string) {
	// 如果启用了Flow层且Adapter已注册，使用新的Flow处理流程
	if mqttAdapter != nil {
		if err := mqttAdapter.HandleTelemetryMessage(payload, topic); err != nil {
			logrus.WithError(err).Error("Flow layer telemetry processing failed")
		}
		return
	}

	// 否则使用原有的处理流程（兼容性保留）
	// 如果配置了别的数据库，遥测数据不写入原来的库了
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" || dbType == "KINGBASE" || dbType == "POLARDB" {
		logrus.Infof("do not insert db for dbType:%v", dbType)
		return
	}

	logrus.Debugln(string(payload))
	// 验证消息有效性
	telemetryPayload, err := verifyPayload(payload)
	if err != nil {
		logrus.Error(err.Error(), topic)
		return
	}
	device, err := initialize.GetDeviceCacheById(telemetryPayload.DeviceId)
	if err != nil {
		logrus.Error(err.Error())
		return
	}
	TelemetryMessagesHandle(device, telemetryPayload.Values, topic)
}

// 尝试将值解析为 JSON 字符串
func tryParseAsJSON(value interface{}) (string, bool) {
	// 先尝试将值转为字符串
	str := fmt.Sprint(value)

	// 检查是否看起来像 JSON（简单检查）
	trimmed := strings.TrimSpace(str)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {

		// 尝试解析为 JSON
		var js interface{}
		if err := json.Unmarshal([]byte(str), &js); err == nil {
			// 是有效的 JSON，重新格式化为标准 JSON
			if jsonBytes, err := json.Marshal(js); err == nil {
				return string(jsonBytes), true
			}
		}
	}

	return str, false
}

func TelemetryMessagesHandle(device *model.Device, telemetryBody []byte, topic string) {
	// TODO脚本处理
	if device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
		newtelemetryBody, err := service.GroupApp.DataScript.Exec(device, "A", telemetryBody, topic)
		if err != nil {
			logrus.Error(err.Error())
			return
		}
		if newtelemetryBody != nil {
			telemetryBody = newtelemetryBody
		}
	}
	telemetryMessagesHandleCore(device, telemetryBody, topic)
}

func telemetryMessagesHandleCore(device *model.Device, telemetryBody []byte, topic string) {
	err := publish.ForwardTelemetryMessage(device.ID, telemetryBody)
	if err != nil {
		logrus.Error("telemetry forward error:", err.Error())
	}

	// 注意: 心跳处理已由 Flow 层的 TelemetryFlow.refreshHeartbeat() 接管
	// 不再使用 HeartbeatDeal(device)

	// byte转map
	reqMap := make(map[string]interface{})
	err = json.Unmarshal(telemetryBody, &reqMap)
	if err != nil {
		logrus.Error(err.Error())
		return
	}

	ts := time.Now().UTC()
	milliseconds := ts.UnixNano() / int64(time.Millisecond)
	logrus.Debug(device, ts)
	var (
		triggerParam  []string
		triggerValues = make(map[string]interface{})
	)
	for k, v := range reqMap {
		logrus.Debug(k, "(", v, ")")
		d := model.TelemetryData{
			DeviceID: device.ID,
			Key:      k,
			T:        milliseconds,
			TenantID: &device.TenantID,
		}

		// 根据类型设置值字段
		switch value := v.(type) {
		case string:
			d.StringV = &value
		case bool:
			d.BoolV = &value
		case float64:
			d.NumberV = &value
		case int:
			// 处理整数类型
			f := float64(value)
			d.NumberV = &f
		case int64:
			// 处理长整数类型
			f := float64(value)
			d.NumberV = &f
		case []interface{}, map[string]interface{}:
			// 处理 JSON 对象或数组
			if jsonBytes, err := json.Marshal(value); err == nil {
				s := string(jsonBytes)
				d.StringV = &s
			} else {
				s := fmt.Sprint(value)
				d.StringV = &s
			}
		default:
			// 尝试检测是否为 JSON 字符串
			if jsonStr, ok := tryParseAsJSON(value); ok {
				d.StringV = &jsonStr
			} else {
				s := fmt.Sprint(value)
				d.StringV = &s
			}
		}
		triggerParam = append(triggerParam, k)
		triggerValues[k] = v
		// ts_kv批量入库
		TelemetryMessagesChan <- map[string]interface{}{
			"telemetryData": d,
		}
	}
	// go 自动化处理
	go func() {
		err = service.GroupApp.Execute(device, service.AutomateFromExt{
			TriggerParamType: model.TRIGGER_PARAM_TYPE_TEL,
			TriggerParam:     triggerParam,
			TriggerValues:    triggerValues,
		})
		if err != nil {
			logrus.Error("自动化执行失败, err: %w", err)
		}
	}()
}
