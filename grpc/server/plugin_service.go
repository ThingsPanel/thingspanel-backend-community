package server

import (
	pb "ThingsPanel-Go/grpc/protocol_plugin"
	"ThingsPanel-Go/services"
	"context"
	"encoding/json"

	"google.golang.org/protobuf/types/known/anypb"
)

func (s *server) PluginDeviceConfig(ctx context.Context, in *pb.PluginDeviceConfigRequest) (*pb.PluginDeviceReply, error) {
	var DeviceService services.DeviceService
	d := DeviceService.GetConfigByToken(in.AccessToken, "")
	jsonData, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	anyData := &anypb.Any{
		// TypeUrl: "type.googleapis.com/" + proto.MessageName(&data),
		Value: jsonData,
	}
	return &pb.PluginDeviceReply{Data: anyData}, nil
}
