package dal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"

	tptodb "project/grpc/tptodb_client"
	pb "project/grpc/tptodb_client/grpc_tptodb"
)

// 从 telemetry_current_datas 中获取遥测当前数据，用于替换 telemetry_datas
func GetCurrentTelemetryDataEvolution(deviceId string) ([]*model.TelemetryCurrentData, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" {
		telemetry := []*model.TelemetryCurrentData{}
		request := &pb.GetDeviceAttributesCurrentsRequest{
			DeviceId: deviceId,
		}

		r, err := tptodb.TptodbClient.GetDeviceAttributesCurrents(context.Background(), request)
		if err != nil {
			logrus.Printf("GetDeviceAttributesCurrents err:%+v", err)
			return nil, err
		}
		logrus.Printf("data: %+v", r.Data)
		err = json.Unmarshal([]byte(r.Data), &telemetry)
		if err != nil {
			logrus.Printf("Unmarshal err:%v", err)
			return nil, err
		}
		return telemetry, nil
	}

	data, err := query.TelemetryCurrentData.Where(query.TelemetryCurrentData.DeviceID.Eq(deviceId)).Order(query.TelemetryCurrentData.T.Desc()).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

// 从 telemetry_current_datas 中获取遥测当前数据，用于替换 telemetry_datas
func GetCurrentTelemetryDataEvolutionByKeys(deviceId string, keys []string) ([]*model.TelemetryCurrentData, error) {
	dbType := viper.GetString("grpc.tptodb_type")
	if dbType == "TSDB" {
		data := make([]*model.TelemetryCurrentData, 0)
		fields := make([]map[string]interface{}, 0)
		request := &pb.GetDeviceAttributesCurrentsRequest{
			DeviceId:  deviceId,
			Attribute: keys,
		}
		r, err := tptodb.TptodbClient.GetDeviceAttributesCurrents(context.Background(), request)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return nil, err
		}
		err = json.Unmarshal([]byte(r.Data), &fields)
		if err != nil {
			logrus.Printf("err: %+v", err)
			return nil, err
		}
		logrus.Printf("fields: %+v", fields)

		err = json.Unmarshal([]byte(r.Data), &data)
		if err != nil {
			logrus.Printf("Unmarshal err:%v", err)
			return nil, err
		}

		return data, nil
	}

	data, err := query.TelemetryCurrentData.Where(query.TelemetryCurrentData.DeviceID.Eq(deviceId), query.TelemetryCurrentData.Key.In(keys...)).Order(query.TelemetryCurrentData.T.Desc()).Find()
	if err != nil {
		return nil, err
	}
	return data, nil
}

func GetCurrentTelemetryDataOneKeys(deviceId string, keys string) (string, error) {
	data, err := query.TelemetryCurrentData.Where(query.TelemetryCurrentData.DeviceID.Eq(deviceId), query.TelemetryCurrentData.Key.Eq(keys)).Order(query.TelemetryCurrentData.T.Desc()).First()
	var result string
	if err != nil {
		return result, err
		//} else if err == gorm.ErrRecordNotFound {
	} else if errors.Is(err, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if data.BoolV != nil {
		result = fmt.Sprintf("%t", *data.BoolV)
	}
	if data.NumberV != nil {
		result = fmt.Sprintf("%f", *data.NumberV)
	}
	if data.StringV != nil {
		result = *data.StringV
	}
	return result, nil
}

// 根据ID和key删除当前遥测数据
func DeleteCurrentTelemetryData(deviceId string, key string) error {
	_, err := query.TelemetryCurrentData.Where(query.TelemetryCurrentData.DeviceID.Eq(deviceId), query.TelemetryCurrentData.Key.Eq(key)).Delete()
	return err
}
