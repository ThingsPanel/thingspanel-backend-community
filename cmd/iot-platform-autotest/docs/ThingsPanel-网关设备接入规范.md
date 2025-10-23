# ThingsPanel MQTT 网关设备接入规范

## 1. 基本约定
- `message_id`：消息标识符，需在短时间内保持唯一，推荐使用毫秒时间戳的后几位或递增序列。
- `device_number`：设备编号，在 ThingsPanel 平台内唯一。
- `sub_device_address`：子设备地址或标识，用于区分挂载在网关下的子设备。
- 数据交互类型分为四类：遥测（Telemetry）、属性（Attributes）、事件（Event）、命令（Command）。
- MQTT 方向说明：
  - 设备上行（设备 → 平台）：遥测 / 属性 / 事件上报，命令与属性设置响应。
  - 平台下行（平台 → 设备）：控制指令、属性设置、属性获取请求、命令下发。

## 2. 设备能力要求
- 支持 MQTT 3.1.1 或以上版本客户端协议，并具备断线重连能力。
- 支持 JSON 格式消息构造与解析。
- 能够稳定联网，并安全存储平台下发的鉴权凭证。
- 维护本地子设备与子网关拓扑，并按需映射至消息体。

## 3. 接入流程
1. 在 ThingsPanel 平台创建网关设备并获取 `device_number` 与鉴权信息。
2. 网关设备连接至平台提供的 MQTT 服务器。
3. 完成鉴权后发送首次上行数据激活设备。
4. 平台校验并标记设备为在线。
5. 设备按规范周期性上报数据并响应指令。
6. 平台可向网关及其子设备下发控制、属性或命令。

## 4. MQTT 主题定义

### 4.1 设备上行（平台订阅）
| 主题 | 描述 |
| --- | --- |
| `gateway/telemetry` | 网关及子设备遥测数据上报 |
| `gateway/attributes/{message_id}` | 属性上报 |
| `gateway/event/{message_id}` | 事件上报 |
| `gateway/command/response/{message_id}` | 命令执行结果 |
| `gateway/attributes/set/response/{message_id}` | 属性设置执行结果 |

### 4.2 平台下行（设备订阅）
| 主题 | 描述 |
| --- | --- |
| `gateway/telemetry/control/{device_number}` | 遥测控制指令 |
| `gateway/attributes/set/{device_number}/+` | 属性设置指令（`+` 处为 `message_id`） |
| `gateway/attributes/get/{device_number}` | 属性查询请求 |
| `gateway/command/{device_number}/+` | 命令下发（`+` 处为 `message_id`） |
| `gateway/attributes/response/{device_number}/+` | 平台收到属性后的确认信息 |
| `gateway/event/response/{device_number}/+` | 平台收到事件后的确认信息 |

> `message_id` 建议由设备生成并写入上下行 payload，便于平台端的链路追踪与幂等控制。

## 5. 上行报文结构

### 5.1 通用结构
除命令与属性设置响应外，上行消息建议使用统一的分层结构，以适配多级网关拓扑：

```json
{
  "gateway_data": { ... },
  "sub_device_data": {
    "sub_device_address": { ... }
  },
  "sub_gateway_data": {
    "sub_gateway_number": {
      "gateway_data": { ... },
      "sub_device_data": { ... },
      "sub_gateway_data": { ... }
    }
  }
}
```

- `gateway_data`：当前网关自身数据。
- `sub_device_data`：该网关直连的普通子设备数据，key 为 `sub_device_address`。
- `sub_gateway_data`：挂载的子网关集合，可递归嵌套，缺省表示无子网关。

### 5.2 遥测上报示例
```json
{
  "gateway_data": {
    "temperature": 28.5,
    "firmware": "v0.1",
    "switch": true
  },
  "sub_device_data": {
    "28da4985": {
      "temperature": 27.1,
      "switch": false
    }
  }
}
```

### 5.3 属性上报示例
```json
{
  "gateway_data": {
    "ip": "192.168.1.10",
    "version": "v1.0"
  },
  "sub_device_data": {
    "sensor_01": {
      "ip": "192.168.1.21",
      "version": "v1.0"
    }
  }
}
```

### 5.4 事件上报示例
```json
{
  "gateway_data": {
    "method": "FindAnimal",
    "params": {
      "count": 2,
      "animalType": "cat"
    }
  },
  "sub_device_data": {
    "sensor_01": {
      "method": "FindAnimal",
      "params": {
        "count": 1,
        "animalType": "dog"
      }
    }
  }
}
```

### 5.5 命令与属性设置响应
命令或属性设置响应需要发布到对应的响应主题，payload 应遵循第 7 节的响应规范。例如：

```json
{
  "result": 0,
  "message": "success",
  "ts": 1609143039,
  "method": "Reboot"
}
```

## 6. 下行报文示例

### 6.1 遥测控制
```json
{
  "gateway_data": {
    "switch": true,
    "temperature": 25.5
  },
  "sub_device_data": {
    "28da4985": {
      "switch": false
    }
  }
}
```

### 6.2 属性设置
```json
{
  "gateway_data": {
    "ip": "192.168.1.10",
    "port": 1883
  },
  "sub_device_data": {
    "sensor_01": {
      "ip": "192.168.1.21",
      "threshold": 10
    }
  }
}
```

### 6.3 属性获取请求
```json
{
  "gateway_data": [],
  "sub_device_data": {
    "sensor_01": [
      "temperature",
      "humidity"
    ],
    "sensor_02": []
  }
}
```

- 空数组表示请求节点的全部属性。
- 指定字段数组表示仅请求对应属性。

### 6.4 命令下发
```json
{
  "gateway_data": {
    "method": "Reboot",
    "params": {
      "delay": 5
    }
  },
  "sub_device_data": {
    "sensor_01": {
      "method": "Calibrate",
      "params": {
        "mode": "full"
      }
    }
  }
}
```

## 7. 响应规范
| 字段 | 必填 | 类型 | 说明 |
| --- | --- | --- | --- |
| `result` | 是 | number | 0 表示成功，1 表示失败 |
| `errcode` | 否 | string | 失败时的错误码 |
| `message` | 是 | string | 结果描述 |
| `ts` | 否 | number | 时间戳（秒） |
| `method` | 否 | string | 关联的事件或命令名称 |

示例：
```json
{"result":0,"message":"success","ts":1609143039}
{"result":1,"errcode":"INVALID_PARAM","message":"invalid battery value","ts":1609143039,"method":"SetAttributes"}
```

## 8. 多级网关支持
为兼容网关挂载子网关的场景，上、下行消息均可通过 `sub_gateway_data` 字段递归描述更多层级。示例：

```json
{
  "gateway_data": {
    "temperature": 26.8,
    "version": "v1.0"
  },
  "sub_device_data": {
    "direct_device_001": {
      "temperature": 25.0,
      "version": "v1.0"
    }
  },
  "sub_gateway_data": {
    "edge_gateway_001": {
      "gateway_data": {
        "temperature": 28.5,
        "version": "v0.1",
        "switch": true
      },
      "sub_device_data": {
        "28da4985": {
          "temperature": 28.5,
          "version": "v0.1",
          "switch": true
        }
      },
      "sub_gateway_data": {
        "edge_gateway_002": {
          "gateway_data": {
            "temperature": 29.1
          }
        }
      }
    }
  }
}
```

- 平台按嵌套结构递归解析各级设备数据。
- 旧版设备若不携带 `sub_gateway_data` 字段，平台仍可按单层结构向后兼容。
