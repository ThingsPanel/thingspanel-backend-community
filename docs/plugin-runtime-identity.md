# 受管插件运行时身份（Managed Plugin Runtime Identity）

给安装器（T5）、Agent、SDK 使用的接口说明。后端实现：`internal/pluginidentity`（GORM 存储）、`internal/middleware/plugin_runtime_identity.go`（守卫）、`internal/api/plugin_runtime_identity.go`（签发/吊销与作用域检查）。迁移：PG `sql/33.sql`，MySQL `sql/mysql/015_plugin_runtime_identity.sql`。

## 1. 模型

一个身份 = **（租户 `tenant_id`，插件实例 `plugin_instance`，服务标识 `service_identifier`，代次 `generation`）** + 一个随机密钥。只存密钥的 SHA-256；令牌只在签发时返回一次。

- `plugin_instance`：安装器给每个已安装插件实例起的稳定名字（同一租户内唯一，例如 `homeassistant-default`）。**与 Agent 的实例名保持一致。**
- `service_identifier`：插件在平台里注册的服务标识（协议类型）。设备配置的 `protocol_type` 必须等于它。
- `generation`：整数 ≥ 1，**与 Agent 的代次（激活/撤销时的 generation）是同一个数**。安装、升级、重装、重启后 Agent 换代次时，安装器用同一个数向后端签发新身份。

## 2. 谁签发、怎么签发

**签发者：该租户的租户管理员会话**（用户 JWT，`authority=TENANT_ADMIN`），通常由安装器在安装/升级时以管理员身份调用。插件自己、普通用户、系统管理员无租户时都不能签发；插件令牌调用签发接口返回 403。

```
POST /api/v1/plugin/runtime/identity          （需要租户管理员 JWT）
{ "plugin_instance": "homeassistant-default",
  "service_identifier": "homeassistant",
  "generation": 3,
  "ttl_hours": 24 }                            // 1..24
→ 200 { "data": { "token": "pri_<16hex>.<48hex>", "instance_id": "pri_...",
                  "generation": 3, "service_identifier": "...", "expires_at": "..." } }
→ 409  generation 不高于该实例已有的最高代次（含已吊销的）
→ 403  不是租户管理员
```

规则：

- **更高代次签发时，同一租户同一实例的所有较低代次被同一事务吊销**（旧实例立即失去平台访问）。相同或更低代次 → 409，不会改变任何已有身份。
- 不同租户的同名实例互相独立。
- 响应带 `Cache-Control: no-store`。令牌不入库、不入日志。

## 3. 怎么交给插件

安装器/Agent 在启动该代次的容器时，把令牌作为**环境变量 `TP_PLUGIN_RUNTIME_TOKEN`**（或只读挂载文件，路径由 `TP_PLUGIN_RUNTIME_TOKEN_FILE` 指定）注入容器。**不写进镜像、不写进 compose 文件的持久化副本、不打印日志。** 同时注入 `TP_PLUGIN_SERVICE_IDENTIFIER`（SDK 用它填请求体）。

## 4. 插件请求头格式

调用 5 个插件入口时：

```
Authorization: Bearer pri_<16hex>.<48hex>
```

仅此一种。其它任何东西（用户 JWT、`x-token`、`X-Plugin-Active`、`X-Instance-Id`、`X-Tenant-Id` 等请求头、没有 `Bearer ` 前缀）都**不被识别**，等同未带身份。

5 个入口（均为 `POST /api/v1/...`）：`plugin/heartbeat`、`plugin/device/config`、`plugin/devices`、`plugin/service/access/list`、`plugin/service/access`。

### 失败响应

| 情况 | 状态码 | `data.errorCode` |
| --- | --- | --- |
| 无身份、格式错误、未知、密钥错误、已吊销、已过期、已被更高代次取代、用户 JWT、伪造请求头 | **503** | `PLUGIN_RUNTIME_IDENTITY_REQUIRED`（`state=blocked`，`automatic_retry=false`） |
| 身份有效但请求越出范围（别的租户、别的服务/协议） | **403** | `PLUGIN_RUNTIME_SCOPE_DENIED` |

所有 503 的原因对调用方不可区分（不泄露令牌是否存在）。SDK 收到 503 `PLUGIN_RUNTIME_IDENTITY_REQUIRED` 必须明确报错并**停止重试**，由 Agent/安装器决定重新签发。

### 各入口的作用域

- `heartbeat`、`devices`、`service/access/list`：请求体的 `service_identifier` 必须等于身份的服务，否则 403。
- `device/config`：设备必须属于身份的租户，且其设备配置的 `protocol_type` 等于身份的服务；不存在的设备走原有“未找到”路径。
- `devices`：结果只含身份租户的设备。
- `service/access/list`：结果只含身份租户的接入点。
- `service/access`：接入点必须属于身份的租户，且属于身份服务对应的服务插件。

## 5. 租约、轮换与吊销

- **租约**：`ttl_hours`（≤24h）即租约。到期后身份被拒绝（503）。本期**心跳不续租**；续约方式是安装器在到期前用**更高代次**重新签发（Agent 本来就在换代次时重启容器）。需要不换代次的续租属于后续工作（见下文 TODO）。Agent 与平台失联、无法续约时，身份自然到期，插件失去平台访问，与 Agent 的“失联租约到期”行为一致。
- **轮换**：签发 `generation = 旧值 + 1`。旧令牌立即失效，没有宽限窗口；安装器必须先签发、再用新令牌启动新容器、最后停旧容器（Agent 的激活会做旧代次拦截）。
- **吊销**：`POST /api/v1/plugin/runtime/identity/revoke {"plugin_instance": "..."}`（租户管理员 JWT），吊销该租户该实例所有存活代次，返回 `{"revoked": N}`。卸载插件时调用。吊销之后同一实例再装需要比以往任何代次更高的 `generation`。
- 租户隔离：吊销只作用于调用者所在租户。

## 6. 与 Agent 代次的对应

| Agent | 后端身份 |
| --- | --- |
| 激活实例 generation=N | 签发 generation=N 并把令牌注入该代次容器 |
| 撤销实例 | 调用 revoke |
| 旧代次拦截（老容器仍在跑） | 后端侧：N+1 签发时 N 已被吊销，老容器所有入口 503 |
| 失联租约到期 | 身份 `expires_at` 到期后 503 |

## 7. 现状与未做（TODO）

- **Broker 的 MQTT 凭据没有细分**：插件的 MQTT 连接目前不使用这个身份，仍沿用现有 broker 凭据/授权（见 `internal/brokercontrol`）。需要按实例独立凭据时另立任务。
- 心跳续租（同代次延长租约）未做。
- 旧的 `RejectUnboundPluginRuntime` 保留，不再挂到路由上；5 个入口没有任何匿名放行开关。
- 迁移接线：PG 用 `sql/33.sql`（`CREATE TABLE IF NOT EXISTS`，幂等）；MySQL 文件已加入 `sql/mysql_embed.go`，提供 `MySQLPluginRuntimeIdentityMigrationNames()`，由 T1-0 统一的迁移执行器接入。
