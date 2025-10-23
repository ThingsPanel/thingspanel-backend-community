# IoT Platform 自动化测试框架

ThingsPanel IoT 平台的自动化测试框架,用于验证 MQTT 设备接入、数据上报、指令下发等核心功能。

## 功能特性

- ✅ MQTT 直连设备模拟
- ✅ 遥测数据上报与验证
- ✅ 属性数据上报与验证
- ✅ 事件数据上报与验证
- ✅ 平台控制指令下发测试
- ✅ 平台属性设置测试
- ✅ 平台命令下发测试
- ✅ 数据库入库验证
- ✅ MQTT 消息响应验证

## 快速开始

### 1. 环境要求

- Go 1.21+
- PostgreSQL 数据库访问权限
- MQTT Broker 访问权限
- ThingsPanel 平台 API Key

### 2. 安装依赖

```bash
go mod download
```

### 3. 配置

复制配置文件模板:

```bash
cp config.example.yaml config.yaml
```

编辑 `config.yaml`,填入实际环境信息:

```yaml
mqtt:
  broker: "your-broker:1883"
  client_id: "your_client_id"
  username: "your_username"
  password: ""

device:
  device_id: "your-device-id"
  device_number: "your-device-number"

database:
  host: "your-db-host"
  port: 5432
  dbname: "ThingsPanel"
  username: "postgres"
  password: "your-password"

api:
  base_url: "http://your-api-url"
  api_key: "your-api-key"
```

### 4. 运行测试

运行所有测试:

```bash
go test ./tests/... -v
```

运行指定测试:

```bash
# 遥测数据测试
go test ./tests/telemetry_test.go -v

# 属性数据测试
go test ./tests/attribute_test.go -v

# 事件数据测试
go test ./tests/event_test.go -v

# 命令数据测试
go test ./tests/command_test.go -v
```

## 项目结构

