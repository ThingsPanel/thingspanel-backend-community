# ThingsPanel 重启 · 备份 · 恢复 · 升级 规划（草案 v2，2026-11-12）

> **状态**：**已定（2026-11-12，用户同意 v2 并授权派工）**。任务编号 R0–R7，独立于 T0–T8；执行安排见 §10。
> **v2 的变化**：① 插件 Agent 与系统管理**合并成一个宿主侧管理者**（§3）；② 实现语言定为 **Go**，不再用 Python 写 `tpctl`（§3.3）；③ 增加界面设计，参照 HA 的“检查更新 / 重启”两个菜单（§5）；④ P1 留下的监视进程、“改成系统服务”的待排卡并入本计划（§3.2、R2）。
> **依据**：`docs/PROJECT-AUDIT-2026-11-04.md` §1（A1–A5）、§7；安装器 develop 的 `agent/`（tp-agent 源码）、`config/native/update.py`、`config/native/UPDATE-SAFETY.md`、`docker-compose.yml`；后端 `main.go`、`initialize/pg_init.go`、`pkg/relational`；Home Assistant 等系统的公开做法（§2）。

---

## 0. 一句话结论

**重启、备份、恢复、升级不是四个功能，而是三个原语的组合：**

| 原语 | 含义 |
|---|---|
| **快照 Snapshot** | 把“数据库 + 配置 + 文件 + 版本清单”打成一个可校验的包 |
| **切换 Apply** | 按顺序停服务、换配置/镜像、再启动 |
| **验收 Verify** | 健康门：所有组件就绪、库表版本符合预期，否则判失败 |

- 重启 = 配置检查 + Apply（不换版本）+ Verify
- 备份 = Snapshot
- 恢复 = Snapshot（先给当前现场留个底）+ Apply（用备份内容）+ Verify
- 升级 = 强制 Snapshot + Apply（新版本）+ Verify，**失败就用刚才的快照自动恢复**

**谁来做**：宿主上只有一个管理者，就是现有的 **tp-agent**。它现在管插件，以后同时管平台本身，相当于 HA 的 Supervisor。全部用 **Go** 实现。

**顺序**：先做命令行（`tp-agentctl system ...`），再做界面；先做 PG + TimescaleDB 单一发行版，再做其他组合。

---

## 1. 现状：已有什么、缺什么

| 能力 | 已有 | 缺口 |
|---|---|---|
| 宿主侧执行者 | **tp-agent（Go，安装器仓库 `agent/`）**：Unix socket + Bearer token 的控制 API（`/v1/prepare /grant /activate /revoke /retire /status /sweep /catalog /config-*`）、带系统文件锁的持久状态（state + journal）、只走 Docker Engine API 的受限适配器、签名目录校验、密钥库；`tp-agentctl` 命令行 | 只管**插件**容器，不能停启平台组件；不能做备份 |
| 配置文件事务 | 安装器 `update.py`（Python）：内核文件锁、journal、备份旧配置、原子替换、签名 release 校验 | 只管配置文件；进入 `activation_started` 后**不能自动恢复**（原文：“No database rollback was attempted”） |
| P1 监视进程 | 插件设备增删后刷新 Broker 范围，并按顺序重启 broker-owner → mosquitto | 不是系统服务，机器重启后要等下一次 `renew` 才会被拉起（审计文档 §7.8 的已知局限） |
| 数据库迁移 | 平台 `sys_version` + `sql/N.sql`；`pkg/relational` 迁移执行器和锁 | 只能向前，**没有回退**；Encore 各模块的迁移版本没有汇总 |
| 健康检查 | Gin `GET /health`、`/api/device-metadata/healthz`；Agent 的插件 `HealthSpec` | 没有统一的“整栈就绪”判断 |
| 重启 / 备份 / 恢复 / 升级 | 只有 `upgrade.sh` → `update.py`（配置部分是事务性的） | 不能从界面重启；没有备份恢复；升级失败不能自动回退 |

**最关键的一点**：现在的升级“配置可以回退，数据库不行”。补上“升级前自动快照 + 失败后用快照恢复”，`activation_started` 这个“只能等人处理”的状态就能自动恢复。这是整件事里收益最大的一步。

---

## 2. 别人怎么做（取其精华）

### 2.1 Home Assistant（主要参考）

| 做法 | 我们学什么 |
|---|---|
| **Core 和 Supervisor 分开**：Core 是业务程序；Supervisor 是宿主上的管理进程，**同时**管 Core、插件（add-on）、备份和升级 | 被重启的程序不能负责重启自己。**插件管理和系统管理放在同一个管理者里**，这就是 tp-agent 要扩展成的样子（§3） |
| 设置页右上角 ⋮ 菜单有两项：**检查更新**、**重启 Home Assistant** | 我们的界面也做这两个入口（§5） |
| 重启对话框：**快速重载**（不重启进程）/ **重启**；“高级选项”里还有重启主机、关机、安全模式 | 第一期只做“重启控制面 / 全栈重启”，其余放第二期 |
| 重启前先**检查配置**，有错就拒绝重启 | 后端加 `--check-config`（R2） |
| 更新列表把 Core、OS、Supervisor、**各个插件**的更新放在一起；每项升级前都有“**先创建备份**”勾选项 | 我们的“检查更新”也同时列出平台、Agent 和插件；平台升级前的备份是**强制**的（§4.3） |
| 备份是一个 tar，里面有 `backup.json` 元数据；分完整/部分备份；新版本还有自动备份、保留份数、加密密钥（Emergency Kit）、多个存储位置 | 第一期做完整备份 + manifest（§4.1）；其余放第二期（§7） |
| 新版本起不来时，Supervisor 会回退到旧版本 | 升级状态机 `verify → commit / rollback`（§3.4） |

### 2.2 其他系统

| 系统 | 经验 |
|---|---|
| **GitLab** `gitlab-backup` | 备份只能恢复到同一版本；密钥文件**不放进**普通备份，要单独保管。→ `.env`、Agent 的 `secrets.json` 在没有加密之前不进备份包 |
| **ThingsBoard** | 升级脚本要求运维先**手工**备份数据库。手工步骤迟早会被跳过 → 我们把备份做成自动、强制 |
| **Grafana / 大多数 Go 服务** | 启动时自动迁移，不支持降级。→ “回退”只能靠恢复快照，不能靠反向迁移 |
| **TimescaleDB 官方** | 恢复前后要调用 `timescaledb_pre_restore()` / `timescaledb_post_restore()`，而且恢复端的扩展版本必须和备份端一致 → manifest 记录扩展版本，恢复前先校验 |
| **Kubernetes** | readiness 探针不通过，就不算发布成功 → 健康门 |
| **Mender / RAUC（嵌入式 A/B 升级）** | 新版本启动后要“确认提交”，没确认就自动回退 → 状态机里的 commit / rollback |

---

## 3. 统筹：一个宿主侧管理者（tp-agent = 我们的 Supervisor）

### 3.1 决定：扩展 tp-agent，不另建 Supervisor

| | 方案甲：扩展 tp-agent（**推荐**） | 方案乙：另建 tp-supervisor |
|---|---|---|
| 宿主上要装、要监控、要升级的东西 | **1 个** | 2 个，而且两者都要操作 Docker，还要互相协调锁 |
| 可以复用的代码 | 文件锁 + journal 状态内核、Docker Engine 适配器、签名校验、密钥库、socket+token API、`tp-agentctl` | 只能复制一份，或者抽成公共库 |
| 和 HA 的对应关系 | 一致：HA 的 Supervisor 同时管 Core 和插件 | 不一致 |
| “插件升级”和“平台升级”放在同一个“检查更新”里 | 天然就在一起 | 要两边汇总 |
| 风险 | 权限集中在一个进程里 | 权限分散，但协调的复杂度更高 |

**风险怎么控制（方案甲的硬性要求）**：

1. **两个角色，彼此隔离**：`plugin` 角色保持现有的严格策略**一字不改**；新增 `system` 角色，使用**独立的 socket 和 token**。插件相关的调用方拿不到 system 的 token。
2. **只接受固定动作**：system 角色只提供固定的几个动作（重启、备份、恢复、升级、状态查询），**不接受**任意命令、任意镜像或任意参数。镜像版本只能来自已校验签名的 release manifest。
3. **不能自己升级自己**：第一期 Agent 自身的升级仍然由安装器负责（HA 的 Supervisor 可以自我更新，我们放到第二期）。
4. **Agent 不在它所管理的 compose 栈里**：现在已经是这样（`agent_host.py` 单独 `docker run`），所以停整个平台栈时 Agent 不会把自己停掉。

> 这需要**修改审计文档的 A4**：把“Agent 只负责拉包、验包、启停插件容器”改成“Agent 是宿主侧唯一的管理者，分插件和系统两个隔离的角色”。请负责人确认。

### 3.2 合并后的职责

| 角色 | 职责 | 来源 |
|---|---|---|
| `plugin` | 插件的准备、授权、激活、吊销、清理；签名目录；插件密钥 | 现有，不改 |
| `plugin`（并入） | 插件设备增删后刷新 Broker 范围，并按顺序重启 broker-owner → mosquitto | **P1 监视进程搬进来**，变成 Agent 的周期任务 |
| `system`（新增） | 检查配置、重启、整栈健康门、备份、恢复、平台升级 + 自动回退；写维护页的 `status.json` | 本计划 |
| 进程托管 | Agent 本身作为系统服务运行（Docker `restart: unless-stopped`，或 systemd/launchd），机器重启后自动拉起 | 原来那张“监视进程改成系统服务”的待排卡，合并进 R2 |

### 3.3 决定：全部用 Go

**结论：用 Go，而且从第一期就用 Go，不先写一版 Python 再重写。** 理由：

1. **tp-agent 已经是 Go**。锁、journal、Docker 适配器、签名校验都已经在 Go 里，而且有真实 Docker 测试。新功能放进同一个程序，是最省事的做法。
2. **整个系统都是 Go**，团队只需要维护一种语言；单个静态二进制，放进容器或宿主都不需要额外的 Python 环境。
3. Agent 在容器里运行，**容器里没有 Python**。如果用 Python 写 `tpctl`，界面触发时还得另想办法去调用它，反而更绕。

**那 Python 的 `update.py` 怎么办**（避免一步到位、全部推倒重来）：

| 阶段 | `update.py`（Python） | tp-agent `system`（Go） |
|---|---|---|
| 现在 | 负责安装和升级 | 不存在 |
| 第一期（R1–R6） | **只负责**：首次安装、安装或升级 Agent 本身、兜底的“仅恢复配置” | 负责重启、备份、恢复、平台升级。配置文件事务按 `update.py` 的语义**移植到 Go**（R5a） |
| 过渡期规则 | 两边使用**同一个锁文件**（`.thingspanel-update` 下的内核锁），保证不会同时执行 | 同左 |
| 第二期 | `upgrade.sh` 只是一层外壳：检测到 Agent 就调用 `tp-agentctl system upgrade` | Agent 可以自我更新 |

### 3.4 统一状态机（所有系统操作共用，复用 Agent 的状态内核）

```
idle ─▶ preflight ─▶ snapshot ─▶ maintenance_on ─▶ apply ─▶ start ─▶ verify ─┬─▶ commit ─▶ idle
          │ 失败                                                              │ 失败
          ▼                                                                   ▼
        aborted（什么都没改）                                    rollback（用 snapshot 恢复）─▶ verify
                                                                              │ 再次失败
                                                                              ▼
                                                                        needs_human（保留现场）
```

| 操作 | preflight | snapshot | apply | 失败时 |
|---|---|---|---|---|
| 重启 | 检查配置 | —（不改数据，不需要） | 按顺序重启 | 报告哪个组件没起来，不自动回退 |
| 备份 | 磁盘空间 | **产出物本身** | — | 删除不完整的包 |
| 恢复 | 校验备份包、版本兼容 | 当前现场的安全快照 | 导入备份 | 用安全快照恢复 |
| 升级 | 签名、升级路径、磁盘空间、**提前拉取镜像** | **强制** | 换配置和镜像，执行迁移 | 用升级前快照恢复，并换回旧镜像 |

journal 增加的阶段：`snapshot_done`（附快照路径和校验值）、`verify_failed`、`rolling_back`、`rolled_back`、`needs_human`。同一时刻只允许一个系统操作；插件操作和系统操作共用 Agent 的状态锁，所以**升级平台时不会同时在装插件**。

### 3.5 总体架构图

```
 浏览器 ──▶ 前端 nginx ──────────▶ Encore「system」API（超级管理员；只记录请求、显示状态）
              │ 后端停掉时                 │ system socket + system token
              ▼ 显示维护页                 ▼
     status.json（只读挂载）◀──写── tp-agent（宿主侧唯一管理者，不在平台 compose 栈里）
                                    ├─ plugin 角色：插件生命周期 + Broker 范围刷新（原 P1 监视进程）
                                    └─ system 角色：检查配置 / 重启 / 备份 / 恢复 / 升级
                                              │
                         docker compose（固定参数）  +  pg_dump / pg_restore（在 postgres 容器里执行）
```

- 停启平台组件：调用 `docker compose`，参数固定（项目目录、compose 文件、服务名都来自安装目录，不接受外部传入）。compose 文件是唯一的权威配置，不在 Agent 里另写一套编排。
- 备份数据库：用 `docker exec` 在 postgres 容器里运行 `pg_dump`/`pg_restore`，这样版本自然和数据库服务端一致。

### 3.6 健康门（Verify）

| 组件 | 判定方法 | 现状 |
|---|---|---|
| backend（Gin） | `/health` 扩展为：数据库连通、`sys_version` 等于程序版本、Redis 连通 | 要改（R1） |
| encore-api / notification-core | 各服务的 healthz，加上 Encore 迁移版本 | 部分已有 |
| automation-scheduler | 进程存活 + 自检 | 要盘点 |
| broker-owner / mosquitto | 端口可连接 + broker-owner 自检 | 要盘点 |
| 插件 | Agent 已有 `HealthSpec` | 已有 |
| 整栈 | `tp-agentctl system verify`：以上各项全部通过，并且版本与 manifest 一致；超时默认 5 分钟 | 新增（R1） |

### 3.7 重启顺序

- **控制面重启（默认）**：停止 automation-scheduler → notification-core → encore-api → backend；启动 backend（执行迁移）→ encore-api → notification-core → automation-scheduler。Broker 和插件不动，**设备不掉线**。
- **全栈重启**：在上面的基础上，再按 P1 已验证的顺序处理 broker-owner → mosquitto，最后是插件（由 plugin 角色处理）。
- postgres、redis 只在恢复、升级（数据库镜像变化）时才重启。

---

## 4. 小处着眼：备份、恢复、升级的细节

### 4.1 备份包格式

```
tp-backup-20261112T0300Z-v1.4.0-full.tar
├── manifest.json          # 先读它，再决定能不能恢复
├── db/relational.dump     # pg_dump -Fc
├── config/                # docker-compose.yml、niginx.conf、config/native/*（不包括密钥文件）
├── files/                 # 上传文件卷
└── plugins/               # Agent 导出的插件实例清单（不包括 secrets.json）
```

`manifest.json` 至少包含：

```json
{
  "format": 1,
  "platform_version": "v1.4.0",
  "agent_version": "v0.3.0",
  "kind": "full",
  "created_at": "2026-11-12T03:00:00Z",
  "trigger": "manual | pre-upgrade | pre-restore | scheduled",
  "schema": { "sys_version": 33, "encore": { "devicemetadata": 12 } },
  "stores": { "relational": { "adapter": "postgres", "server": "14.x", "timescaledb": "2.x" } },
  "images": { "backend": "sha256:...", "encore-api": "sha256:..." },
  "plugins": [ { "id": "homeassistant", "version": "0.2.0", "generation": 3 } ],
  "includes": { "history": true, "secrets": false },
  "encrypted": false,
  "files": [ { "path": "db/relational.dump", "sha256": "...", "size": 0 } ]
}
```

**备份 Adapter 接口**（Go，放在 Agent 里；符合 A3，第一期只实现 PG）：

```go
type BackupAdapter interface {
    Name() string                                         // "postgres" | "mysql" | "tdengine"
    Describe(ctx context.Context) (StoreInfo, error)      // 服务端版本、扩展版本，写进 manifest
    Estimate(ctx context.Context, o Options) (int64, error)
    Dump(ctx context.Context, w io.Writer, o Options) error
    Restore(ctx context.Context, r io.Reader, o Options) error // PG 实现里负责 pre_restore / post_restore
}
```

**备份包含什么、不包含什么：**

| 内容 | 第一期 | 说明 |
|---|---|---|
| PG 关系库（含 TimescaleDB 历史数据） | ✅ | 完整 `pg_dump -Fc` |
| “不含历史数据”的部分备份 | ⏳ 第二期 | 历史数据可能很大；跳过 hypertable 数据块需要实测 |
| 配置文件 | ✅ | 和 `update.py` 管理的文件组一致 |
| 上传文件卷 | ✅ | |
| 插件实例清单（ID、版本、代次、非密钥配置） | ✅ | 由 plugin 角色导出 |
| `.env`、broker token、RSA 私钥、Agent `secrets.json` | ❌（第一期） | 参考 GitLab，密钥单独保管；恢复到同一台机器时这些文件本来就在。加密功能做好后再放进备份包 |
| Redis | ❌ | 默认认为只是缓存；**R0 必须确认里面没有权威数据** |
| TDengine / MySQL | ❌ | 不在发行版组合里，以后增加 Adapter |

**一致性**：手动或定时备份在线执行（`pg_dump` 本身一致，文件和数据库之间允许秒级偏差）；升级、恢复前的快照先停控制面再做，保证三者一致。

**存放与保留**：默认放在安装目录 `backups/`（权限 0700）；自动产生的快照保留最近 3 份；手动备份不自动删除；可用空间不到预估大小的 2 倍时拒绝执行。

### 4.2 恢复规则

| 情况 | 处理 |
|---|---|
| 备份版本 = 当前版本 | 直接恢复 |
| 备份版本 < 当前版本 | 恢复数据库后**按迁移向前升级** |
| 备份版本 > 当前版本 | 拒绝，提示先升级程序 |
| TimescaleDB 扩展版本不一致 | 拒绝并说明原因 |
| 校验值不匹配、manifest 缺失 | 拒绝 |

步骤：预检 → 当前现场安全快照 → 维护页 → 停止除 postgres 以外的组件（插件由 plugin 角色停止）→ 重建数据库 → `timescaledb_pre_restore()` → `pg_restore` → `timescaledb_post_restore()` → 用配置事务写回配置 → 切换到 manifest 记录的镜像 → 启动 → 插件按清单恢复 → 健康门。

### 4.3 升级规则

1. **预检**（不改任何东西）：校验 release 签名；检查升级路径；检查磁盘空间；**提前拉取新镜像**。
2. **强制快照**：第一期不能跳过。
3. 停控制面 → 用配置事务替换配置和镜像版本 → 启动 backend 执行迁移 → 启动其余组件。
4. **健康门**：通过就 commit；不通过就 rollback（停服务 → 用快照恢复数据库 → 换回旧镜像和配置 → 启动 → 再过一次健康门）。
5. rollback 也失败：进入 `needs_human`，保留所有文件，维护页显示“需要人工处理”和对应命令。

**插件升级**走 plugin 角色已有的“代次”机制（新代次准备 → 授权 → 激活 → 旧代次吊销），不走整栈快照；但会和平台升级出现在同一个“检查更新”列表里（§5）。

**迁移纪律**（写进开发规范）：迁移只向前、可重复执行；由单一执行者带锁执行；迁移完成之前服务不报就绪；所有迁移来源都汇总进 manifest。

### 4.4 维护窗口内的数据面

升级、恢复期间 backend 会停止，设备上报可能丢失。第一期：测出实际停机时长和丢失条数，写进交付说明；界面上明确告诉用户“恢复会把数据回到备份时刻”；不做停机期间的缓冲（第二期）。

---

## 5. 界面设计（参照 HA）

**HA 的做法**：设置页右上角 ⋮ 菜单里有两项，“检查更新”和“重启 Home Assistant”；有可用更新时，设置页顶部会出现更新列表；备份在“设置 → 系统 → 备份”。我们照这个结构做：

### 5.1 入口：系统设置页右上角 ⋮ 菜单

| 菜单项 | 点击后 |
|---|---|
| **检查更新** | 立即读取签名 release manifest 和插件目录，列出可用的更新 |
| **重启系统** | 弹出重启对话框 |

### 5.2 重启对话框

```
┌ 重启系统 ─────────────────────────────────────┐
│ ✓ 配置检查通过                                  │
│                                                │
│ ○ 重启平台服务（推荐）  设备保持连接，约 30 秒     │
│ ○ 全栈重启             包括消息服务，设备会短暂掉线 │
│                                                │
│ ▸ 高级选项（第二期：重启主机 / 关机 / 安全模式）   │
│                               [取消]  [重启]    │
└────────────────────────────────────────────────┘
```

- 打开对话框时先执行“检查配置”；检查不通过时，重启按钮不可用，并显示出错的配置项。
- 点击重启后，页面切换到维护页，显示进度；完成后自动刷新。

### 5.3 更新列表（检查更新的结果）

| 项目 | 当前版本 | 新版本 | 操作 |
|---|---|---|---|
| ThingsPanel 平台 | v1.3.0 | v1.4.0 | [查看说明] [升级]（升级前自动备份，不能取消勾选） |
| tp-agent | v0.3.0 | v0.4.0 | 第一期显示“请用安装器升级”的命令；第二期支持自我更新 |
| 插件：Home Assistant | 0.2.0 | 0.3.0 | [升级]（走插件代次机制） |

### 5.4 备份页（系统 → 备份）

列表（时间、版本、大小、触发方式、是否含历史数据）+ [立即备份] + 每行的 [下载] [恢复] [删除]。第二期增加：自动备份设置、上传备份文件来恢复、加密密钥。

### 5.5 维护页

Agent 把进度写入 `status.json`（只包含步骤、百分比和错误摘要，**不含密钥**），并以只读方式挂载给前端 nginx。维护期间 `/api` 返回 503，页面轮询 `status.json`。这样后端停掉时界面也不会突然消失。

---

## 6. 难事作易：分步路线（每张卡 ≤ 3 天）

| 卡 | 内容 | 仓库 | 完成标准 |
|---|---|---|---|
| **R0** 盘点与审定 | Redis 里有没有权威数据；各组件健康端点；所有迁移来源；`files` 卷位置；PG/Timescale 版本；tp-agent 状态内核哪些能直接复用。只写文档 | 后端 + 安装器 | 盘点表 + 本文定稿 + A4 修订 |
| **R1** 健康门 + system 角色骨架 | 后端 `/health` 增加数据库/迁移/Redis 检查；Encore `GET /api/system/info`；Agent 增加 system socket/token、`/v1/system/status`、`/v1/system/verify`；`tp-agentctl system status/verify` | 后端（占 1 条线）+ 安装器 | 停掉数据库后 `verify` 报失败，并指出是哪个组件；plugin token 调用 system 接口被拒绝 |
| **R2** 检查配置 + 重启 + Agent 托管 | `thingspanel-go --check-config`；`system restart --scope control\|full`；维护页 + `status.json`；**P1 监视进程并入 Agent**；Agent 作为系统服务，机器重启后自动拉起 | 后端（小改）+ 安装器 | 配置写错时拒绝重启；控制面重启时设备不掉线（实测）；重启宿主机后 Agent 和范围刷新自动恢复 |
| **R3** 备份 | `system backup create/list/verify/delete`；PG Adapter；manifest；校验值；保留策略；空间预检；插件清单导出 | 安装器 | 备份包能独立校验；`kill -9` 后不会留下“看起来完整”的包 |
| **R4** 恢复 | `system backup restore`；安全快照；版本兼容规则；Timescale pre/post；插件按清单恢复 | 安装器 | **恢复演练**：备份 → 删除数据卷 → 恢复 → `smoke_channels.sh` + `smoke_history_chain.sh` 通过，HA 插件重新上行 |
| **R5a** 配置事务移植到 Go | 把 `update.py` 的锁、journal、原子替换、签名 release 校验的语义移植进 Agent；和 `update.py` 共用同一个锁文件；用 `update.py` 现有的测试场景做对照 | 安装器 | 对照测试全部通过；两边同时执行时，一方被锁拒绝 |
| **R5b** 升级 + 自动回退 | `system upgrade --to vX`；强制快照；健康门；rollback；`needs_human` | 安装器 | **故障注入**：①迁移报错 ②新镜像起不来 ③每一步 `kill -9`。三种情况都能回到旧版本，或者停在 `needs_human` 且现场完整 |
| **R6** 界面 | Encore `system` API（提交请求、查询状态）；前端：⋮ 菜单（检查更新、重启系统）、重启对话框、更新列表、备份页、维护页 | 后端 + 前端 | 在界面上完成一次“检查更新 → 升级 → 故意失败 → 自动回退”的完整演示 |
| **R7** 统一更新列表 | “检查更新”同时列出平台、Agent 和插件；插件升级接入现有代次机制 | 后端 + 安装器 + 前端 | 界面上能升级 HA 插件，旧代次被吊销 |

**排期建议**：R0 → R1 → R2 → R3 → R4 → R5a → R5b → R6 → R7。大部分工作在安装器仓库（Agent），不占后端的 2 条线；R1、R2、R6 各占后端 1 条线，要和 T1 关系库线错开（R1 改 `/health`，可能和 T1-A1“平台启动与连接”碰到同一块代码）。

**最小可用版本（MVP）= R1 到 R5b，全部用命令行完成。** 做完之后，运维就能安全地重启、备份、升级和回退；界面（R6、R7）只是再加一个入口。

---

## 7. 以后再说（现在不派工）

- 定时自动备份、保留策略界面、异地存储（NAS / S3 兼容）
- 备份加密（类似 HA 的 Emergency Kit），加密后才把密钥文件放进备份包
- 部分备份（不含历史数据）、部分恢复（只恢复配置或某个插件）
- 配置热加载（快速重载，不重启进程）
- 重启主机、关机、安全模式
- Agent 自我更新；`upgrade.sh` 改成只调用 Agent 的外壳
- 维护窗口内缓冲设备上报
- MySQL、TDengine 的备份 Adapter（在 T2、T3 之后）
- 零停机升级（expand/contract 迁移）
- Windows 验收（Agent 目前只支持 Linux）

---

## 8. 决定事项

| # | 问题 | 结论 |
|---|---|---|
| D1 | 扩展 tp-agent，还是另建 Supervisor？ | **已定：扩展 tp-agent**，分 plugin / system 两个隔离的角色（§3.1）。A4 措辞的修订在 R0 中完成 |
| D2 | 用什么语言？ | **已定：Go**，从第一期开始（§3.3）。`update.py` 收缩为“安装 + 安装/升级 Agent + 兜底恢复配置” |
| D3 | 升级前快照能不能跳过？ | 第一期**不能跳过** |
| D4 | 第一期备份包含 TimescaleDB 历史数据吗？ | **包含**；“不含历史数据”放到第二期 |
| D5 | 密钥文件进备份包吗？ | 第一期**不进**，加密做好后再放 |
| D6 | 默认重启范围 | **只重启控制面**，设备不掉线 |
| D7 | 任务编号和排期 | 暂用 R0–R7；是否放在 T8 之前，由负责人决定 |

---

## 9. 风险

| 风险 | 应对 |
|---|---|
| Agent 权限集中 | 两个角色使用独立的 socket 和 token；system 只接受固定动作；镜像版本只能来自签名 manifest；R1 的验收包含“plugin token 不能调用 system 接口” |
| `update.py` 和 Agent 在过渡期同时操作 | 共用同一个锁文件；R5a 的验收包含“两边同时执行时，一方被拒绝” |
| 历史数据很大，备份和升级太慢 | R3 测出每 GB 的耗时，写进文档；第二期提供“不含历史数据”的选项 |
| Redis 里其实有权威数据 | R0 盘点 |
| Encore 迁移和平台迁移版本对不上 | 汇总进同一个 manifest，健康门两者都检查 |
| 回退也失败 | `needs_human` + 保留所有快照 + 维护页给出人工处理命令；不自动删除任何东西 |
| 备份包泄露 | 目录权限 0700；第一期不放密钥；`status.json` 不放敏感信息 |
| 拿测试结果冒充验收 | 交付说明分开写“单元测试”和“真实整栈演练”；R2、R4、R5b 的完成标准都必须是真实整栈演练 |

---

## 10. 执行安排（2026-11-12）

- 由**一个** Sonnet 会话按 R0 → R1 → R2 → R3 → R4 → R5a → R5b → R6 → R7 的顺序逐卡执行，同一时间只占**一条**改代码的线。
- 每张卡一个分支：`wp/R<n>-<短名>`。后一张卡依赖前一张卡、而前一张卡还没合入 develop 时，后一张卡从前一张卡的分支切出，并在交付说明里写明“叠在哪个分支上”。
- 后端 worktree 放在后端仓库的 `.claude/worktrees/`；安装器（Agent）worktree 放在安装器仓库的 `.claude/worktrees/`。两者都要当天登记到后端 `docs/WORKSPACES.md`。
- 每张卡交付一份 `docs/tasks/R<n>-DONE.md`。**不推送、不合并、不发布**，等负责人审查。
- 遇到以下情况就停下来汇报，不要继续往下做：需要用户决定的事；同一张卡超过 3 天；R2、R4、R5b 的真实整栈演练做不了（不能用单元测试代替）。
- 本文件目前只在后端主目录里，没有提交。R0 负责把它提交进分支，并修订审计文档的 A4。
