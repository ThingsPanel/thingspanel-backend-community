# ThingsPanel提供给协议插件的grpc服务


## 重新生成gRPC代码，编译proto
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative protocol_plugin.proto

