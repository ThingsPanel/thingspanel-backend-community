# 细粒度工作包（2026-11-03）

这是 `MYSQL-MVP-WORK-PLAN-2026-11-03.md` 第五节的进一步拆分：每个包一个人、一个分支、一个验收口径，互相尽量不碰同一批文件。
通用规则、环境命令、踩坑清单见 `MYSQL-MVP-WORK-PLAN-2026-11-03.md` 第八节；提示语见 `DISPATCH-PROMPTS-2026-11-03.md`。
## 用户决定（2026-11-03，最新）
1. **E1 已授权**：可以重建 18080 本地栈缺失的 postgres 与 backend 容器（产品后端镜像 bb60421f）。仍遵守：只动这套隔离栈（compose 项目 `tp-develop-local`），先确认数据卷/数据来源，不动别的栈。
2. **broker 控制面：暂时不支持 MySQL，先不做**（包 B1–B5 暂缓，不要认领）。
   - 它是什么：负责管理 MQTT broker 的那一层——设备凭据校验、能订阅/发布哪些主题、路由与映射快照、上报消息的入账（幂等账本）、命令下发租约。代码在 `internal/brokercontrol`、`brokerstate`、`brokerledger`、`brokercommands`、`brokerroute`、`brokermapper`，表在 `sql/broker/001-017`，现在只用 PostgreSQL。
   - 后果（必须如实写进验收和联合报告）：**MySQL+TDengine 档位里，broker 控制面仍依赖 PostgreSQL**，所以交接说明要求的“MySQL+TDengine 完整 profile 在 PostgreSQL 不可用时通过”**无法满足**，这个组合最多做到“平台与 Encore owner 在 MySQL、broker 控制面仍用 PG”。其余两种（PG+Timescale、PG+TDengine）不受影响。
   - 目前 MySQL 平台上的遥测上报走的是平台订阅 `devices/telemetry` 的旧通道（已验证），不经过 broker 控制面。
3. **Windows 包暂不构建**（包 W1、W2 暂缓，不要认领，也不需要 HTTPS 分发地址）。

分支命名 `wp/<编号>-<短名>`，从 `develop` 切，**不要推送、不要碰 main、不要发布生产**。


## devicemetadata

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| D1 | thing model store 的 RETURNING 改写（15 处） | - | `encore-api/devicemetadata/store.go` | 新增 store_mysql_contract_test.go：同一剧本在 PG 与 MySQL 上逐步一致（创建/更新/重名冲突/软删/列 |
| D2 | 分组读写删（group_*.go）的拒绝语句与递归查询 | - | `group_read.go group_write.go group_update.go group_delete.go group_members*.go` | 分组 CRUD、移动、成员、级联删除在两库上逐步一致，含环与深度上限场景 |
| D3 | 设备读写（native_device_*.go） | - | `native_device_write.go native_device_read.go native_device_operation.go native_device_stat` | 设备创建/改号/删除/模板绑定/统计在两库上一致 |
| D4 | 凭据与访问模板（native_credential_*、access_template_store、native_template_*） | - | `native_credential_write.go native_credential_secret_write.go access_template_store.go nati` | 凭据生命周期（建/激活/撤销/撤销后不可逆）和模板列表搜索在两库上一致 |
| D5 | 读数导出、命令执行、运行时回执、元数据写事务 | - | `native_reading_export.go native_reading_export_async.go native_command_execution.go native` | 导出分块、命令幂等、回执去重在两库上一致，含并发领取 |
| D6 | 网关树触发器移植 | - | `encore-api/devicemetadata/mysqlschema/002_guards.sql 的生成脚本 scripts/pg2mysql_metadata_guard` | PG 对 MySQL 场景：自环、成环、深度 33、并发加边、设备改种类/删除导致的成员失效，结果一致 |
| D7 | 模板模型选择触发器 + 种子数据 | - | `同上` | 模板写入后 thing_model_* 两列在两库一致；种子逐行一致 |
| D8 | 11 个未转换 CHECK 的替代 | - | `生成器报告里 'not converted' 的 CHECK 清单` | 每个被丢弃的 CHECK 都有等价守卫和一个正反例测试 |
| D9 | 行级安全替代与跨租户测试 | D1,D2,D3 | `encore-api/devicemetadata/*.go（beginTenantScopedSQLTx 及所有 tx.* 查询）` | 跨租户读/写/删在 MySQL 上全部拒绝或不可见，测试覆盖每个 store |
| D10 | MySQL 档位的服务启动与端到端冒烟 | D1,D2,D3,D4,D5 | `encore-api/devicemetadata/db.go（configureDatabaseFromEnv 已有）` | MySQL 栈上设备管理页能列出设备（配合前端 G3） |

## 其他 Encore owner

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| O1 | thingmodel MySQL 化 | D 的 dbx/生成器经验 | `encore-api/thingmodel` | 同 D1 的验收口径 |
| O2 | northbound MySQL 化 | - | `encore-api/northbound` | 两库差分测试通过 |
| O3 | l3-core 通知 owner（上半：模型与存储） | - | `encore-api/l3-core（通知组、路由、outbox 相关）` | 通知组/路由/outbox 两库差分一致 |
| O4 | l3-core 通知 owner（下半：投递与 worker） | O3 | `encore-api/l3-core（投递、租约、重试）` | 并发领取、重试、幂等在两库上一致 |
| O5 | fileassets MySQL 化 | - | `encore-api/fileassets` | 上传恢复、去重在两库上一致 |

## 平台后端

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| C1 | gorm 带 Where 的 OnConflict 与 17 处 DoNothing 读行数 | - | `internal/dal internal/service internal/repo（grep OnConflict）` | 每处有双库差分测试；grep 清单归零或有书面理由 |
| C2 | cmd/* 运维工具的 MySQL 范围 | - | `cmd/` | 清单文档 + 拒绝逻辑的单测 |
| C3 | casbin 基础策略导出 | - | `initialize/casbin_init.go sql/25.sql` | 新装 MySQL 栈的租户用户菜单与 PG 参照安装一致 |
| C4 | 下行/属性/事件上行通道在 MySQL 上的冒烟 | - | `internal/adapter/mqttadapter internal/uplink internal/service` | 每条通道有冒烟脚本和双库对比 |
| C5 | TDengine 作为历史后端 + MySQL 平台整链 | - | `internal/tdenginehistory internal/app/history_backend.go` | MySQL+TDengine 在没有 PG 时整链通过，附日志 |
| C6 | 平台其余 PG 专用 SQL 清扫 | - | `internal/dal/telemetry_datas.go notification_*.go app_push_devices.go sys_user.go` | go test -p 1 ./internal/... 双库全绿 |

## broker 控制面（暂缓，不要认领）

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| B1 | brokercontrol | 决定 | `internal/brokercontrol sql/broker/001-017` | 路由成员、变更事件、屏障在两库上一致 |
| B2 | brokerstate | 决定 | `internal/brokerstate` | 连接观测、委托路由证明在两库上一致 |
| B3 | brokerledger + brokercommands + brokerroute + brokermapper | 决定 | `internal/brokerledger internal/brokercommands internal/brokerroute internal/brokermapper` | 重复上报只算一次、命令租约、快照不可变在两库上一致 |
| B4 | 入口账本写入：telemetry_broker + uplink/reliable + broker_automation | 决定 | `internal/storage/telemetry_broker.go internal/uplink/reliable.go internal/service/broker_a` | 提交未知、重放、乱序场景在两库上一致 |
| B5 | mosquitto 驱动与启动 | B1 | `internal/mosquittodriver cmd/tp-mosquitto-plugin cmd/tp-broker-*` | MySQL+TDengine 栈用真实 broker 插件完成设备接入 |

## 安装器

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| I1 | PG+Timescale profile | - | `/Users/junhong/Downloads/code/thingspanel-installer` | 干净环境新装→登录→设备上报，附日志；升级保留数据 |
| I2 | PG+TDengine profile | I1 | `同上` | 同 I1 + 历史走 TDengine |
| I3 | MySQL+TDengine profile（无 PG） | A 组完成 MVP-1 | `同上` | 在没有任何 PG 容器的机器上装起来并通过冒烟 |
| I4 | 升级、中断、搬迁恢复测试 | I1,I2,I3 | `同上 tests/` | 每种 profile 三类场景的测试记录 |

## 验收台账

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| V1 | 35 包 / 100 API 卡 / 357 路由台账 | - | `证据目录 team-a/ team-b-*/ test-specs/` | 台账文件 + 校验脚本统计数字与要求一致 |
| V2 | J-G0：真实插件安装 | 18080 栈恢复 或 新集成栈 | `见 HANDOFF` | 附录像/日志与制品哈希 |
| V3 | J-G1：设备上报 | V2 | `同上` | 同上 |
| V4 | J-G2：故障恢复与旧实例围栏 | V3 | `同上` | 每个故障一条记录 |
| V5 | J-G3：三种 profile 矩阵 | I1,I2,I3,C5 | `同上` | 矩阵表 + 每格证据 |
| V6 | J-G4：唯一 installer 新装/升级/恢复 | I4 | `同上` | 同上 |
| V7 | 甲乙队独立报告与联合验收报告 | V1-V6 | `证据目录` | 报告 + 遗留清单 |

## 前端

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| F1 | 乙队前端工作在 develop 上重做 | - | `/Users/junhong/Downloads/code/thingspanel-frontend-community（develop=1da66eca，用 worktree，别` | pnpm typecheck && pnpm build 通过；每个功能有截图 |
| F2 | MySQL 栈上的界面冒烟（设备/分组/物模型/模板） | D10,F1 | `前端 worktree + 后端 MySQL 栈` | 每页截图/结论表 |

## Windows 包

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| W1 | 构建 Windows 包 | 暂缓：用户决定暂不构建 | `installer 仓库` | 地址 + 哈希 |
| W2 | Windows 安装验证 | W1 | `干净 Windows` | 验证记录 |

## 环境

| 编号 | 任务 | 依赖 | 位置 | 验收 |
|---|---|---|---|---|
| E1 | 重建 18080 本地栈（需用户授权）（已授权（2026-11-03）） | 授权 | `/Volumes/80G/tp-develop-integration/fixture-bb60421f-8ea8e1ff` | 用授权账号登录成功，界面可用 |
| E2 | 磁盘清理（见 DISK-CLEANUP-PLAN） | - | `本机` | 系统卷可用空间 ≥ 30 GiB，且证据与源码无损 |

## 各包详细说明

### D1 thing model store 的 RETURNING 改写（15 处）
- **已有起点**：分支 `wip/d1-dbx-returning`（在 `/Users/junhong/Downloads/code/thingspanel-history-store-20261103` 仓库里，提交 `b65688b8`）：在 `pkg/dbx` 里让 MySQL 的 `QueryRow` 对单行 INSERT/UPDATE ... RETURNING 先写、再按 id（和 tenant_id）读回，调用点不用改。**MySQL 路径目前返回错误，错误文本没抓到**，对比测试 `TestReturningBehavesTheSameOnPostgresAndMySQL` 是红的；先打印错误再修，或推翻重做。
- 分组：devicemetadata；依赖：-
- 位置：encore-api/devicemetadata/store.go
- 做什么：把 MYSQL-REFUSED-STATEMENTS.txt 里 store.go 的 RETURNING 语句改成 MySQL 可执行形式：dbx.DB 的 Dialect()=="mysql" 分支里先写后读（同事务内 SELECT 回读），或抽出一个 `insertReturning` 辅助函数；PostgreSQL 分支保持原样。
- 验收：新增 store_mysql_contract_test.go：同一剧本在 PG 与 MySQL 上逐步一致（创建/更新/重名冲突/软删/列表分页）；encore test ./devicemetadata/... 全绿

### D2 分组读写删（group_*.go）的拒绝语句与递归查询
- 分组：devicemetadata；依赖：-
- 位置：group_read.go group_write.go group_update.go group_delete.go group_members*.go
- 做什么：处理清单里 group_* 的 RETURNING、`ARRAY[id]`/`= ANY` 递归路径查询（MySQL 用 CONCAT 路径字符串或 JSON_ARRAY 做环检测）；保持 PG 行为不变。
- 验收：分组 CRUD、移动、成员、级联删除在两库上逐步一致，含环与深度上限场景

### D3 设备读写（native_device_*.go）
- 分组：devicemetadata；依赖：-
- 位置：native_device_write.go native_device_read.go native_device_operation.go native_device_statistics.go native_device_number_check.go native_device_initial_groups.go native_device_template_binding_write.go
- 做什么：改写清单中这些文件的 RETURNING、`$n::interval`、`jsonb_*` 语句。设备编号大小写/空白规则要与 PG 一致（PG 靠 CHECK，MySQL 暂无，见 D8）。
- 验收：设备创建/改号/删除/模板绑定/统计在两库上一致

### D4 凭据与访问模板（native_credential_*、access_template_store、native_template_*）
- 分组：devicemetadata；依赖：-
- 位置：native_credential_write.go native_credential_secret_write.go access_template_store.go native_template_list.go native_template_delete.go native_template_record.go
- 做什么：改写清单中的 RETURNING 与 ILIKE 复合表达式；凭据撤销不可逆、身份不可改由触发器守护（已生成），测试里要覆盖。
- 验收：凭据生命周期（建/激活/撤销/撤销后不可逆）和模板列表搜索在两库上一致

### D5 读数导出、命令执行、运行时回执、元数据写事务
- 分组：devicemetadata；依赖：-
- 位置：native_reading_export.go native_reading_export_async.go native_command_execution.go native_runtime_receipt.go metadata_write_tx.go native_device_simulator.go
- 做什么：改写清单里的语句，特别注意异步导出的分块、租约领取（FOR UPDATE SKIP LOCKED 形式，参考 automation/store/mysql_support.go 的 lockIDs/placeholders）。
- 验收：导出分块、命令幂等、回执去重在两库上一致，含并发领取

### D6 网关树触发器移植
- 分组：devicemetadata；依赖：-
- 位置：encore-api/devicemetadata/mysqlschema/002_guards.sql 的生成脚本 scripts/pg2mysql_metadata_guards.py
- 做什么：移植 validate_gateway_membership_tree（环、深度 32、自引用、端点必须是活的 gateway/subdevice，租户级 tp_advisory_locks 行锁）和延迟约束 enforce_gateway_memberships_after_device_state_change（MySQL 无延迟触发器：改为在改设备状态的写事务里应用层检查，或同一触发器里直接检查）。
- 验收：PG 对 MySQL 场景：自环、成环、深度 33、并发加边、设备改种类/删除导致的成员失效，结果一致

### D7 模板模型选择触发器 + 种子数据
- 分组：devicemetadata；依赖：-
- 位置：同上
- 做什么：移植 resolve_current_template_model_selection（BEFORE INSERT/UPDATE：从 configuration 解析并填 thing_model_id / thing_model_snapshot_id）；导出迁移 0002_seed_meta_item_keys 的种子到 MySQL（参考 scripts/pg2mysql_seed.py）。
- 验收：模板写入后 thing_model_* 两列在两库一致；种子逐行一致

### D8 11 个未转换 CHECK 的替代
- 分组：devicemetadata；依赖：-
- 位置：生成器报告里 'not converted' 的 CHECK 清单
- 做什么：设备标识校验函数（device_identity_*_valid、编号/子设备地址首尾空白规则、access_binding_parameters_valid、operation problem/result shape）：用触发器 SIGNAL 3819，或在 Go 写入前校验并加单测。
- 验收：每个被丢弃的 CHECK 都有等价守卫和一个正反例测试

### D9 行级安全替代与跨租户测试
- 分组：devicemetadata；依赖：D1,D2,D3
- 位置：encore-api/devicemetadata/*.go（beginTenantScopedSQLTx 及所有 tx.* 查询）
- 做什么：PG 靠 app.current_tenant_id 的行级安全兜底；MySQL 没有。逐条检查每个读写语句都带 tenant_id 条件，缺的补上；参考 cross_tenant_test.go 为 MySQL 写等价测试。
- 验收：跨租户读/写/删在 MySQL 上全部拒绝或不可见，测试覆盖每个 store

### D10 MySQL 档位的服务启动与端到端冒烟
- 分组：devicemetadata；依赖：D1,D2,D3,D4,D5
- 位置：encore-api/devicemetadata/db.go（configureDatabaseFromEnv 已有）
- 做什么：TP_DEVICE_METADATA_DB_DRIVER=mysql + TP_DEVICE_METADATA_MYSQL_DSN 下用 `encore run` 起服务，验证没有任何 PostgreSQL 时服务能启动（sqldb.NewDatabase 在包级声明，需实测 Encore 不连 PG 能否起来；不行就记录并提出方案）；对 /api/thing-models、/api/devices 做冒烟。
- 验收：MySQL 栈上设备管理页能列出设备（配合前端 G3）

### O1 thingmodel MySQL 化
- 分组：其他 Encore owner；依赖：D 的 dbx/生成器经验
- 位置：encore-api/thingmodel
- 做什么：照 devicemetadata 的做法：sqldb→dbx、生成 MySQL schema（scripts/pg2mysql_platform.py --standalone）、翻译器拒绝清单、差分测试。
- 验收：同 D1 的验收口径

### O2 northbound MySQL 化
- 分组：其他 Encore owner；依赖：-
- 位置：encore-api/northbound
- 做什么：同上（15 文件、59 条 SQL，1 个迁移）。
- 验收：两库差分测试通过

### O3 l3-core 通知 owner（上半：模型与存储）
- 分组：其他 Encore owner；依赖：-
- 位置：encore-api/l3-core（通知组、路由、outbox 相关）
- 做什么：66 文件、165 条 SQL 的前半：先测翻译清单，改写拒绝语句，补 MySQL schema。
- 验收：通知组/路由/outbox 两库差分一致

### O4 l3-core 通知 owner（下半：投递与 worker）
- 分组：其他 Encore owner；依赖：O3
- 位置：encore-api/l3-core（投递、租约、重试）
- 做什么：后半：领取/租约/重试，注意 SKIP LOCKED 形式与并发测试。
- 验收：并发领取、重试、幂等在两库上一致

### O5 fileassets MySQL 化
- 分组：其他 Encore owner；依赖：-
- 位置：encore-api/fileassets
- 做什么：注意该包有 Encore 生成的符号（RecoverInterruptedUploads），只能用 encore test/check 编译。
- 验收：上传恢复、去重在两库上一致

### C1 gorm 带 Where 的 OnConflict 与 17 处 DoNothing 读行数
- 分组：平台后端；依赖：-
- 位置：internal/dal internal/service internal/repo（grep OnConflict）
- 做什么：带 Where 的条件 upsert 在 MySQL 不可用：逐处改成先 UPDATE 再 INSERT 或应用层判断；读受影响行数的 DoNothing 改用 pkg/dbdialect.InsertIfAbsent。
- 验收：每处有双库差分测试；grep 清单归零或有书面理由

### C2 cmd/* 运维工具的 MySQL 范围
- 分组：平台后端；依赖：-
- 位置：cmd/
- 做什么：列出每个工具是否需要支持 MySQL（审计、迁移、broker 工具），不需要的在 README 声明仅 PG 并在启动时拒绝 MySQL 配置。
- 验收：清单文档 + 拒绝逻辑的单测

### C3 casbin 基础策略导出
- 分组：平台后端；依赖：-
- 位置：initialize/casbin_init.go sql/25.sql
- 做什么：找到完整基础策略的来源（参考安装或安装器数据），导出成 sql/mysql/015_casbin_policy.sql（可重复执行）；写测试：租户用户能看到应有菜单和接口。
- 验收：新装 MySQL 栈的租户用户菜单与 PG 参照安装一致

### C4 下行/属性/事件上行通道在 MySQL 上的冒烟
- 分组：平台后端；依赖：-
- 位置：internal/adapter/mqttadapter internal/uplink internal/service
- 做什么：参考 scripts/smoke_mvp1_ingest.sh，扩展到属性上报、事件、命令下发与回执、OTA；记录翻译器拒绝的语句并改。
- 验收：每条通道有冒烟脚本和双库对比

### C5 TDengine 作为历史后端 + MySQL 平台整链
- 分组：平台后端；依赖：-
- 位置：internal/tdenginehistory internal/app/history_backend.go
- 做什么：config: db.driver=mysql + storage.history_backend=tdengine（需 experimental=true）。起 TDengine 3.4 容器，跑上报→历史读取→统计→导出→删设备清理；PG 完全不存在。
- 验收：MySQL+TDengine 在没有 PG 时整链通过，附日志

### C6 平台其余 PG 专用 SQL 清扫
- 分组：平台后端；依赖：-
- 位置：internal/dal/telemetry_datas.go notification_*.go app_push_devices.go sys_user.go
- 做什么：unnest/to_regclass/咨询锁已由翻译器兜底的位置补双库测试；telemetry_datas 批量 upsert 与 MySQL 兜底历史表对齐。
- 验收：go test -p 1 ./internal/... 双库全绿

### B1 brokercontrol
- 分组：broker 控制面（暂缓，不要认领）；依赖：决定
- 位置：internal/brokercontrol sql/broker/001-017
- 做什么：约 71 处 PG 专用 SQL（?::uuid、?::jsonb、UPDATE ... FROM、咨询锁）。MySQL DDL 用生成器从 PG 终态生成，复杂语句写 MySQL 分支。
- 验收：路由成员、变更事件、屏障在两库上一致

### B2 brokerstate
- 分组：broker 控制面（暂缓，不要认领）；依赖：决定
- 位置：internal/brokerstate
- 做什么：约 46 处，核心是 to_jsonb(o)->'delegated_route' 与连接观测；MySQL 用 JSON_OBJECT/列直读重写。
- 验收：连接观测、委托路由证明在两库上一致

### B3 brokerledger + brokercommands + brokerroute + brokermapper
- 分组：broker 控制面（暂缓，不要认领）；依赖：决定
- 位置：internal/brokerledger internal/brokercommands internal/brokerroute internal/brokermapper
- 做什么：账本、隔离区、命令、映射快照；SKIP LOCKED 与咨询锁已有翻译器支持，补并发测试。
- 验收：重复上报只算一次、命令租约、快照不可变在两库上一致

### B4 入口账本写入：telemetry_broker + uplink/reliable + broker_automation
- 分组：broker 控制面（暂缓，不要认领）；依赖：决定
- 位置：internal/storage/telemetry_broker.go internal/uplink/reliable.go internal/service/broker_automation.go
- 做什么：broker_ingress_receipts / effects 的幂等写入（?::jsonb、ON CONFLICT 多行）。
- 验收：提交未知、重放、乱序场景在两库上一致

### B5 mosquitto 驱动与启动
- 分组：broker 控制面（暂缓，不要认领）；依赖：B1
- 位置：internal/mosquittodriver cmd/tp-mosquitto-plugin cmd/tp-broker-*
- 做什么：插件/驱动对数据库的访问改走 dbx 或 MySQL 配置；不再要求 PG。
- 验收：MySQL+TDengine 栈用真实 broker 插件完成设备接入

### I1 PG+Timescale profile
- 分组：安装器；依赖：-
- 位置：/Users/junhong/Downloads/code/thingspanel-installer
- 做什么：compose 模板、配置生成、新装/升级/中断恢复，保持现有默认路径行为不变。
- 验收：干净环境新装→登录→设备上报，附日志；升级保留数据

### I2 PG+TDengine profile
- 分组：安装器；依赖：I1
- 位置：同上
- 做什么：TDengine 容器、storage.history_backend=tdengine 配置生成、健康检查。
- 验收：同 I1 + 历史走 TDengine

### I3 MySQL+TDengine profile（无 PG）
- 分组：安装器；依赖：A 组完成 MVP-1
- 位置：同上
- 做什么：MySQL 8.4（log_bin_trust_function_creators=1、受限应用账号：无 CREATE TEMPORARY TABLES）、TDengine、Redis、Mosquitto；安装期不得出现任何 PG。**注意：broker 控制面暂缓后仍依赖 PG**，所以这个 profile 目前只能做到“平台与 Encore owner 在 MySQL、broker 控制面仍用 PG”，不能宣称“没有 PG 也能跑”。
- 验收：在没有任何 PG 容器的机器上装起来并通过冒烟

### I4 升级、中断、搬迁恢复测试
- 分组：安装器；依赖：I1,I2,I3
- 位置：同上 tests/
- 做什么：对三种 profile：升级保留数据、中途杀进程可重入、搬迁到新目录后可启动；签名清单校验（已接入）要覆盖失败路径。
- 验收：每种 profile 三类场景的测试记录

### V1 35 包 / 100 API 卡 / 357 路由台账
- 分组：验收台账；依赖：-
- 位置：证据目录 team-a/ team-b-*/ test-specs/
- 做什么：按 HANDOFF 要求建台账（含 89 张未完成的卡），每行有状态、证据路径、对应提交。
- 验收：台账文件 + 校验脚本统计数字与要求一致

### V2 J-G0：真实插件安装
- 分组：验收台账；依赖：18080 栈恢复 或 新集成栈
- 位置：见 HANDOFF
- 做什么：固定六仓版本与制品 SHA256；真实插件安装全过程（含失败与回滚）。
- 验收：附录像/日志与制品哈希

### V3 J-G1：设备上报
- 分组：验收台账；依赖：V2
- 位置：同上
- 做什么：真实设备（或真实 MQTT 客户端）上报→平台入库→界面可见。
- 验收：同上

### V4 J-G2：故障恢复与旧实例围栏
- 分组：验收台账；依赖：V3
- 位置：同上
- 做什么：杀进程、断网、重启后的恢复；旧实例（陈旧租约/代次）不能越权。
- 验收：每个故障一条记录

### V5 J-G3：三种 profile 矩阵
- 分组：验收台账；依赖：I1,I2,I3,C5
- 位置：同上
- 做什么：PG+Timescale、PG+TDengine、MySQL+TDengine（PG 不可达时）各自完整链路和关键故障。MySQL+TDengine 因 broker 控制面暂缓仍依赖 PG，**无法在 PG 不可用时通过**，报告里如实写成已知缺口。MySQL+Timescale 已取消，报告里如实写明范围变更。
- 验收：矩阵表 + 每格证据

### V6 J-G4：唯一 installer 新装/升级/恢复
- 分组：验收台账；依赖：I4
- 位置：同上
- 做什么：用同一份固定制品跑安装器三类场景。
- 验收：同上

### V7 甲乙队独立报告与联合验收报告
- 分组：验收台账；依赖：V1-V6
- 位置：证据目录
- 做什么：各自独立成文，再合并成联合报告；真实遗留清单不得美化。
- 验收：报告 + 遗留清单

### F1 乙队前端工作在 develop 上重做
- 分组：前端；依赖：-
- 位置：/Users/junhong/Downloads/code/thingspanel-frontend-community（develop=1da66eca，用 worktree，别动主目录）
- 做什么：乙队分支与 develop 冲突 80 处，不要硬合；按功能逐个在 develop 上重做。
- 验收：pnpm typecheck && pnpm build 通过；每个功能有截图

### F2 MySQL 栈上的界面冒烟（设备/分组/物模型/模板）
- 分组：前端；依赖：D10,F1
- 位置：前端 worktree + 后端 MySQL 栈
- 做什么：VITE_METADATA_CONNECTOR_URL 指向 MySQL 后端，覆盖设备管理、分组、物模型、模板、看板、个人中心（第三方账号区“操作失败”要定位）。
- 验收：每页截图/结论表

### W1 构建 Windows 包
- 分组：Windows 包；依赖：暂缓：用户决定暂不构建
- 位置：installer 仓库
- 做什么：按安装器产出 Windows 包，给出包地址与 SHA256。
- 验收：地址 + 哈希

### W2 Windows 安装验证
- 分组：Windows 包；依赖：W1
- 位置：干净 Windows
- 做什么：同事在 Windows 上安装、启动、登录、卸载。
- 验收：验证记录

### E1 重建 18080 本地栈（需用户授权）
- 分组：环境；依赖：授权
- 位置：/Volumes/80G/tp-develop-integration/fixture-bb60421f-8ea8e1ff
- 做什么：缺 postgres 与 backend 容器；按 build-provenance.json 重建产品后端镜像 bb60421f，起 postgres，恢复数据卷（先确认数据在哪）。
- 验收：用授权账号登录成功，界面可用
- 需要先得到：已授权（2026-11-03）

### E2 磁盘清理（见 DISK-CLEANUP-PLAN）
- 分组：环境；依赖：-
- 位置：本机
- 做什么：按清理计划分步骤执行，每步先量后删，删除前向用户确认。
- 验收：系统卷可用空间 ≥ 30 GiB，且证据与源码无损

