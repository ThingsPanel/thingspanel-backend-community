# PH1-S1 统一版本重建默认栈并验收核心路径 — 交付说明

分支：`claude/wp-ph1-s1-core-c02f8c`（基于 develop `520a3222`）。未推送、未合并。证据：`/Volumes/KIOXIA/archive/evidence/2026-11-05-PH1-S1/`（脚本与输出，无令牌）。

## 结论（一句话）
后端 + Broker + PostgreSQL/Timescale 的核心路径在**同一版本**（后端 `520a3222`）上验证通过：登录、租户隔离、真实 MQTT 上报与历史、命令回执成功、无回执真实超时后显示失败、重启后数据可读。**Encore 镜像没能构建，因此前端设备页（依赖 `/api/v2/*`）无法显示设备，前端层面的验收未完成。**

## 第 0 步：上一任 HA 现场盘点（只读）
`mvp-ha-20261105` 栈（目录 `/Volumes/KIOXIA/tmp/mvp-ha-resume-20261105/stack`）基础设施仍在跑（postgres、redis、mosquitto、broker-owner 健康，tp-agent 在线），但应用层已经停了：约 18:59 起的 `backend-1`（镜像 `thingspanel-go:p1-local`）在 19:04 正常退出（exit 0），之前一直在接收 `/api/v1/plugin/heartbeat`；19:04 起的 `backend-current`（旧镜像 `mvp-run-a29fecc`）因连 `127.0.0.1:5432` 失败（没配 PG 主机）10 次重试后退出；19:06 起的 `backend-p2` 补上了 `GOTP_RELATIONAL_*` 变量，但在“创建 Adapter 的 MQTT 客户端”时 context deadline exceeded，FATAL 退出。此后没有任何进展。证据目录下 11-05 没有 HA 相关证据（只有 MVP-RUN、P3）。结论：HA 端到端**没有做到设备上行/下行这一步**，最远只到“插件心跳打到后端”；卡点是后端换镜像后配置不全、以及新 Broker 下后端 MQTT 客户端连不上。该栈本卡未动。

## 版本表
| 组件 | 镜像 | 镜像 ID | 源码 | 构建方式 |
|---|---|---|---|---|
| 后端 | `thingspanel-go:ph1-520a3222` | `sha256:86eb835e…` | 后端 develop `520a3222` | 宿主机交叉编译 linux/arm64（CGO=0，ldflags 版本 `ph1-520a3222`），叠加到 `thingspanel-go:v1.3.0` 基础层，替换 `sql/`、`encore-api/` 迁移等整棵源码树（`33.sql`、broker 001–017 均在镜像内） |
| Broker | `thingspanel-mosquitto:ph1-520a3222`（= 本机 `t7b-local`，同一 ID） | `sha256:b459e470…` | 构建自 `563e2497` | **未重建**：`563e2497..520a3222` 之间 broker 相关路径只改了 `internal/brokercommands/store.go`（及其测试），`go list -deps` 确认 `tp-broker-owner` 不依赖该包，所以复用。Docker 内重建因网络（见下）失败 |
| 前端 | `thingspanel-vue:ph1-1da66eca` | `sha256:940a22b6…` | 前端 develop `1da66eca`（干净 worktree `/Volumes/KIOXIA/tmp/ph1-s1-work/frontend`） | 宿主机 `pnpm build` → dist，叠加到 `thingspanel-vue:v1.3.0` |
| Encore | **无** | — | 后端 develop `encore-api/` | **构建阻塞**，见下 |
| 数据库 | installer 默认 `timescaledb:14`：PostgreSQL 14.5 + TimescaleDB 2.8.1；Redis 6.2.7 | | | |
| 安装器 | develop `4a8f286` | | | 复制到 `/Volumes/KIOXIA/tmp/ph1-s1-work/ph1-s1`（项目名 `ph1-s1`） |

登录后前端页脚显示版本 `ph1-520a3222`，`/api/v1/sys_version` 同样返回它。

### 构建遇到的问题
- Docker 内构建后端/Broker：下载 Go 依赖 TLS 超时；前端 pnpm 在并行时 ECONNRESET。改为串行 + 宿主机编译 + 复用本地模块缓存后成功（总监指示的方案）。
- **Encore**：`encore build docker`（带/不带 `--config`、`--base postgres:16`、`--services`，共 4 次，最长 21 分钟）都是 CPU 100%、无输出、无子进程，`sample` 显示在做文件路径遍历/GC，不是网络问题。`encore check` 可以正常编译并启动（只是 northbound 等服务因没有本机 DB 报错超时）。未找到根因，按总监要求记为阻塞，未超出额外尝试。

## 第 2 步：栈
`/Volumes/KIOXIA/tmp/ph1-s1-work/ph1-s1`，`docker compose -p ph1-s1`，端口全部 127.0.0.1：前端 31080、后端 31999、PG 31432、MQTT 31883、Redis 31379（modbus/http_adapter/thingsvis 的端口已改写但**未启动**；Encore 相关三个服务**未启动**；frontend 的 depends_on 去掉了 Encore/thingsvis）。凭据在 `/Volumes/KIOXIA/tmp/ph1-s1-work/secrets/`（0700/0600）。

迁移：`prepare.py --bootstrap` 自动执行成功（`sys_version=25`，`installer_migrations`：`community-ext` 到 33、`broker` 到 17，device_metadata/automation/notification 库已建并迁移）。

**安装器 develop 的新装 bug**：`prepare.py --bootstrap` 在全新目录直接失败：`Cannot confirm the supported PostgreSQL backend configuration`。原因是 `validate_supported_backend_database` 在“未插值”模式下把 `GOTP_RELATIONAL_POSTGRES_PLATFORM_PASSWORD=${TP_DATABASE_PASSWORD:-…}` 里的 `$` 当成非法。我没有改安装器；绕过办法是先单独调用安装器自己的 `configure()` 生成 `.env`，再跑 `--bootstrap`（见 `prepare-configure-only.log`）。需要安装器那条线修。另外 `prepare.py --verify` 硬编码 19091/503 端口，在改端口的隔离栈上不能用，未使用。

健康状态：backend、broker-owner、postgres、redis 为 healthy；mosquitto/frontend 无 healthcheck，处于 Up。

## 第 3 步：逐项结果
| # | 验收项 | 结果 | 证据 |
|---|---|---|---|
| 1 | 版本和健康 | 通过（Encore 除外）：后端、Broker、PG、Redis healthy；版本一致 | `compose-up.log`、版本表 |
| 2a | 登录 | 通过：API 登录三个角色；前端页面用租户 A 登录进入首页 | `flow1_setup.out`，浏览器截图（未存仓库） |
| 2b | 租户隔离 | 通过：租户 B 对 A 的设备详情/更新/当前遥测均返回 `201001` 无数据，发命令和删除被拒；之后 A 的设备数据未变；命令表里只有租户 A 的行 | `flow2_isolation.out` |
| 2c | 插件入口 | 通过：匿名和普通租户令牌访问 `/api/v1/plugin/heartbeat`、`/plugin/device/config` 均 503 `managed plugin instance authorization is required` | 同上 |
| 3a | 真实 MQTT 上报 | 通过：设备用自己的 voucher 凭据，paho 连 31883，QoS1 发 `devices/telemetry`；API 当前值 temp/humidity/online 正确 | `flow3_telemetry.out` |
| 3b | 历史与 Timescale | 通过：历史 API（时间参数单位是**秒**，用毫秒会返回空）读到 4 条 temp；直接查 `telemetry_datas`（hypertable，1 个 chunk）与 `telemetry_current_datas` 完全一致；设备 B 无数据 | `flow3_sql.out` |
| 3c | 命令有回执 → 成功 | 通过：设备（订阅 `devices/command/<编号>/+`）真实收到命令，回 `devices/command/response/<id>` 后状态 `device_reported_success`，列表状态 `3` | `flow4_command.out` |
| 3d | 命令无回执 → 失败 | 通过：设备收到命令但不回；状态 `broker_acknowledged` → 约 80 秒内（真实 sweep 周期，未回拨时间）变 `failed`，`error_message=device acknowledgement timed out`；`command_set_logs` 状态 `2`，带“no device response received”。未出现成功 | `flow4_command.out` |
| 4 | 重启后可读 | 通过：重启 backend、broker-owner、mosquitto、frontend 后设备数 2、current、telemetry 12 行、命令状态（成功/失败）都不变；租户 A 重新登录成功；设备重连后重新上报成功 | `flow5_restart_*.out` |
| — | 前端显示当前值/历史/命令失败原因 | **未验证**：前端设备管理/详情页请求 `/api/v2/devices*`、`device-group-tree` 等，由 Encore 提供，本栈没有 Encore，全部 404（页面显示 “Device service request failed (404)”） | 浏览器网络记录 |
| — | HA 接入 | 本卡不含（下一张卡） | — |

## 发现的问题（未修复，按范围写入 TODO）
1. **Broker 拒绝平台发布时状态仍是 `broker_acknowledged`**：新建设备后若没跑 `prepare.py --sync-scope`，Broker 对平台的 `devices/command/…` 发布返回 PUBACK rc135（Denied），后端却记 `broker_acknowledged`；约 80 秒后才以“设备应答超时”失败。此时若有任何人往 `devices/command/response/<id>` 回包，状态会变成 `device_reported_success`（我第一次运行脚本就是这样得到“成功”但设备其实没收到命令，已用同步范围后重跑，第一次输出保留为 `flow4_command.run1-before-scope-sync.out`）。这属于 T7b 发现 4 的同类“误报”，最终不会显示成功，但中间状态和失败原因有误导。建议另开卡：PUBACK 非成功码应直接标 `failed`。
2. **新建设备后必须同步 Broker 范围**（`--sync-scope` 会重启 broker-owner 和 mosquitto），否则平台下发命令被拒。这是安装器已知设计，但第一阶段“接入设备”体验里需要有人/watcher 触发。
3. 安装器新装 `$` 校验 bug、`--verify` 端口硬编码（见上）。
4. Encore 构建挂起；前端依赖 Encore 才能显示设备。
5. 前端设备详情的 URL 参数是 `d_id`，不是 `id`。

## 没做 / 没验证
- Encore 镜像、Encore 与后端的数据一致性、前端设备页读数/历史/命令失败文案。
- 一次完整的“全部卸载重装”和升级；modbus/http_adapter/thingsvis 未启动。
- Broker 镜像未重建（理由见版本表）。
- 本验收是软件设备（paho）+ 本机 Broker，不代表物理设备或客户网络。

## 修了什么
未改产品代码，没有需要的最小修复（命令误报问题影响最终状态之外的中间状态，已记待办）。仅新增本说明、`docs/WORKSPACES.md` 与 `docs/TODO.md` 条目。

## 栈保留
`ph1-s1` 栈保持运行，下一张卡（HA）可接着用。注意：加新设备后需 `python3 config/native/prepare.py --sync-scope`（在栈目录执行）。mosquitto 曾临时挂载 debug 配置，已恢复。
