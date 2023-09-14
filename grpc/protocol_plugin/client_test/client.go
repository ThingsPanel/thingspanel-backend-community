// Package main implements a client for Greeter service.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	pb "test/protocol_plugin"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var addr = flag.String("addr", "localhost:50051", "the address to connect to")

func main() {
	flag.Parse()

	// Set up a connection to the server.
	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewProtocolPluginServiceClient(conn)

	// Contact the server and print out its response.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// 获取设备配置
	//PluginDeviceConfig(client, ctx)

	// 获取设备配置list
	PluginDeviceConfigList(client, ctx)
}

// 获取设备配置
func PluginDeviceConfig(client pb.ProtocolPluginServiceClient, ctx context.Context) {
	response, err := client.PluginDeviceConfig(ctx, &pb.PluginDeviceConfigRequest{AccessToken: "15ffb096-7128-db08-6757-2938d5b83b06"})
	if err != nil {
		log.Fatalf("could not retrieve config: %v", err)
	}

	fmt.Println("Response data:", string(response.Data))
}

// 获取设备配置list
func PluginDeviceConfigList(client pb.ProtocolPluginServiceClient, ctx context.Context) {
	response, err := client.PluginDeviceConfigList(ctx, &pb.PluginDeviceConfigListRequest{ProtocolType: "MODBUS_RTU", DeviceType: "2"})
	if err != nil {
		log.Fatalf("could not retrieve config: %v", err)
	}

	fmt.Println("Response data:", string(response.Data))

}
