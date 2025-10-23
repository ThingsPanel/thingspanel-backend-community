# ThingsPanel MQTT 设备接入规范

## 1. 基本概念

在开始之前,让我们先了解一下四个核心概念:

- **遥测 (Telemetry)**: 设备实时上报的数据,通常是随时间变化的测量值
  - 示例: 温度传感器定期上报的温度读数
  
- **属性 (Attributes)**: 设备的静态或较少变化的特征
  - 示例: 设备的IP地址、MAC地址或固件版本
  
- **事件 (Events)**: 设备中发生的特定事件或状态变化
  - 示例: 检测到运动或设备启动完成
  
- **命令 (Commands)**: 从平台发送到设备的指令,用于控制设备行为或请求特定操作
  - 示例: 开关灯或重置设备

---

## 2. 关键参数说明

### 2.1 message_id (消息标识符)
- 用途: 唯一标识一条消息
- 建议: 使用毫秒时间戳的后七位,确保短期内不重复

### 2.2 device_number (设备编号)
- 用途: 设备的唯一标识符

### 2.3 method (方法)
- 定义: 标识特定的命令或事件类型
- 作用:
  - **命令**: 指定设备应执行的操作
    - 示例: `SetTemperature`、`TurnOnLight`、`RebootDevice`
  - **事件**: 指示发生的事件类型
    - 示例: `TemperatureExceeded`、`MotionDetected`、`BatteryLow`

### 2.4 params (参数)
- 定义: 包含与 method 相关的详细信息或数据
- 作用:
  - **命令**: 执行命令所需的具体参数
  - **事件**: 描述事件的相关数据

---

## 3. MQTT 认证规则

### 认证要求

| 项目 | 要求 |
|------|------|
| **唯一性** | Username + Password 组合必须唯一<br>ClientID 必须唯一 |
| **一致性** | 设备每次连接使用相同的 ClientID、Username 和 Password |

---

## 4. MQTT 主题规范

> **重要说明**: 设备无需实现所有列出的MQTT主题。应根据设备的具体功能和应用场景,选择性地实现相关主题。

### 4.1 设备上报主题

| 主题 | 说明 | 报文示例 |
|------|------|----------|
| `devices/telemetry` | 上报遥测数据 | 见 4.1.1 |
| `devices/attributes/{message_id}` | 上报属性数据 | 见 4.1.2 |
| `devices/event/{message_id}` | 上报事件 | 见 4.1.3 |
| `devices/command/response/{message_id}` | 上报命令响应 | 见 5.1 |
| `devices/attributes/set/response/{message_id}` | 上报属性设置响应 | 见 5.1 |
| `ota/devices/progress` | 上报OTA升级进度 | 见 4.1.4 |

#### 4.1.1 遥测上报报文

```json
{
  "temperature": 28.5,
  "switch": true
}
```

#### 4.1.2 属性上报报文

```json
{
  "ip": "127.0.0.1",
  "mac": "xxxxxxxxxx",
  "port": 1883
}
```

#### 4.1.3 事件上报报文

```json
{
  "method": "FindAnimal",
  "params": {
    "count": 2,
    "animalType": "cat"
  }
}
```

#### 4.1.4 OTA升级进度上报

**成功进度**:
```json
{
  "step": "100",
  "desc": "升级进度100%",
  "module": "MCU"
}
```

**失败示例**:
```json
{
  "step": "-1",
  "desc": "OTA升级失败,请求不到升级包信息。",
  "module": "MCU"
}
```

**步骤说明**:
- `1~100`: 升级进度百分比
- `-1`: 升级失败
- `-2`: 下载失败
- `-3`: 校验失败
- `-4`: 烧写失败

---

### 4.2 设备订阅主题

> 注意: `+` 表示 message_id 通配符

| 主题 | 说明 | 报文示例 |
|------|------|----------|
| `devices/telemetry/control/{device_number}` | 接收平台下发的控制指令 | 见 4.2.1 |
| `devices/attributes/set/{device_number}/+` | 接收平台下发的属性设置 | 见 4.2.2 |
| `devices/attributes/get/{device_number}` | 接收平台的属性获取请求 | 见 4.2.3 |
| `devices/command/{device_number}/+` | 接收平台下发的命令 | 见 4.2.4 |
| `devices/attributes/response/{device_number}/+` | 接收平台对属性上报的响应 | 见 5.1 |
| `devices/event/response/{device_number}/+` | 接收平台对事件上报的响应 | 见 5.1 |
| `ota/devices/inform/{device_number}` | 接收OTA升级任务 | 见 4.2.5 |

#### 4.2.1 接收控制指令

```json
{
  "temperature": 28.5,
  "light": 2000,
  "switch": true
}
```

#### 4.2.2 接收属性设置

```json
{
  "ip": "127.0.0.1",
  "mac": "xxxxxxxxxx",
  "port": 1883
}
```

#### 4.2.3 接收属性获取请求

**请求所有属性**:
```json
{
  "keys": []
}
```

**请求指定属性**:
```json
{
  "keys": ["temp", "hum"]
}
```

#### 4.2.4 接收命令

```json
{
  "method": "ReSet",
  "params": {
    "switch": 1,
    "light": "close"
  }
}
```

#### 4.2.5 接收OTA升级任务

**参数说明**:

| 参数 | 类型 | 说明 |
|------|------|------|
| id | Long | 消息ID号,在当前设备中唯一 |
| code | String | 状态码 |
| version | String | 升级包版本信息 |
| size | Long | 升级包文件大小(字节) |
| url | String | 升级包OSS存储地址 |
| sign | String | 升级包文件签名 |
| signMethod | String | 签名方法: SHA256 或 MD5 |
| module | String | 升级包所属模块名 |
| extData | Object | 升级批次标签和自定义信息 |

**示例报文**:
```json
{
  "id": "123",
  "code": 200,
  "params": {
    "version": "1.1",
    "size": 432945,
    "url": "http://dev.thingspane.cn/files/ota/s121jg3245gg.zip",
    "signMethod": "Md5",
    "sign": "a243fgh4b9v",
    "module": "MCU",
    "extData": {
      "key1": "value1",
      "key2": "value2"
    }
  }
}
```

---

## 5. 响应规范

### 5.1 响应报文格式

**参数说明**:

| 参数 | 必填 | 类型 | 说明 |
|------|------|------|------|
| result | 是 | number | 0-成功, 1-失败 |
| errcode | 否 | string | 错误码 |
| message | 是 | string | 消息内容 |
| ts | 否 | number | 时间戳(秒) |
| method | 否 | string | 事件和命令的方法标识 |

### 5.2 响应示例

**成功响应**:
```json
{
  "result": 0,
  "message": "success",
  "ts": 1609143039
}
```

**失败响应**:
```json
{
  "result": 1,
  "errcode": "xxx",
  "message": "xxxx",
  "ts": 1609143039,
  "method": "xxxx"
}
```

**带方法的成功响应**:
```json
{
  "result": 0,
  "message": "success",
  "ts": 1609143039,
  "method": "xxxx"
}
```

---

## 6. 通信流程图

```
设备                                    平台
 |                                       |
 |------ 上报遥测 (telemetry) -------->  |
 |<----- 响应 (result:0) --------------|
 |                                       |
 |------ 上报属性 (attributes) ------->  |
 |<----- 响应 (result:0) --------------|
 |                                       |
 |<----- 下发命令 (command) ------------|
 |------ 命令响应 (response) --------->  |
 |                                       |
```

---

## 7. 状态码说明

### 7.1 下发日志状态码

| 状态码 | 说明 |
|--------|------|
| 1 | 发送成功 |
| 2 | 发送失败 |
| 3 | 响应成功(设备已回复) |

### 7.2 操作类型

| 类型 | 说明 |
|------|------|
| 1 | 手动操作 |
| 2 | 自动触发 |


