package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"project/constant"
	"project/initialize"
	config "project/mqtt"
	"project/mqtt/publish"
	simulationpublish "project/mqtt/simulation_publish"
	"project/utils"
	"strconv"
	"strings"
	"time"

	"github.com/go-basic/uuid"
	"github.com/mintance/go-uniqid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"

	dal "project/dal"
	model "project/internal/model"
)

type TelemetryData struct{}

func (t *TelemetryData) GetCurrentTelemetrData(device_id string) (interface{}, error) {
	// d, err := dal.GetCurrentTelemetrData(device_id)
	// 数据源替换
	d, err := dal.GetCurrentTelemetryDataEvolution(device_id)
	if err != nil {
		return nil, err
	}

	// 查询设备信息
	deviceInfo, err := dal.GetDeviceByID(device_id)
	if err != nil {
		return nil, err
	}
	var telemetryModelMap = make(map[string]*model.DeviceModelTelemetry)
	var telemetryModelUintMap = make(map[string]interface{})
	// 是否有设备配置
	if deviceInfo.DeviceConfigID != nil {
		// 查询设备配置
		deviceConfig, err := dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return nil, err
		}
		// 是否有设备模板
		if deviceConfig.DeviceTemplateID != nil {
			// 查询遥测模型
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, err
			}
			if len(telemetryModel) > 0 {
				// 遍历并转换为map
				for _, v := range telemetryModel {
					telemetryModelMap[v.DataIdentifier] = v
					telemetryModelUintMap[v.DataIdentifier] = v.Unit
				}
			}
		}
	}
	// 格式化返回值
	data := make([]map[string]interface{}, 0)
	if len(d) > 0 {
		for _, v := range d {
			tmp := make(map[string]interface{})
			tmp["device_id"] = v.DeviceID
			tmp["key"] = v.Key
			tmp["ts"] = v.T
			tmp["tenant_id"] = v.TenantID
			if v.BoolV != nil {
				tmp["value"] = v.BoolV
			}
			if v.NumberV != nil {
				tmp["value"] = v.NumberV
			}
			if v.StringV != nil {
				tmp["value"] = v.StringV
			}
			// 是否有设备模型
			if len(telemetryModelMap) > 0 {
				telemetryModel, ok := telemetryModelMap[v.Key]
				if ok {
					tmp["label"] = telemetryModel.DataName
					tmp["unit"] = telemetryModelUintMap[v.Key]
					tmp["data_type"] = telemetryModel.DataType
					if telemetryModel.DataType != nil && *telemetryModel.DataType == "Enum" {
						var enumItems []model.EnumItem
						json.Unmarshal([]byte(*telemetryModel.AdditionalInfo), &enumItems)
						tmp["enum"] = enumItems
					}
				}
			}
			data = append(data, tmp)
		}
	}

	return data, err
}

// 根据设备ID和key获取当前遥测数据
func (t *TelemetryData) GetCurrentTelemetrDataKeys(req *model.GetTelemetryCurrentDataKeysReq) (interface{}, error) {
	// d, err := dal.GetCurrentTelemetrData(device_id)
	// 数据源替换
	d, err := dal.GetCurrentTelemetryDataEvolutionByKeys(req.DeviceID, req.Keys)
	if err != nil {
		return nil, err
	}
	// 查询设备信息
	deviceInfo, err := dal.GetDeviceByID(req.DeviceID)
	if err != nil {
		return nil, err
	}
	var telemetryModelMap = make(map[string]*model.DeviceModelTelemetry)
	var telemetryModelUintMap = make(map[string]interface{})
	// 是否有设备配置
	if deviceInfo.DeviceConfigID != nil {
		// 查询设备配置
		deviceConfig, err := dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return nil, err
		}
		// 是否有设备模板
		if deviceConfig.DeviceTemplateID != nil {
			// 查询遥测模型
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, err
			}
			if len(telemetryModel) > 0 {
				// 遍历并转换为map
				for _, v := range telemetryModel {
					telemetryModelMap[v.DataIdentifier] = v
					telemetryModelUintMap[v.DataIdentifier] = v.Unit
				}
			}
		}
	}
	// 格式化返回值
	data := make([]map[string]interface{}, 0)
	if len(d) > 0 {
		for _, v := range d {
			tmp := make(map[string]interface{})

			tmp["device_id"] = v.DeviceID
			tmp["key"] = v.Key
			tmp["ts"] = v.T
			tmp["tenant_id"] = v.TenantID
			if v.BoolV != nil {
				tmp["value"] = v.BoolV
			}
			if v.NumberV != nil {
				tmp["value"] = v.NumberV
			}
			if v.StringV != nil {
				tmp["value"] = v.StringV
			}
			// 是否有设备模型
			if len(telemetryModelMap) > 0 {
				telemetryModel, ok := telemetryModelMap[v.Key]
				if ok {
					tmp["label"] = telemetryModel.DataName
					tmp["unit"] = telemetryModelUintMap[v.Key]
					tmp["data_type"] = telemetryModel.DataType
					if telemetryModel.DataType != nil && *telemetryModel.DataType == "Enum" {
						var enumItems []model.EnumItem
						json.Unmarshal([]byte(*telemetryModel.AdditionalInfo), &enumItems)
						tmp["enum"] = enumItems
					}
				}
			}
			data = append(data, tmp)
		}
	}

	return data, err
}

// 返回数据格式{"key":value,"key1":value1}
func (t *TelemetryData) GetCurrentTelemetrDataForWs(device_id string) (interface{}, error) {
	// d, err := dal.GetCurrentTelemetrData(device_id)

	// 数据源替换
	d, err := dal.GetCurrentTelemetryDataEvolution(device_id)
	if err != nil {
		return nil, err
	}

	// 格式化返回值
	data := make(map[string]interface{})
	if len(d) > 0 {
		for _, v := range d {
			if v.BoolV != nil {
				data[v.Key] = v.BoolV
			}
			if v.NumberV != nil {
				data[v.Key] = v.NumberV
			}
			if v.StringV != nil {
				data[v.Key] = v.StringV
			}
		}
	}
	return data, err
}

// 返回数据格式{"key":value,"key1":value1}
func (t *TelemetryData) GetCurrentTelemetrDataKeysForWs(device_id string, keys []string) (interface{}, error) {
	// d, err := dal.GetCurrentTelemetrData(device_id)

	// 数据源替换
	d, err := dal.GetCurrentTelemetryDataEvolutionByKeys(device_id, keys)
	if err != nil {
		return nil, err
	}

	// 格式化返回值
	data := make(map[string]interface{})
	if len(d) > 0 {
		for _, v := range d {
			if v.BoolV != nil {
				data[v.Key] = v.BoolV
			}
			if v.NumberV != nil {
				data[v.Key] = v.NumberV
			}
			if v.StringV != nil {
				data[v.Key] = v.StringV
			}
		}
	}
	return data, err
}

func (t *TelemetryData) GetTelemetrHistoryData(req *model.GetTelemetryHistoryDataReq) (interface{}, error) {
	// 时间戳转换
	sT := req.StartTime * 1000
	eT := req.EndTime * 1000

	d, err := dal.GetHistoryTelemetrData(req.DeviceID, req.Key, sT, eT)
	if err != nil {
		return nil, err
	}

	// 格式化返回值
	data := make([]map[string]interface{}, 0)
	if len(d) > 0 {
		for _, v := range d {
			tmp := make(map[string]interface{})

			tmp["device_id"] = v.DeviceID
			tmp["key"] = v.Key
			tmp["ts"] = v.T
			tmp["tenant_id"] = v.TenantID
			if v.BoolV != nil {
				tmp["value"] = v.BoolV
			}
			if v.NumberV != nil {
				tmp["value"] = v.NumberV
			}
			if v.StringV != nil {
				tmp["value"] = v.StringV
			}
			data = append(data, tmp)
		}
	}

	return data, nil
}

func (t *TelemetryData) DeleteTelemetrData(req *model.DeleteTelemetryDataReq) error {
	err := dal.DeleteTelemetrData(req.DeviceID, req.Key)
	if err != nil {
		return err
	}
	// 删除当前值
	err = dal.DeleteCurrentTelemetryData(req.DeviceID, req.Key)
	return err
}

func (t *TelemetryData) GetCurrentTelemetrDetailData(device_id string) (interface{}, error) {
	data, err := dal.GetCurrentTelemetrDetailData(device_id)

	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})

	dataMap["device_id"] = data.DeviceID
	dataMap["key"] = data.Key
	dataMap["ts"] = data.T
	dataMap["tenant_id"] = data.TenantID

	if data.BoolV != nil {
		dataMap["value"] = data.BoolV
	}

	if data.NumberV != nil {
		dataMap["value"] = data.NumberV
	}

	if data.StringV != nil {
		dataMap["value"] = data.StringV
	}

	return dataMap, err
}

func (t *TelemetryData) GetTelemetrHistoryDataByPage(req *model.GetTelemetryHistoryDataByPageReq) (interface{}, error) {

	if *req.ExportExcel {
		var addr string
		f := excelize.NewFile()
		f.SetCellValue("Sheet1", "A1", "时间")
		f.SetCellValue("Sheet1", "B1", "数值")

		batchSize := 100000
		offset := 0
		rowNumber := 2

		for {
			datas, err := dal.GetHistoryTelemetrDataByExport(req, offset, batchSize)
			if err != nil {
				return addr, err
			}
			if len(datas) == 0 {
				break
			}
			for _, data := range datas {
				t := time.Unix(0, data.T*int64(time.Millisecond))
				f.SetCellValue("Sheet1", fmt.Sprintf("A%d", rowNumber), t.Format("2006-01-02 15:04:05"))
				f.SetCellValue("Sheet1", fmt.Sprintf("B%d", rowNumber), *data.NumberV)
				rowNumber++
			}
			offset += batchSize
		}

		uploadDir := "./files/excel/"
		errs := os.MkdirAll(uploadDir, os.ModePerm)
		if errs != nil {
			return addr, errs
		}
		// 根据指定路径保存文件
		uniqidStr := uniqid.New(uniqid.Params{Prefix: "excel", MoreEntropy: true})
		addr = "files/excel/数据列表" + uniqidStr + ".xlsx"
		if err := f.SaveAs(addr); err != nil {
			return nil, err
		}
		return addr, nil
	}

	//  暂时忽略总数
	_, data, err := dal.GetHistoryTelemetrDataByPage(req)
	if err != nil {
		return nil, err
	}
	// 格式化
	var easyData []map[string]interface{}
	for _, v := range data {
		d := make(map[string]interface{})
		d["ts"] = v.T
		d["key"] = v.Key
		if v.StringV == nil {
			d["value"] = v.NumberV
		} else {
			d["value"] = v.StringV
		}
		easyData = append(easyData, d)
	}
	return easyData, nil
}

// 获取模拟设备发送遥测数据的回显数据
func (t *TelemetryData) GetEchoData(req *model.GetEchoDataReq) (interface{}, error) {
	// 获取设备信息
	deviceInfo, err := dal.GetDeviceByID(req.DeviceId)
	if err != nil {
		return nil, err
	}
	voucher := deviceInfo.Voucher
	// 校验voucher是否json
	if !IsJSON(voucher) {
		return nil, fmt.Errorf("voucher is not json")
	}
	var voucherMap map[string]interface{}
	err = json.Unmarshal([]byte(voucher), &voucherMap)
	if err != nil {
		return nil, err
	}
	// 判断是否有username字段
	var username, password, host, post, payload, clientID string
	if _, ok := voucherMap["username"]; !ok {
		return nil, fmt.Errorf("voucher has no MQTT username")
	}
	username = voucherMap["username"].(string)
	// 判断是否有password字段
	if _, ok := voucherMap["password"]; !ok {
		password = ""
	} else {
		password = voucherMap["password"].(string)
	}

	accessAddress := viper.GetString("mqtt.access_address")
	if accessAddress == "" {
		return nil, fmt.Errorf("mqtt access address is empty")
	}
	accessAddressList := strings.Split(accessAddress, ":")
	host = accessAddressList[0]
	post = accessAddressList[1]
	topic := config.MqttConfig.Telemetry.SubscribeTopic
	clientID = "mqtt_" + uuid.New()[0:12] //代表随机生成
	payload = `{\"test_data1\":25.5,\"test_data2\":60}`
	// 拼接命令
	command := utils.BuildMosquittoPubCommand(host, post, username, password, topic, payload, clientID)
	return command, nil

}

// 模拟设备发送遥测数据
func (t *TelemetryData) TelemetryPub(mosquittoCommand string) (interface{}, error) {
	// 解析mosquitto_pub命令
	params, err := utils.ParseMosquittoPubCommand(mosquittoCommand)
	if err != nil {
		return nil, err
	}
	// 根据凭证信息查询设备信息
	// 组装凭证信息
	var voucher string
	if params.Password == "" {
		voucher = fmt.Sprintf("{\"username\":\"%s\"}", params.Username)
	} else {
		voucher = fmt.Sprintf("{\"username\":\"%s\",\"password\":\"%s\"}", params.Username, params.Password)
	}
	// 查询设备信息
	deviceInfo, err := dal.GetDeviceByVoucher(voucher)
	if err != nil {
		return nil, err
	}
	var isOnline int
	if deviceInfo.IsOnline == int16(1) {
		isOnline = 1
	}

	// 发送mqtt消息
	logrus.Debug("params:", params)
	err = simulationpublish.PublishMessage(params.Host, params.Port, params.Topic, params.Payload, params.Username, params.Password, params.ClientId)
	if err != nil {
		return nil, err
	}
	go func() {
		time.Sleep(3 * time.Second)
		// 更新设备状态
		if isOnline == 1 {
			dal.UpdateDeviceOnlineStatus(deviceInfo.ID, int16(isOnline))
			// 发送上线消息
			// 发送mqtt消息
			err = publish.PublishOnlineMessage(deviceInfo.ID, []byte("1"))
			if err != nil {
				logrus.Error("publish online message failed:", err)
			}
		}
	}()
	return nil, nil
}

func (t *TelemetryData) GetTelemetrSetLogsDataListByPage(req *model.GetTelemetrySetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetTelemetrySetLogsListByPage(req)
	if err != nil {
		return nil, err
	}

	dataMap := make(map[string]interface{})
	dataMap["count"] = count
	dataMap["list"] = data
	return dataMap, nil

}

/*
 1. 部分参数说明：
    aggregate_window [聚合间隔]
    - no_aggregate 不聚合
    - "30s","1m","2m","5m","10m","30m","1h","3h","6h","1d","7d","1mo"
    time_range
    - 时间范围，后端支持的参数有：custom，last_5m，last_15m，last_30m，last_1h，last_3h 当选择自定义时，后端会根据开始和结束时间来判断是否超过3小时，如过超过3小时，则不能选择“不聚合”
    aggregate_function [聚合方法]
    - avg 平均数
    - max 最大值
 2. 前端筛选联动规则：
    - 页面初始化：最近1小时 - 不聚合 - 默认不展示计算方式，当选择了间隔后 展示两种计算方式（平均值/最大值）
    - 最近5分钟 - 展示全部聚合间隔
    - 最近15分钟 - 展示全部聚合间隔
    - 最近30分钟 - 展示全部聚合间隔
    - 最近1小时 - 展示全部聚合间隔
    - 最近3小时 - 间隔默认选择“30秒”（不聚合不可选） - 计算方式默认为 “平均值”
    - 最近6小时 - 间隔默认选择“1分钟”（不聚合，小于等于30秒的不可选） - 计算方式默认为 “平均值”
    - 最近12小时 - 间隔默认选择“2分钟”（不聚合，小于等于1分钟的不可选） - 计算方式默认为 “平均值”
    - 最近24小时 - 间隔默认选择“5分钟”（不聚合，小于等于2分钟的不可选） - 计算方式默认为 “平均值”
    - 最近3天 - 间隔默认选择“10分钟”（不聚合，小于等于5分钟的不可选） - 计算方式默认为 “平均值”
    - 最近7天 - 间隔默认选择“30分钟”（不聚合，小于等于10分钟的不可选） - 计算方式默认为 “平均值”
    - 最近15天 - 间隔默认选择“1小时”（不聚合，小于等于30分钟的不可选） - 计算方式默认为 “平均值”
    - 最近30天 - 间隔默认选择“1小时”（不聚合，小于等于30分钟的不可选） - 计算方式默认为 “平均值”
    - 最近60天 - 间隔默认选择“3小时”（不聚合，小于等于1小时的不可选） - 计算方式默认为 “平均值”
    - 最近90天 - 间隔默认选择“6小时”（不聚合，小于等于3小时的不可选） - 计算方式默认为 “平均值”
    - 最近6个月 - 间隔默认选择“6小时”（不聚合，小于等于3小时的不可选） - 计算方式默认为 “平均值”
    - 最近1年 - 间隔默认选择“1个月”（不聚合，小于等于7天的不可选） - 计算方式默认为 “平均值”
    - 今天 - 间隔默认选择“5分钟”（不聚合，小于等于2分钟的不可选） - 计算方式默认为 “平均值”
    - 昨天 - 间隔默认选择“5分钟”（不聚合，小于等于2分钟的不可选） - 计算方式默认为 “平均值”
    - 前天 - 间隔默认选择“5分钟”（不聚合，小于等于2分钟的不可选） - 计算方式默认为 “平均值”
    - 上周今日 - 间隔默认选择“5分钟”（不聚合，小于等于2分钟的不可选） - 计算方式默认为 “平均值”
    - 本周 - 间隔默认选择“30分钟”（不聚合，小于等于10分钟的不可选） - 计算方式默认为 “平均值”
    - 上周 - 间隔默认选择“30分钟”（不聚合，小于等于10分钟的不可选） - 计算方式默认为 “平均值”
    - 本月 - 间隔默认选择“1小时”（不聚合，小于等于30分钟的不可选） - 计算方式默认为 “平均值”
    - 上个月 - 间隔默认选择“1小时”（不聚合，小于等于30分钟的不可选） - 计算方式默认为 “平均值”
    - 今年 - 间隔默认选择“1个月”（不聚合，小于等于7天的不可选） - 计算方式默认为 “平均值”
    - 去年 - 间隔默认选择“1个月”（不聚合，小于等于7天的不可选） - 计算方式默认为 “平均值”

请求参数示例，前端可以直接用这个开发：
```

	{
	    "device_id": "4a5b326c-ba99-9ea2-34a9-1c484d69a1ab",
	    "key": "temperature",
	    "start_time": 1691048558615446,
	    "end_time": 1691048693603021,
	    "aggregate_window": "no_aggregate",
	    "time_range": "custom"
	}

```
30秒最大值
```

	{
	    "device_id": "4a5b326c-ba99-9ea2-34a9-1c484d69a1ab",
	    "key": "temperature",
	    "start_time": 1691048558615446,
	    "end_time": 1691048693603021,
	    "aggregate_window": "30s",
	    "aggregate_function":"max"
	}

```
*/
func (t *TelemetryData) GetTelemetrGetStatisticData(req *model.GetTelemetryStatisticReq) ([]map[string]interface{}, error) {
	if req.TimeRange == "custom" {
		if req.StartTime == 0 || req.EndTime == 0 || req.StartTime > req.EndTime {
			return nil, fmt.Errorf("time range is invalid")
		}
	} else {
		switch req.TimeRange {
		//last_5m，last_15m，last_30m，last_1h，last_3h，last_6h，last_12h，last_24h，last_3d，last_7d，last_15d，last_30d，last_60d
		case "last_5m":
			req.StartTime = time.Now().Add(-5*time.Minute).UnixNano() / 1e6
		case "last_15m":
			req.StartTime = time.Now().Add(-15*time.Minute).UnixNano() / 1e6
		case "last_30m":
			req.StartTime = time.Now().Add(-30*time.Minute).UnixNano() / 1e6
		case "last_1h":
			req.StartTime = time.Now().Add(-1*time.Hour).UnixNano() / 1e6
		case "last_3h":
			req.StartTime = time.Now().Add(-3*time.Hour).UnixNano() / 1e6
		case "last_6h":
			req.StartTime = time.Now().Add(-6*time.Hour).UnixNano() / 1e6
		case "last_12h":
			req.StartTime = time.Now().Add(-12*time.Hour).UnixNano() / 1e6
		case "last_24h":
			req.StartTime = time.Now().Add(-24*time.Hour).UnixNano() / 1e6
		case "last_3d":
			req.StartTime = time.Now().Add(-72*time.Hour).UnixNano() / 1e6
		case "last_7d":
			req.StartTime = time.Now().Add(-7*24*time.Hour).UnixNano() / 1e6
		case "last_15d":
			req.StartTime = time.Now().Add(-15*24*time.Hour).UnixNano() / 1e6
		case "last_30d":
			req.StartTime = time.Now().Add(-30*24*time.Hour).UnixNano() / 1e6
		case "last_60d":
			req.StartTime = time.Now().Add(-60*24*time.Hour).UnixNano() / 1e6
		case "last_90d":
			req.StartTime = time.Now().Add(-90*24*time.Hour).UnixNano() / 1e6
		case "last_6m":
			req.StartTime = time.Now().Add(-180*24*time.Hour).UnixNano() / 1e6
		case "last_1y":
			req.StartTime = time.Now().Add(-365*24*time.Hour).UnixNano() / 1e6
		default:
			return nil, fmt.Errorf("unknown time range")
		}
		req.EndTime = time.Now().UnixNano() / 1e6
	}

	// 不聚合
	if req.AggregateWindow == "no_aggregate" {
		if req.TimeRange == "custom" {
			if (req.EndTime - req.StartTime) > int64(time.Duration(3)*time.Hour/time.Microsecond) {
				return nil, fmt.Errorf("time range is too long, can not use no_aggregate")
			}
		}
		data, err := dal.GetTelemetrStatisticData(req.DeviceId, req.Key, req.StartTime, req.EndTime)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			data = []map[string]interface{}{}
		}
		return data, nil
	} else {

		if req.AggregateFunction == "" {
			req.AggregateFunction = "avg"
		}
		// 聚合查询
		data, err := dal.GetTelemetrStatisticaAgregationData(
			req.DeviceId,
			req.Key,
			req.StartTime,
			req.EndTime,
			dal.StatisticAggregateWindowMillisecond[req.AggregateWindow],
			req.AggregateFunction,
		)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			data = []map[string]interface{}{}
		}
		return data, nil
	}

}

func (t *TelemetryData) TelemetryPutMessage(ctx context.Context, userID string, param *model.PutMessage, operationType string) error {
	var (
		log = dal.TelemetrySetLogsQuery{}

		errorMessage string
	)
	// 校验param.Value必须是json
	if !json.Valid([]byte(param.Value)) {
		errorMessage = "value must be json"
	}

	deviceInfo, err := initialize.GetDeviceById(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "[TelemetryPutMessage][GetDeviceById]failed:", err)
		return err
	}
	// 获取设备配置
	var protocolType string
	var deviceConfig *model.DeviceConfig
	var deviceType string

	if deviceInfo.DeviceConfigID != nil {
		deviceConfig, err = dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "[TelemetryPutMessage][GetDeviceConfigByID]failed:", err)
			return err
		}
		deviceType = deviceConfig.DeviceType

		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
		} else {
			return fmt.Errorf("protocolType is nil")
		}
	} else {
		protocolType = "MQTT"
		deviceType = "1"

	}
	var topic string
	if protocolType == "MQTT" {
		// 网关和子设备需要特殊处理
		//messageID := common.GetMessageID()
		topic, err = getTopicByDevice(deviceInfo, deviceType, param)
		if err != nil {
			logrus.Error(ctx, "failed to get topic", err)
			return err
		}
	} else {
		// 获取主题前缀
		subTopicPrefix, err := dal.GetServicePluginSubTopicPrefixByDeviceConfigID(*deviceInfo.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "failed to get sub topic prefix", err)
			return err
		}
		topic = fmt.Sprintf("%s%s%s", subTopicPrefix, config.MqttConfig.Telemetry.PublishTopic, deviceInfo.ID)

	}
	err = publish.PublishTelemetryMessage(topic, deviceInfo, param)
	if err != nil {
		logrus.Error(ctx, "下发失败", err)
		errorMessage = err.Error()
	}
	//operationType := strconv.Itoa(constant.Manual)

	description := "下发遥测日志记录"
	logInfo := &model.TelemetrySetLog{
		ID:            uuid.New(),
		DeviceID:      param.DeviceID,
		OperationType: &operationType,
		Datum:         &(param.Value),
		Status:        nil,
		ErrorMessage:  &errorMessage,
		CreatedAt:     time.Now().UTC(),
		Description:   &description,
	}
	// 系统自动发送
	if userID == "" {
		logInfo.UserID = nil
	}
	if err != nil {
		logInfo.ErrorMessage = &errorMessage
		status := strconv.Itoa(constant.StatusFailed)
		logInfo.Status = &status
	} else {
		status := strconv.Itoa(constant.StatusOK)
		logInfo.Status = &status
	}
	_, err = log.Create(ctx, logInfo)
	return err
}

// 根据设备信息获取要发送的控制主题（内置MQTT协议）
func getTopicByDevice(deviceInfo *model.Device, deviceType string, param *model.PutMessage) (string, error) {
	if deviceType == "1" {
		return fmt.Sprintf("%s%s", config.MqttConfig.Telemetry.PublishTopic, deviceInfo.DeviceNumber), nil
	} else if deviceType == "2" || deviceType == "3" {
		gatewayInfo, err := initialize.GetDeviceById(deviceInfo.ID)
		if err != nil {
			logrus.Error(err)
			return "", err
		}
		// 修改payload
		// 解析输入的 JSON 字符串
		var inputData map[string]interface{}
		err = json.Unmarshal([]byte(param.Value), &inputData)
		if err != nil {
			return "", fmt.Errorf("解析输入 JSON 失败: %v", err)
		}
		if deviceType == "3" {
			// 校验subDeviceAddr是否为空
			if deviceInfo.SubDeviceAddr == nil {
				return "", fmt.Errorf("subDeviceAddr is nil")
			}
			// 创建新的结构
			outputData := map[string]interface{}{
				"sub_device_data": map[string]interface{}{
					*deviceInfo.SubDeviceAddr: inputData,
				},
			}

			// 将新结构转换回 JSON 字符串
			output, err := json.Marshal(outputData)
			if err != nil {
				return "", fmt.Errorf("生成输出 JSON 失败: %v", err)
			}

			param.Value = string(output)
		} else if deviceType == "2" {
			outputData := map[string]interface{}{
				"gateway_data": inputData,
			}

			// 将新结构转换回 JSON 字符串
			output, err := json.Marshal(outputData)
			if err != nil {
				return "", fmt.Errorf("生成输出 JSON 失败: %v", err)
			}

			param.Value = string(output)
		}

		return fmt.Sprintf("%s%s", config.MqttConfig.Telemetry.GatewayPublishTopic, gatewayInfo.DeviceNumber), nil
	} else {
		return "", fmt.Errorf("unknown device type")
	}

}

func (t *TelemetryData) GetMsgCountByTenantId(tenantId string) (int64, error) {
	cnt, err := dal.GetTelemetryDataCountByTenantId(tenantId)
	return cnt, err
}
