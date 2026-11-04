# T7b — 用 develop 重建 Broker 镜像，打通 HA 上行和下行

**结论：上行、下行都通过。** 用 develop `563e2497` 本地重建 Broker 镜像后，不再出现 CONNACK 5；HA 的数据进了平台，平台也能控制 HA 的灯。没有改任何代码（构建脚本、Dockerfile、Go 代码、安装器均未改），所以本任务没有代码提交，只有这份说明。未推送、未合并、未发布镜像。

证据（不进仓库）：`/Volumes/500G/archive/evidence/2026-11-04-T7b/`。栈目录 `/Volumes/500G/t7b-stack/`（只剩 compose、安装器快照、测试密钥和二进制；密钥、凭据、HA 配置、卷已删）。

## 1. 镜像构建
- worktree 原先落后 develop 210 个提交（没有自己的提交），已 `git merge --ff-only develop` 到 `563e2497`。
- `docker build --platform linux/arm64 -f deploy/mosquitto-community/Dockerfile.production -t thingspanel-mosquitto:t7b-local .`，一次成功，约 3.5 分钟（`build.log`）。
- 镜像 ID：`sha256:b459e470fd6c9fb5586e1b4f838178e119fb9e626513ed5c18c7f55f58405626`，arm64，179MB，只在本机，含 `tp-broker-owner` 和 `libtp-mosquitto-plugin.so`（Mosquitto 2.1.2 同源头文件编译）。对比：旧 `v1.3.0` 是 152MB、ID `91469e41c992`。

## 2. 栈与安装（对应 T7 的步骤）
栈：timescaledb pg16、redis、develop 后端二进制（复用 T7 的 `ThingsPanel-Go`）、新镜像的 broker-owner + mosquitto（owner 也用镜像里的，不再挂载外部二进制）、HA stable（demo + 虚拟实体）。容器全部 `t7b-` 前缀。
1. 手工执行 `sql/33.sql` 和 `sql/broker/001–017`（后端 ingress_v1 启动前必须有，否则 FATAL `schema missing table …`）。
2. 一条命令安装 HA 插件（安装器快照 `cf242df`，HA 地址 + 环境变量里的 token）：generation 1，`/health` ok（`step1-install.log`）。
3. 平台里手工建服务插件（`sub_topic_prefix`、`http_address`）、接入点，批量建 3 台设备：Bed Light、Outside Temperature、Outside Humidity（`step2-create-devices.log`）。
4. 刷新服务账号设备范围（见 §5），重启 broker-owner → mosquitto → 插件容器。

## 3. 上行：通过
HA 里把 `sensor.outside_temperature` 设为 31.7，再设为 32.4；开灯一次。
- `telemetry_datas` 里有 `ha_state` 历史：31.7、32.4（温度），on/off/on（灯）；`telemetry_current_datas` 为最新值 32.4 / on；接口 `/api/v1/telemetry/datas/current/{id}` 返回当前值（`step3-uplink-db.log`）。
- 没有测历史分页接口（它要求 StartTime，我没带）；历史是直接查库确认的。

## 4. 下行：通过
平台 `POST /api/v1/command/datas/pub`（identify `switch`，value true/false）：
- 开：HA 灯 off → on，`command_set_logs` 状态 3，`rsp_data` = `{"result":0,"message":"success"}`。
- 关：HA 灯 on → off，回执同上（`step4-downlink-off.log`）。

## 5. 根因与发现
1. **镜像过旧是 CONNACK 5 的直接原因（基本确认）**：同样的栈定义，换成 develop 重建的镜像并在建完设备后刷新范围，服务账号和设备凭据都连得上、发得出。**没有做对照实验**去区分「只重建 `.so`」和「范围刷新顺序」各自的贡献，T7 的 CONNACK 5 是两者叠加还是单一原因，没有隔离。
2. **范围刷新前，插件账号被拒**：建设备之前/刷新之前 `tp-community-protocols` 的范围为空，插件命令桥日志里反复 `not authorised`；刷新并重启后立即恢复（`mosquitto.log`）。
3. **新发现：平台账号也要把插件设备放进范围，否则下行被静默丢弃。** 现有 `prepare.py --refresh-devices` 生成的平台服务账号（`tp-community-platform`）只含普通 MQTT 设备（`targets`），不含服务接入点设备；没有普通设备时它的 `publish` 列表是空的。结果：后端下发命令，`broker_command_requests` 状态到 `broker_acknowledged`，平台日志显示成功，但消息从未到达插件，HA 不变、无回执。把插件设备 ID/租户也加进平台账号的 `allowed_device_ids/allowed_tenant_ids`（即 `service(env, targets + plugins)`），重启 owner 和 mosquitto 后下行立即通。这个结论来自一次前后对比（同一栈、同一命令），已确认；用的是 `owner.go` 里 `pluginOwnedRecord` → `addPluginPublishAliases` 那条路径，我没有改任何代码，只改了生成的 `services.json`。
4. 下行失败的表现具有欺骗性：命令 `accepted`、`broker_acknowledged`，没有任何错误。

## 6. 设备范围刷新必须手工做，要自动化（给安装器的需求，本任务未改安装器）
本次实际顺序与命令（`/Volumes/500G/archive/evidence/2026-11-04-T7b/refresh2.py` 是 `prepare.py` 的 `service()`/`refresh()` 复制版，只改了一行）：
1. 平台里建好 HA 设备（服务接入点 + 批量建设备）。
2. 重新生成 `broker/services.json`，其中：
   - 平台账号：`allowed_device_ids/allowed_tenant_ids` = 普通 MQTT 设备 **加上** 服务接入点设备，`publish` 非空；
   - 协议账号：`allowed_device_ids` = 服务接入点设备，`plugin_socket: true`、`allow_any_client_id: true`。
   （官方入口是 `python3 config/native/prepare.py --refresh-devices`，但它的平台账号这一段需要如上修正，见发现 3。）
3. `docker compose restart broker-owner`，然后 `restart mosquitto`（mosquitto 与 owner 共网络命名空间，必须后重启）。
4. 重启 HA 插件容器（插件只在启动时同步设备；T7 已指出，本次也是这样做的，没有单独验证不重启会怎样）。
需求：安装器在 `binding: none` 的服务型插件装好并建设备后，自动做 2–4；`prepare.py refresh` 的平台账号范围要包含插件设备；之后新增设备也要触发同样的刷新。

## 7. 还剩什么
- 安装器：自动登记服务插件、稳定网络别名、设备范围自动刷新（§6）、`sql/33.sql` 与 broker 迁移在安装/升级流程里自动执行。
- 后端：命令被 Broker 丢弃时状态仍显示成功，缺少可见的失败信号（发现 4）；HA token 进入请求日志/`service_access.voucher`（T7 已报）。
- 没做对照实验隔离旧镜像的具体故障点（见 §5.1）；没有在 amd64 上构建；未验证长时间运行、重连和身份续期。
- 本机 `thingspanel-mosquitto:t7b-local` 镜像保留（未发布），后续要不要保留由负责人定。
- 清理：所有 `t7b-` 容器、卷、网络已删，`tp-agent-*` 容器已删，HA 里的长期令牌已撤销（旧令牌访问返回 401，`ha-token-revoked.log`），平台身份已通过安装器卸载吊销。

## 8. 追加：用真实 Home Assistant 验证（192.168.31.97，设备 ESP-01S灯 `switch.esp_01sdeng`）
负责人提供了真实 HA 和开发用长期令牌，并明确授权操作灯。栈重建后（同样的新镜像、同样的流程）：
- 一条命令安装：generation 1，健康检查 ok（`real-step1-install.log`）。
- 建服务插件、接入点（base_url `http://192.168.31.97:8123` + 令牌），插件列出 HA 实体（分页上限 100 条，ESP-01 在其中），选 ESP-01S灯建为设备（`real-step2-create-devices.log`）。
- 刷新设备范围（平台账号含插件设备）并重启 owner、mosquitto、插件。
- **下行通过**：平台下发 `switch` false/true，真实灯 on→off→on，两次回执 `result:0`、状态 3（`real-step4-downlink.log`）。
- **上行通过**：灯的状态变化进入平台，`telemetry_datas` 有 off、on 两条 `ha_state`，当前值为 on（`real-step3-uplink-db.log`）。
- 插件不会在启动时推送当前状态，只在状态变化时上报；刚接入的设备在变化前平台没有数据。
- 与另一会话（t7c）同时在本机 Docker 上跑栈时，我的插件容器曾消失一次，重新 `install`（generation 2）后恢复；原因未查（容器名为 `tp-agent-prepared-<hash>`，两个会话可能互相影响）。
- 清理：t7b 容器、卷、网络、agent 与插件容器已删，平台运行身份已吊销，本地令牌文件已删；HA 里的令牌按负责人意见保留。
