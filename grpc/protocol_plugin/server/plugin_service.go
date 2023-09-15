package server

import (
	pb "ThingsPanel-Go/grpc/protocol_plugin"
	"ThingsPanel-Go/services"
	"context"
	"encoding/json"
	"errors"

	"github.com/beego/beego/v2/core/logs"
)

// 获取设备配置
func (s *server) PluginDeviceConfig(ctx context.Context, in *pb.PluginDeviceConfigRequest) (*pb.PluginDeviceConfigReply, error) {
	var deviceService services.DeviceService
	responseData := &pb.PluginDeviceConfigReply{Data: nil}

	configData, err := deviceService.GetConfigByToken(in.AccessToken, in.DeviceID)
	if err != nil {
		logs.Error("Failed to get config data: %v", err)
		return responseData, err
	}

	if configData == nil {
		err := errors.New("config data is nil")
		logs.Error("Error: %v", err)
		return responseData, err
	}

	jsonBytes, err := json.Marshal(configData)
	if err != nil {
		logs.Error("Failed to marshal config data: %v", err)
		return responseData, err
	}

	responseData.Data = jsonBytes
	return responseData, nil
}

// 获取设备配置列表（通过协议类型和设备类型）
func (s *server) PluginDeviceConfigList(ctx context.Context, in *pb.PluginDeviceConfigListRequest) (*pb.PluginDeviceConfigListReply, error) {
	var deviceService services.DeviceService
	responseData := &pb.PluginDeviceConfigListReply{Data: nil}

	configData, err := deviceService.GetConfigByProtocolAndDeviceType(in.ProtocolType, in.DeviceType)
	if err != nil {
		logs.Error("Failed to get config data: %v", err)
		return responseData, err
	}

	if configData == nil {
		err := errors.New("config data is nil")
		logs.Error("Error: %v", err)
		return responseData, err
	}

	jsonBytes, err := json.Marshal(configData)
	if err != nil {
		logs.Error("Failed to marshal config data: %v", err)
		return responseData, err
	}

	responseData.Data = jsonBytes
	return responseData, nil
}
