## 开发提示
### win环境搭建
proto下载后将proto.exe放入go的bin目录下：
https://github.com/protocolbuffers/protobuf/releases/download/v23.3/protoc-23.3-win64.zip
### proto文件如果缺少依赖可能需要执行以下命令
go mod download google.golang.org/grpc

### 重新生成gRPC代码，编译proto
protoc --go_out=. --go_opt=paths=source_relative  --go-grpc_out=. --go-grpc_opt=paths=source_relative protocol_plugin.proto
