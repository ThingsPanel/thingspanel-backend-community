/*
 *
 * Copyright 2015 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package main implements a client for Greeter service.
package tptodb

import (
	"fmt"
	"time"

	pb "ThingsPanel-Go/grpc/tptodb_client/grpc_tptodb"

	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var TptodbClient pb.ThingsPanelClient

func GrpcTptodbInit() {
	fmt.Println("grpc tptodb init...")
	var conn *grpc.ClientConn
	var err error
	grpcHost := viper.GetString("grpc.tptodb_server")
	for {
		// 尝试连接服务器
		conn, err = grpc.Dial(grpcHost, grpc.WithTransportCredentials(insecure.NewCredentials()))

		if err == nil {
			// 如果连接成功，则退出循环
			break
		}

		fmt.Println("Failed to connect:", err)
		fmt.Println("Retrying in 5 seconds...")
		time.Sleep(5 * time.Second) // 等待5秒
	}

	// 如果连接成功，初始化客户端
	TptodbClient = pb.NewThingsPanelClient(conn)
	//r, err := TptodbClient.GetDeviceAttributesCurrents(context.Background(), &pb.GetDeviceAttributesCurrentsRequest{})
	// 在这里可以添加其他的逻辑
}
