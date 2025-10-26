package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"project/initialize"
	dal "project/internal/dal"
	"project/internal/downlink"
	model "project/internal/model"
	config "project/mqtt"
	"project/mqtt/publish"
	simulationpublish "project/mqtt/simulation_publish"
	"project/pkg/constant"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/mintance/go-uniqid"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/xuri/excelize/v2"
)

type TelemetryData struct {
	downlinkBus *downlink.Bus // ✨ 依赖注入
}

// SetDownlinkBus 设置 downlink Bus（在 Application 初始化时调用）
func (t *TelemetryData) SetDownlinkBus(bus *downlink.Bus) {
	t.downlinkBus = bus
}

func (*TelemetryData) GetCurrentTelemetrData(device_id string) (interface{}, error) {
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
	telemetryModelMap := make(map[string]*model.DeviceModelTelemetry)
	telemetryModelUintMap := make(map[string]interface{})
	telemetryModelRWMap := make(map[string]interface{})
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
					telemetryModelRWMap[v.DataIdentifier] = v.ReadWriteFlag
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
					tmp["read_write_flag"] = telemetryModelRWMap[v.Key]
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
func (*TelemetryData) GetCurrentTelemetrDataKeys(req *model.GetTelemetryCurrentDataKeysReq) (interface{}, error) {
	// d, err := dal.GetCurrentTelemetrData(device_id)
	// 数据源替换
	d, err := dal.GetCurrentTelemetryDataEvolutionByKeys(req.DeviceID, req.Keys)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 查询设备信息
	deviceInfo, err := dal.GetDeviceByID(req.DeviceID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	telemetryModelMap := make(map[string]*model.DeviceModelTelemetry)
	telemetryModelUintMap := make(map[string]interface{})
	// 是否有设备配置
	if deviceInfo.DeviceConfigID != nil {
		// 查询设备配置
		deviceConfig, err := dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		// 是否有设备模板
		if deviceConfig.DeviceTemplateID != nil {
			// 查询遥测模型
			telemetryModel, err := dal.GetDeviceModelTelemetryDataList(*deviceConfig.DeviceTemplateID)
			if err != nil {
				return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
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
func (*TelemetryData) GetCurrentTelemetrDataForWs(device_id string) (interface{}, error) {
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
func (*TelemetryData) GetCurrentTelemetrDataKeysForWs(device_id string, keys []string) (interface{}, error) {
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

func (*TelemetryData) GetTelemetrHistoryData(req *model.GetTelemetryHistoryDataReq) (interface{}, error) {
	// 时间戳转换
	sT := req.StartTime * 1000
	eT := req.EndTime * 1000

	d, err := dal.GetHistoryTelemetrData(req.DeviceID, req.Key, sT, eT)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
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

func (*TelemetryData) DeleteTelemetrData(req *model.DeleteTelemetryDataReq) error {
	err := dal.DeleteTelemetrData(req.DeviceID, req.Key)
	if err != nil {
		return err
	}
	// 删除当前值
	err = dal.DeleteCurrentTelemetryData(req.DeviceID, req.Key)
	return err
}

func (*TelemetryData) GetCurrentTelemetrDetailData(device_id string) (interface{}, error) {
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

func (*TelemetryData) GetTelemetrHistoryDataByPage(req *model.GetTelemetryHistoryDataByPageReq) (interface{}, error) {
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
				return addr, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
			if len(datas) == 0 {
				break
			}
			for _, data := range datas {
				t := time.Unix(0, data.T*int64(time.Millisecond))
				f.SetCellValue("Sheet1", fmt.Sprintf("A%d", rowNumber), t.Format("2006-01-02 15:04:05"))
				// 根据字段值类型判断并写入B列
				cellRef := fmt.Sprintf("B%d", rowNumber)

				if data.StringV != nil && *data.StringV != "" {
					// 如果字符串值不为空，写入字符串值
					f.SetCellValue("Sheet1", cellRef, *data.StringV)
				} else if data.NumberV != nil {
					// 如果数值不为空，写入数值
					f.SetCellValue("Sheet1", cellRef, *data.NumberV)
				} else if data.BoolV != nil {
					// 如果布尔值不为空，写入布尔值
					f.SetCellValue("Sheet1", cellRef, *data.BoolV)
				} else {
					// 如果所有值都为空，写入空字符串或其他默认值
					f.SetCellValue("Sheet1", cellRef, "")
				}
				rowNumber++
			}
			offset += batchSize
		}

		// 创建保存目录
		uploadDir := "./files/excel/"
		if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
			return "", errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
				"error": err.Error(),
			})
		}

		// 生成文件名
		uniqidStr := uniqid.New(uniqid.Params{
			Prefix:      "excel",
			MoreEntropy: true,
		})
		addr = "files/excel/数据列表" + uniqidStr + ".xlsx"

		// 保存文件
		if err := f.SaveAs(addr); err != nil {
			return "", errcode.WithVars(errcode.CodeFileSaveError, map[string]interface{}{
				"error": err.Error(),
			})
		}

		return addr, nil
	}

	//  暂时忽略总数
	_, data, err := dal.GetHistoryTelemetrDataByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 格式化
	var easyData []map[string]interface{}
	for _, v := range data {
		d := make(map[string]interface{})
		d["ts"] = v.T
		d["key"] = v.Key
		if v.StringV != nil {
			d["value"] = v.StringV
		} else if v.NumberV != nil {
			d["value"] = v.NumberV
		} else if v.BoolV != nil {
			d["value"] = v.BoolV
		} else {
			d["value"] = ""
		}

		easyData = append(easyData, d)
	}
	return easyData, nil
}

func (*TelemetryData) GetTelemetrHistoryDataByPageV2(req *model.GetTelemetryHistoryDataByPageReq) (interface{}, error) {
	if req.ExportExcel != nil && *req.ExportExcel {
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
				return addr, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"sql_error": err.Error(),
				})
			}
			if len(datas) == 0 {
				break
			}
			for _, data := range datas {
				t := time.Unix(0, data.T*int64(time.Millisecond))
				f.SetCellValue("Sheet1", fmt.Sprintf("A%d", rowNumber), t.Format("2006-01-02 15:04:05.000"))
				f.SetCellValue("Sheet1", fmt.Sprintf("B%d", rowNumber), *data.NumberV)
				rowNumber++
			}
			offset += batchSize
		}

		// 创建保存目录
		uploadDir := "./files/excel/"
		if err := os.MkdirAll(uploadDir, os.ModePerm); err != nil {
			return nil, errcode.WithVars(errcode.CodeFilePathGenError, map[string]interface{}{
				"error": err.Error(),
			})
		}

		// 生成文件名
		uniqidStr := uniqid.New(uniqid.Params{
			Prefix:      "excel",
			MoreEntropy: true,
		})
		fileName := "数据列表" + uniqidStr + ".xlsx"
		filePath := "files/excel/" + fileName

		// 保存文件
		if err := f.SaveAs(filePath); err != nil {
			return nil, errcode.WithVars(errcode.CodeFileSaveError, map[string]interface{}{
				"error": err.Error(),
			})
		}

		result := map[string]interface{}{
			"filePath":   filePath,
			"fileName":   fileName,
			"fileType":   "excel",
			"createTime": time.Now().Format("2006-01-02T15:04:05-0700"),
		}

		return result, nil
	}

	//  暂时忽略总数
	total, data, err := dal.GetHistoryTelemetrDataByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	// 格式化
	var easyData []map[string]interface{}
	for _, v := range data {
		d := make(map[string]interface{})
		d["ts"] = v.T
		d["key"] = v.Key
		if v.StringV != nil {
			d["value"] = v.StringV
		} else if v.NumberV != nil {
			d["value"] = v.NumberV
		} else if v.BoolV != nil {
			d["value"] = v.BoolV
		} else {
			d["value"] = ""
		}

		easyData = append(easyData, d)
	}
	dataRsp := make(map[string]interface{})
	dataRsp["total"] = total
	dataRsp["list"] = easyData
	return dataRsp, nil
}

// 获取模拟设备发送遥测数据的回显数据
func (*TelemetryData) ServeEchoData(req *model.ServeEchoDataReq, clientIP string) (interface{}, error) {
	// 获取设备信息
	deviceInfo, err := dal.GetDeviceByID(req.DeviceId)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	voucher := deviceInfo.Voucher
	// 校验voucher是否json
	if !IsJSON(voucher) {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "voucher is not json")
	}
	var voucherMap map[string]interface{}
	err = json.Unmarshal([]byte(voucher), &voucherMap)
	if err != nil {
		return nil, err
	}
	// 判断是否有username字段
	var username, password, host, post, payload, clientID string
	if _, ok := voucherMap["username"]; !ok {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "username is not exist")
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
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "mqtt access address is not exist")
	}
	accessAddressList := strings.Split(accessAddress, ":")

	if clientIP == "{MQTT_HOST}" {
		host = clientIP
	} else {
		host = accessAddressList[0]
	}
	post = accessAddressList[1]
	topic := config.MqttConfig.Telemetry.SubscribeTopic
	clientID = "mqtt_" + uuid.New()[0:12] // 代表随机生成
	payload = `{\"test_data1\":25.5,\"test_data2\":60}`
	// 拼接命令
	command := utils.BuildMosquittoPubCommand(host, post, username, password, topic, payload, clientID)
	return command, nil
}

// 模拟设备发送遥测数据
func (*TelemetryData) TelemetryPub(mosquittoCommand string) (interface{}, error) {
	// 解析mosquitto_pub命令
	params, err := utils.ParseMosquittoPubCommand(mosquittoCommand)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": err.Error(),
		})
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
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	var isOnline int
	if deviceInfo.IsOnline == int16(1) {
		isOnline = 1
	}

	// 发送mqtt消息
	logrus.Debug("params:", params)
	err = simulationpublish.PublishMessage(params.Host, params.Port, params.Topic, params.Payload, params.Username, params.Password, params.ClientId)
	if err != nil {
		return nil, errcode.WithVars(500007, map[string]interface{}{
			"error_message": err.Error(),
		})
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

func (*TelemetryData) GetTelemetrSetLogsDataListByPage(req *model.GetTelemetrySetLogsListByPageReq) (interface{}, error) {
	count, data, err := dal.GetTelemetrySetLogsListByPage(req)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
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
    - 时间范围，后端支持的参数有：custom，last_5m，last_15m，last_30m，last_1h，last_3h 当选择自定义时，后端会根据开始和结束时间来判断是否超过3小时，如过超过3小时，则不能选择"不聚合"
    aggregate_function [聚合方法]
    - avg 平均数
    - max 最大值
 2. 前端筛选联动规则：
    - 页面初始化：最近1小时 - 不聚合 - 默认不展示计算方式，当选择了间隔后 展示两种计算方式（平均值/最大值）
    - 最近5分钟 - 展示全部聚合间隔
    - 最近15分钟 - 展示全部聚合间隔
    - 最近30分钟 - 展示全部聚合间隔
    - 最近1小时 - 展示全部聚合间隔
    - 最近3小时 - 间隔默认选择"30秒"（不聚合不可选） - 计算方式默认为 "平均值"
    - 最近6小时 - 间隔默认选择"1分钟"（不聚合，小于等于30秒的不可选） - 计算方式默认为 "平均值"
    - 最近12小时 - 间隔默认选择"2分钟"（不聚合，小于等于1分钟的不可选） - 计算方式默认为 "平均值"
    - 最近24小时 - 间隔默认选择"5分钟"（不聚合，小于等于2分钟的不可选） - 计算方式默认为 "平均值"
    - 最近3天 - 间隔默认选择"10分钟"（不聚合，小于等于5分钟的不可选） - 计算方式默认为 "平均值"
    - 最近7天 - 间隔默认选择"30分钟"（不聚合，小于等于10分钟的不可选） - 计算方式默认为 "平均值"
    - 最近15天 - 间隔默认选择"1小时"（不聚合，小于等于30分钟的不可选） - 计算方式默认为 "平均值"
    - 最近30天 - 间隔默认选择"1小时"（不聚合，小于等于30分钟的不可选） - 计算方式默认为 "平均值"
    - 最近60天 - 间隔默认选择"3小时"（不聚合，小于等于1小时的不可选） - 计算方式默认为 "平均值"
    - 最近90天 - 间隔默认选择"6小时"（不聚合，小于等于3小时的不可选） - 计算方式默认为 "平均值"
    - 最近6个月 - 间隔默认选择"6小时"（不聚合，小于等于3小时的不可选） - 计算方式默认为 "平均值"
    - 最近1年 - 间隔默认选择"1个月"（不聚合，小于等于7天的不可选） - 计算方式默认为 "平均值"
    - 今天 - 间隔默认选择"5分钟"（不聚合，小于等于2分钟的不可选） - 计算方式默认为 "平均值"
    - 昨天 - 间隔默认选择"5分钟"（不聚合，小于等于2分钟的不可选） - 计算方式默认为 "平均值"
    - 前天 - 间隔默认选择"5分钟"（不聚合，小于等于2分钟的不可选） - 计算方式默认为 "平均值"
    - 上周今日 - 间隔默认选择"5分钟"（不聚合，小于等于2分钟的不可选） - 计算方式默认为 "平均值"
    - 本周 - 间隔默认选择"30分钟"（不聚合，小于等于10分钟的不可选） - 计算方式默认为 "平均值"
    - 上周 - 间隔默认选择"30分钟"（不聚合，小于等于10分钟的不可选） - 计算方式默认为 "平均值"
    - 本月 - 间隔默认选择"1小时"（不聚合，小于等于30分钟的不可选） - 计算方式默认为 "平均值"
    - 上个月 - 间隔默认选择"1小时"（不聚合，小于等于30分钟的不可选） - 计算方式默认为 "平均值"
    - 今年 - 间隔默认选择"1个月"（不聚合，小于等于7天的不可选） - 计算方式默认为 "平均值"
    - 去年 - 间隔默认选择"1个月"（不聚合，小于等于7天的不可选） - 计算方式默认为 "平均值"

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
func (*TelemetryData) GetTelemetrServeStatisticData(req *model.GetTelemetryStatisticReq) (any, error) {
	// 处理时间范围
	if err := processTimeRange(req); err != nil {
		return nil, err
	}

	// 获取数据
	rspData, err := fetchTelemetryData(req)
	if err != nil {
		return nil, err
	}

	// 如果不需要导出且无数据，返回空切片
	if !req.IsExport {
		if len(rspData) == 0 {
			return []map[string]interface{}{}, nil
		}
		return rspData, nil
	}

	// 处理导出逻辑
	data, err := exportToCSV(req, rspData)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, nil
}

// 处理时间范围
func processTimeRange(req *model.GetTelemetryStatisticReq) error {
	if req.AggregateWindow == "no_aggregate" {
		// 起始时间和结束时间的差值不能大于一天，时间示例1741679355121
		if req.EndTime-req.StartTime > 24*time.Hour.Milliseconds() {
			return errcode.New(207001)
		}
	}
	if req.TimeRange == "custom" {
		if req.StartTime == 0 || req.EndTime == 0 || req.StartTime > req.EndTime {
			return errcode.New(207002) // 时间范围无效
		}
		return nil
	}

	timeRanges := map[string]time.Duration{
		"last_5m":  5 * time.Minute,
		"last_15m": 15 * time.Minute,
		"last_30m": 30 * time.Minute,
		"last_1h":  time.Hour,
		"last_3h":  3 * time.Hour,
		"last_6h":  6 * time.Hour,
		"last_12h": 12 * time.Hour,
		"last_24h": 24 * time.Hour,
		"last_3d":  72 * time.Hour,
		"last_7d":  7 * 24 * time.Hour,
		"last_15d": 15 * 24 * time.Hour,
		"last_30d": 30 * 24 * time.Hour,
		"last_60d": 60 * 24 * time.Hour,
		"last_90d": 90 * 24 * time.Hour,
		"last_6m":  180 * 24 * time.Hour,
		"last_1y":  365 * 24 * time.Hour,
	}

	duration, ok := timeRanges[req.TimeRange]
	if !ok {
		return errcode.WithVars(207003, map[string]interface{}{
			"time_range": req.TimeRange,
		})
	}

	now := time.Now()
	req.EndTime = now.UnixNano() / 1e6
	req.StartTime = now.Add(-duration).UnixNano() / 1e6
	return nil
}

// 获取遥测数据
func fetchTelemetryData(req *model.GetTelemetryStatisticReq) ([]map[string]interface{}, error) {
	if req.AggregateWindow == "no_aggregate" {
		data, err := dal.GetTelemetrStatisticData(req.DeviceId, req.Key, req.StartTime, req.EndTime)
		if err != nil {
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		return data, nil
	}

	if err := validateAggregateWindow(req.StartTime, req.EndTime, req.AggregateWindow); err != nil {
		return nil, err
	}

	if req.AggregateFunction == "" {
		req.AggregateFunction = "avg"
	}

	return dal.GetTelemetrStatisticaAgregationData(
		req.DeviceId,
		req.Key,
		req.StartTime,
		req.EndTime,
		dal.StatisticAggregateWindowMillisecond[req.AggregateWindow],
		req.AggregateFunction,
	)
}

func exportToCSV(req *model.GetTelemetryStatisticReq, data []map[string]interface{}) (map[string]interface{}, error) {
	// 检查数据是否为空
	if len(data) == 0 {
		return nil, errcode.New(202100) // 导出数据不能为空
	}

	// 创建导出目录
	exportDir := "./files/excel/telemetry/"
	if err := os.MkdirAll(exportDir, os.ModePerm); err != nil {
		return nil, errcode.WithVars(202101, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 生成文件名和路径
	fileName := fmt.Sprintf("%s_%s_%d_%d.csv", req.DeviceId, req.Key, req.StartTime, req.EndTime)
	filePath := filepath.Join(exportDir, fileName)

	// 创建文件
	file, err := os.Create(filePath)
	if err != nil {
		return nil, errcode.WithVars(202102, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 确保文件最终会被关闭和同步
	defer func() {
		syncErr := file.Sync()
		closeErr := file.Close()
		if err == nil {
			err = syncErr
		}
		if err == nil {
			err = closeErr
		}
	}()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入表头
	if err := writer.Write([]string{"时间戳", "数值"}); err != nil {
		return nil, errcode.WithVars(202103, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 写入数据
	for _, row := range data {
		timestamp, ok := row["x"].(int64)
		if !ok {
			return nil, errcode.New(202105) // 无效的时间戳格式
		}

		value, ok := row["y"].(float64)
		if !ok {
			return nil, errcode.New(202106) // 无效的数值格式
		}

		// 格式化时间
		t := time.Unix(0, timestamp*int64(time.Millisecond))
		formattedTime := t.Format("2006-01-02 15:04:05.000")

		if err := writer.Write([]string{formattedTime, fmt.Sprintf("%.3f", value)}); err != nil {
			return nil, errcode.WithVars(202104, map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	logrus.Info("CSV文件已创建:", filePath)

	return map[string]interface{}{
		"file_name": fileName,
		"file_path": filePath,
	}, nil
}

// AggregateRule 定义聚合规则结构
type AggregateRule struct {
	Days         int    // 天数
	MinInterval  string // 最小允许的聚合间隔
	FriendlyDesc string // 友好描述
}

// validateAggregateWindow 校验聚合窗口设置
func validateAggregateWindow(startTime, endTime int64, aggregateWindow string) error {
	// 计算天数
	days := int((endTime - startTime) / (24 * 60 * 60 * 1000))

	// 定义规则（从大到小排序）
	rules := []AggregateRule{
		{365, "7d", "一年"},
		{180, "1d", "6个月"},
		{90, "6h", "90天"},
		{60, "3h", "60天"},
		{30, "1h", "30天"},
		{15, "30m", "15天"},
		{7, "10m", "7天"},
		{3, "5m", "3天"},
		{1, "2m", "1天"},
	}

	// 检查规则
	for _, rule := range rules {
		if days > rule.Days && !isValidInterval(aggregateWindow, rule.MinInterval) {
			return errcode.WithVars(207004, map[string]interface{}{
				"time_range":         rule.FriendlyDesc,
				"min_interval":       rule.MinInterval,
				"current_time_range": fmt.Sprintf("%s 至 %s（%d天）", formatTime(startTime), formatTime(endTime), days),
				"aggregate_window":   aggregateWindow,
			})
		}
	}

	return nil
}

// isValidInterval 检查聚合间隔是否符合最小要求
func isValidInterval(current, minInterval string) bool {
	// 定义间隔的排序权重
	weights := map[string]int{
		"30s": 1,
		"1m":  2,
		"2m":  3,
		"5m":  4,
		"10m": 5,
		"30m": 6,
		"1h":  7,
		"3h":  8,
		"6h":  9,
		"1d":  10,
		"7d":  11,
		"1mo": 12,
	}

	currentWeight, exists := weights[current]
	if !exists {
		return false
	}

	minWeight, exists := weights[minInterval]
	if !exists {
		return false
	}

	return currentWeight >= minWeight
}

// formatTime 格式化时间戳为可读字符串
func formatTime(timestamp int64) string {
	return time.Unix(timestamp/1000, 0).Format("2006-01-02 15:04:05")
}

// TelemetryPutMessage 处理遥测数据下发
// 参数:
//
//	ctx: 上下文
//	userID: 用户ID，用于记录操作日志
//	param: 下发的消息内容
//	operationType: 操作类型
//
// 返回:
//
//	error: 处理过程中的错误
func (t *TelemetryData) TelemetryPutMessage(ctx context.Context, userID string, param *model.PutMessage, operationType string) error {
	// 步骤1: 校验入参
	// ---------------------------------------------
	// 校验参数值必须是有效的JSON
	if !json.Valid([]byte(param.Value)) {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"error": "value must be json",
		})
	}

	// 步骤2: 获取设备信息
	// ---------------------------------------------
	deviceInfo, err := initialize.GetDeviceCacheById(param.DeviceID)
	if err != nil {
		logrus.Error(ctx, "[TelemetryPutMessage][GetDeviceCacheById]failed:", err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 步骤3: 获取设备配置和协议类型
	// ---------------------------------------------
	var (
		protocolType string
		deviceConfig *model.DeviceConfig
		deviceType   string
	)

	if deviceInfo.DeviceConfigID != nil {
		// 读取设备配置信息
		deviceConfig, err = dal.GetDeviceConfigByID(*deviceInfo.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "[TelemetryPutMessage][GetDeviceConfigByID]failed:", err)
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		deviceType = deviceConfig.DeviceType

		// 获取协议类型
		if deviceConfig.ProtocolType != nil {
			protocolType = *deviceConfig.ProtocolType
		} else {
			return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error": "protocolType is nil",
			})
		}
	} else {
		// 默认协议和设备类型
		protocolType = "MQTT"
		deviceType = "1"
	}

	// 步骤4: 获取协议插件前缀和目标设备编号
	// ---------------------------------------------
	var topicPrefix string
	var targetDeviceNumber string

	// 获取Topic前缀（仅协议插件需要）
	if protocolType != "MQTT" {
		subTopicPrefix, err := dal.GetServicePluginSubTopicPrefixByDeviceConfigID(*deviceInfo.DeviceConfigID)
		if err != nil {
			logrus.Error(ctx, "failed to get sub topic prefix", err)
			return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		topicPrefix = subTopicPrefix
	}

	// 根据设备类型和协议类型确定目标设备编号
	// MQTT协议：网关/子设备需要查找顶层网关
	// 非MQTT协议（协议插件）：直接使用设备自己的编号，插件会处理层级关系
	if protocolType == "MQTT" && (deviceType == "2" || deviceType == "3") {
		// MQTT 网关/子设备：递归查找顶层网关
		topGateway, err := findTopLevelGateway(deviceInfo, deviceType)
		if err != nil {
			logrus.Error(ctx, "failed to find top level gateway", err)
			return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		targetDeviceNumber = topGateway.DeviceNumber
	} else {
		// 直连设备 或 非MQTT协议：使用设备自己的编号
		targetDeviceNumber = deviceInfo.DeviceNumber
	}

	logrus.Info("target device number:", targetDeviceNumber)
	logrus.Info("device type:", deviceType)
	logrus.Info("topic prefix:", topicPrefix)

	// 步骤5: 修改payload (仅对MQTT协议的特定设备类型)
	// ---------------------------------------------
	if protocolType == "MQTT" && (deviceType == "3" || deviceType == "2") {
		// 解析JSON
		var inputData map[string]interface{}
		if err := json.Unmarshal([]byte(param.Value), &inputData); err != nil {
			return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error": err.Error(),
			})
		}

		// 根据设备类型和是否有父网关构建不同的输出数据结构
		var outputData map[string]interface{}
		if deviceType == "3" { // 子设备
			if deviceInfo.SubDeviceAddr == nil {
				return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
					"error": "subDeviceAddr is nil",
				})
			}

			// 查找子设备的直接父网关（可能是子网关）
			parentGateway, err := initialize.GetDeviceCacheById(*deviceInfo.ParentID)
			if err != nil {
				return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
					"error": err.Error(),
				})
			}

			// 如果父网关是子网关（有parent_id），需要嵌套结构
			if parentGateway.ParentID != nil {
				// 父网关是子网关，需要构建嵌套的sub_gateway_data结构
				if parentGateway.SubDeviceAddr == nil {
					return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
						"error": "parent gateway subDeviceAddr is nil",
					})
				}
				outputData = buildNestedSubGatewayData(parentGateway, *deviceInfo.SubDeviceAddr, inputData)
			} else {
				// 父网关是顶层网关，直接构建sub_device_data
				outputData = map[string]interface{}{
					"sub_device_data": map[string]interface{}{
						*deviceInfo.SubDeviceAddr: inputData,
					},
				}
			}
		} else if deviceType == "2" { // 网关设备
			if deviceInfo.ParentID != nil {
				// 子网关：构建为sub_gateway_data格式
				if deviceInfo.SubDeviceAddr == nil {
					return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
						"error": "sub gateway subDeviceAddr is nil",
					})
				}
				outputData = map[string]interface{}{
					"sub_gateway_data": map[string]interface{}{
						*deviceInfo.SubDeviceAddr: map[string]interface{}{
							"gateway_data": inputData,
						},
					},
				}
			} else {
				// 顶层网关：构建为gateway_data格式
				outputData = map[string]interface{}{
					"gateway_data": inputData,
				}
			}
		}

		// 重新构建payload
		output, err := json.Marshal(outputData)
		if err != nil {
			return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
				"error": err.Error(),
			})
		}
		param.Value = string(output)
	}

	// 步骤6: 先创建日志记录（状态为初始）
	// ---------------------------------------------
	description := "下发遥测日志记录"
	logID := uuid.New()
	logInfo := &model.TelemetrySetLog{
		ID:            logID,
		DeviceID:      param.DeviceID,
		OperationType: &operationType,
		Datum:         &(param.Value),
		Status:        nil, // 初始状态为空，等待 Handler 更新
		ErrorMessage:  nil,
		CreatedAt:     time.Now().UTC(),
		Description:   &description,
		UserID:        &userID,
	}

	// 系统自动发送时不记录用户ID
	if userID == "" {
		logInfo.UserID = nil
	}

	// 写入日志记录
	_, err = dal.TelemetrySetLogsQuery{}.Create(ctx, logInfo)
	if err != nil {
		logrus.Error(ctx, "failed to create telemetry set log", err)
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error": err.Error(),
		})
	}

	// 步骤7: 通过 Downlink Bus 发布遥测消息
	// ---------------------------------------------
	if t.downlinkBus == nil {
		errorMessage := "downlink bus not initialized"
		logrus.Error(ctx, "下发失败: ", errorMessage)

		// 更新日志为失败
		status := strconv.Itoa(constant.StatusFailed)
		logInfo.Status = &status
		logInfo.ErrorMessage = &errorMessage
		if updateErr := dal.UpdateTelemetrySetLog(logInfo); updateErr != nil {
			logrus.Error(ctx, "failed to update telemetry set log", updateErr)
		}

		return fmt.Errorf(errorMessage)
	}

	// 构造下行消息（使用日志ID作为MessageID）
	msg := &downlink.Message{
		DeviceID:       deviceInfo.ID,
		DeviceNumber:   targetDeviceNumber, // ✅ 目标设备编号（网关/子设备时为顶层网关）
		DeviceType:     deviceType,          // ✅ 设备类型
		DeviceConfigID: getDeviceConfigID(deviceInfo),
		Type:           downlink.MessageTypeTelemetry,
		Data:           []byte(param.Value),
		Topic:          "",           // ✅ 不再传Topic，由Adapter构造
		TopicPrefix:    topicPrefix,  // ✅ 协议插件前缀
		MessageID:      logID,        // ✅ 使用日志ID作为MessageID
	}

	// 发送到 Bus（异步处理，Handler 会更新日志状态）
	t.downlinkBus.PublishTelemetry(msg)

	return nil
}

// getTopicByDevice 函数已废弃，Topic构造逻辑已移至Adapter层

// findTopLevelGateway 递归查找顶层网关（parent_id为空的网关）
func findTopLevelGateway(deviceInfo *model.Device, deviceType string) (*model.Device, error) {
	currentDevice := deviceInfo

	// 如果是子设备(3)，先找到它的父设备
	if deviceType == "3" {
		if deviceInfo.ParentID == nil {
			return nil, fmt.Errorf("子设备的parentID为空")
		}
		parentDevice, err := initialize.GetDeviceCacheById(*deviceInfo.ParentID)
		if err != nil {
			return nil, fmt.Errorf("获取父设备信息失败: %v", err)
		}
		currentDevice = parentDevice
	}

	// 递归查找顶层网关（parent_id为空的设备）
	maxDepth := 10 // 防止无限循环
	depth := 0

	for currentDevice.ParentID != nil && depth < maxDepth {
		parentDevice, err := initialize.GetDeviceCacheById(*currentDevice.ParentID)
		if err != nil {
			return nil, fmt.Errorf("获取父设备信息失败: %v", err)
		}
		currentDevice = parentDevice
		depth++
	}

	if depth >= maxDepth {
		return nil, fmt.Errorf("网关层级过深，超过最大深度限制")
	}

	// 确保找到的是网关设备（device_type=2）
	if currentDevice.DeviceConfigID != nil {
		deviceConfig, err := dal.GetDeviceConfigByID(*currentDevice.DeviceConfigID)
		if err != nil {
			return nil, fmt.Errorf("获取设备配置失败: %v", err)
		}
		if deviceConfig.DeviceType != strconv.Itoa(constant.GATEWAY_DEVICE) {
			return nil, fmt.Errorf("顶层设备不是网关类型")
		}
	}

	return currentDevice, nil
}

// buildNestedSubGatewayData 递归构建多层子网关的嵌套数据结构
func buildNestedSubGatewayData(gateway *model.Device, subDeviceAddr string, inputData map[string]interface{}) map[string]interface{} {
	if gateway.ParentID == nil {
		// 到达顶层网关，构建最内层结构
		return map[string]interface{}{
			"sub_device_data": map[string]interface{}{
				subDeviceAddr: inputData,
			},
		}
	}

	// 递归查找父网关并构建嵌套结构
	parentGateway, err := initialize.GetDeviceCacheById(*gateway.ParentID)
	if err != nil {
		// 如果出错，返回当前层级的结构
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: map[string]interface{}{
					"sub_device_data": map[string]interface{}{
						subDeviceAddr: inputData,
					},
				},
			},
		}
	}

	// 构建当前层级的嵌套结构
	innerData := buildNestedSubGatewayData(parentGateway, subDeviceAddr, inputData)

	// 如果父网关也是子网关，继续嵌套
	if parentGateway.ParentID != nil {
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: innerData,
			},
		}
	} else {
		// 父网关是顶层网关
		return map[string]interface{}{
			"sub_gateway_data": map[string]interface{}{
				*gateway.SubDeviceAddr: map[string]interface{}{
					"sub_device_data": map[string]interface{}{
						subDeviceAddr: inputData,
					},
				},
			},
		}
	}
}

func (*TelemetryData) ServeMsgCountByTenantId(tenantId string) (int64, error) {
	cnt, err := dal.GetTelemetryDataCountByTenantId(tenantId)
	if err != nil {
		return 0, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return cnt, err
}

// GetTelemetryStatisticDataByDeviceIds 根据设备ID和key查询遥测统计数据
func (*TelemetryData) GetTelemetryStatisticDataByDeviceIds(req *model.GetTelemetryStatisticByDeviceIdReq) (interface{}, error) {
	// 参数验证
	if len(req.DeviceIds) != len(req.Keys) {
		return nil, errcode.WithVars(errcode.CodeParamError, map[string]interface{}{
			"error":            "设备ID数量与key数量必须一致",
			"device_ids_count": len(req.DeviceIds),
			"keys_count":       len(req.Keys),
		})
	}

	if len(req.DeviceIds) == 0 {
		return nil, errcode.WithVars(errcode.CodeParamError, map[string]interface{}{
			"error": "设备ID和key不能为空",
		})
	}

	// 调用DAL层查询数据
	results, err := dal.GetTelemetryStatisticDataByDeviceIds(
		req.DeviceIds,
		req.Keys,
		req.TimeType,
		req.Limit,
		req.AggregateMethod,
	)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	// 直接返回ChartValue数组
	var chartData []model.ChartValue
	for _, result := range results {
		key, _ := result["key"].(string)

		if req.AggregateMethod == "count" {
			// 计数查询结果
			if count, exists := result["count"]; exists {
				if countVal, ok := count.(int64); ok {
					// 根据time_type生成相应的时间格式
					now := time.Now()
					var timeStr string
					switch req.TimeType {
					case "hour":
						timeStr = now.Format("2006-01-02 15:00:00") // 整点小时
					case "day":
						timeStr = now.Format("2006-01-02") // 只显示日期
					case "week":
						timeStr = now.Format("2006-01-02") // 周的开始日期
					case "month":
						timeStr = now.Format("2006-01") // 年-月
					case "year":
						timeStr = now.Format("2006") // 只显示年
					default:
						timeStr = now.Format("2006-01-02 15:04:05") // 默认格式
					}

					chartData = append(chartData, model.ChartValue{
						Key:   key,
						Time:  timeStr,
						Value: float64(countVal),
					})
				}
			}
		} else if req.AggregateMethod == "diff" {
			// 差值查询结果 - 时间窗口分组数据
			if data, exists := result["data"]; exists {
				if dataSlice, ok := data.([]map[string]interface{}); ok {
					for _, item := range dataSlice {
						var timeStr string
						var value float64

						// 处理时间
						if time_val, ok := item["time"].(string); ok {
							timeStr = time_val
						}

						// 处理数值
						if val, ok := item["value"].(float64); ok {
							value = val
						}

						chartData = append(chartData, model.ChartValue{
							Key:   key,
							Time:  timeStr,
							Value: value,
						})
					}
				}
			}
		} else if data, exists := result["data"]; exists {
			// 时间序列数据
			if dataSlice, ok := data.([]map[string]interface{}); ok {
				for _, item := range dataSlice {
					var timeStr string
					var value float64

					// 处理时间戳 - 根据time_type使用不同的格式
					if timestamp, ok := item["timestamp"].(int64); ok {
						t := time.Unix(0, timestamp*int64(time.Millisecond))
						switch req.TimeType {
						case "hour":
							// 小时级：保持整点小时，带时区
							hourTime := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
							timeStr = hourTime.Format("2006-01-02T15:04:05.000-07:00")
						case "day":
							// 天级：保持整天，带时区
							dayTime := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
							timeStr = dayTime.Format("2006-01-02T15:04:05.000-07:00")
						case "week":
							// 周级：保持周的开始日期，带时区
							weekTime := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
							timeStr = weekTime.Format("2006-01-02T15:04:05.000-07:00")
						case "month":
							// 月级：保持月的第一天，带时区
							monthTime := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
							timeStr = monthTime.Format("2006-01-02T15:04:05.000-07:00")
						case "year":
							// 年级：保持年的第一天，带时区
							yearTime := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
							timeStr = yearTime.Format("2006-01-02T15:04:05.000-07:00")
						default:
							timeStr = t.Format("2006-01-02T15:04:05.000-07:00") // 默认格式，带时区
						}
					}

					// 处理数值
					if val, ok := item["value"].(float64); ok {
						value = val
					} else if val, ok := item["value"].(int64); ok {
						value = float64(val)
					}

					chartData = append(chartData, model.ChartValue{
						Key:   key,
						Time:  timeStr,
						Value: value,
					})
				}
			}
		}
	}

	return chartData, nil
}

// getDeviceConfigID 获取设备配置ID（辅助函数）
func getDeviceConfigID(device *model.Device) string {
	if device.DeviceConfigID == nil {
		return ""
	}
	return *device.DeviceConfigID
}
