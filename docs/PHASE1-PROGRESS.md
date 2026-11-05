# 第一阶段产品进度与交接（2026-11-05）

## 给产品经理：现在能做什么

第一阶段目标是让默认 PostgreSQL + TimescaleDB 组合在隔离环境可运行。当前已验证：在本机独立栈登录、建测试设备、按租户隔离设备读写；真实 MQTT 客户端经 Mosquitto 上报后，平台 API 和临时前端副本能显示当前值及历史值。一个测试设备显示 `temp=23.5`，24 小时历史页读到一条记录，平均/最大/最小均为 23.50。证据来自测试数据，不能代表物理设备、客户网络或生产负载。

命令超时的代码与真实 Broker 回归测试现已合入本地 develop。测试使用正常 ACK 和设备收到了命令但不回 ACK 两种路径：前者显示成功，后者经超时 sweep 后显示失败及原因。测试把 durable 行时间回拨并直接调用 sweep，没有等待真实调度周期；证据日志也未记录 Broker/PostgreSQL 完整版本与运行模式。MVP 栈后端镜像仍早于 P3，因此不能说这套运行中的 MVP 镜像已验证超时行为。

## 当前不能承诺

- 当前可见的整条产品路径尚未完成验收：登录/设备/遥测是在社区后端镜像上验证；Encore 使用旧镜像和局部旧迁移，虽确认其独立数据库连接可建立且设备列表 API 可读，但不是当前 kernel 源码构建，也没有证明服务间数据一致。
- 当前 MVP 前端是从有未提交改动的源码临时复制后启动的。验证了设备列表及当前/历史读数显示；不是干净发布版前端验收。一个设备有读数，另一个没有读数且显示离线。
- 真实 Home Assistant 上下行曾在 2026-11-04 的 T7b 独立栈通过；当时还需手工执行迁移、刷新 Broker 设备范围和重启服务。它不是当前默认 MVP 栈上的一键安装闭环，也不能证明这些机器步骤已自动化。
- 当前 MVP 证据没有覆盖：P3 超时行为在这套 MVP 镜像中的表现、实体设备断电重连、完整 Encore/前端/HA 组合、生产发布或升级。
- 隔离栈经历 OrbStack 停止/启动后，已有数据库、Redis、Broker 数据及 UI 读数仍可读；这是已存在测试数据的重启持久性检查，不代表新建/升级迁移或服务间同步已验收。
- HA 接入账号已验证；当前 HA 设备的上行/下行完整端到端和一键安装闭环仍在验收中。这里不记录账号或任何凭据。
- MySQL 全量适配、TDengine 正式支持、MySQL UI 冒烟、完整四组合及发布均不属于本阶段完成项。

## 已验收范围与证据

| 范围 | 结论 | 证据 / 精确版本 |
|---|---|---|
| 最新后端基线 | 本地 `develop` HEAD `dc5fc8e7419df85c310fc22e348f9bba0cb005fa`，本地领先 `origin/develop` 141 个提交。含 P3 状态修复、真实 Broker 测试与验收记录；未发布。 | `docs/DISPATCH-PROMPTS-2026-11-04.md` §0、§2；`docs/tasks/P3-real-broker-DONE.md` |
| 独立默认栈 | Compose 项目 `mvp-run-20261105`；TimescaleDB `2.31.0-pg16` / PostgreSQL `16.15`，Timescale 扩展 `2.31.0`，Redis 7，Mosquitto 2。后端镜像 `thingspanel-go:mvp-run-a29fecc`，源码 `a29fecc1c07611c669d633e01030bb8ace22b11c`，不是最新 develop/P3。 | `/Volumes/KIOXIA/archive/evidence/2026-11-05-MVP-RUN/summary.md`、`2026-11-05-MVP-RUN-resume/status.md` |
| MVP 用户路径 | API 初始化管理员、登录、建立两个租户、建设备；已验证跨租户设备详情/更新/当前值读取被拒；独立 MQTT 身份上报，API/SQL 读回当前值和历史。浏览器读数使用临时前端副本。 | 上述 MVP evidence；backend 仅验证到设备/遥测核心路径 |
| P3 真实 Broker 回归 | 测试已合入本地 develop（真实 Mosquitto + Paho 设备客户端、PostgreSQL）；正常 ACK 显示成功、无 ACK 经超时 sweep 后显示失败并更新列表投影。PASS 日志为 7.08s。未验证真实一分钟调度周期、强制 ACL 拒绝、HTTP/UI；日志没有完整 Broker/PG 版本及运行模式元数据。 | `docs/tasks/P3-real-broker-DONE.md`；`/Volumes/KIOXIA/archive/evidence/2026-11-05-P3-real-broker/p3-native-e2e-run3.log` |
| MVP 重启后读回 | 18:30 OrbStack 恢复后，隔离栈健康检查通过；PG 中 2 个设备、3 条 current、9 条 history 与重启前一致；授权 UI 仍显示 `temp=23.5` 和 24 小时历史。Encore 独立库为 0 设备，确认的是各自存续，不是跨服务同步。 | `/Volumes/KIOXIA/archive/evidence/2026-11-05-MVP-RUN-resume/status.md` § OrbStack recovery / persistence recheck |
| HA 接入状态 | HA 账号已验证；当前 MVP 的 HA 设备上行、下行与一键安装闭环仍在进行端到端验收。历史真实 HA 上下行曾通过，但当时需手工迁移、刷新 Broker 设备范围及重启服务。 | 当前验证状态按本次交接更新；历史结果见 `docs/tasks/T7b-DONE.md` |

## 正在进行与下一步

1. **默认栈运行线**：worktree `.claude/worktrees/mvp-run-default-stack`，分支 `wp/mvp-run-default-stack`，HEAD `a29fecc1`。工作区仍有 `docs/WORKSPACES.md` 修改和未跟踪 `deploy/mvp-run/compose.yaml`；保留现场。栈容器在 OrbStack 恢复后已启动，已有数据和 UI 读数可读；状态以旧镜像为准。下一步应基于明确的最新源码版本决定是否重建隔离栈，然后补验该版本的命令超时、无回执不报成功及 HA 设备链路，不把旧镜像结果贴到新源码上。
2. **P3 真实 Broker 验收**：代码及测试已合入 develop；独立测试 worktree 和证据保留。下一步是在当前明确版本的 MVP 镜像上验证无回执不误报成功，必要时补足版本/运行模式证据。
3. **HA 端到端**：HA 账号已验证，继续完成当前设备上行、下行及安装器自动步骤验收；如外部环境导致无法完成，记录具体障碍并保持未验收。
4. **整栈验收**：按派工第 0 节补齐当前版本、服务健康、租户边界、真实遥测历史、命令 ACK/无 ACK、HA 接入及重启后读回。

## 环境、路径与版本风险

- 当前运行中的 MVP 项目为 `mvp-run-20261105`，端口仅绑定 loopback：PG `25432`、MQTT `28883`、后端 `29999`、Encore 兼容服务 `24200`；数据库和 Broker 使用独立命名卷。不要把它与同时运行的旧 `thingspanel-community-v130-*` 栈混为一谈；截至本记录，旧栈的 `broker-owner` 和 ThingsVis 容器在重启，后端健康检查仍在启动中。
- Encore 镜像 `tp-local-encore:bb60421f8b06c3e2d1160e7980312aa540a79e62` 构建于 2026-11-02，镜像无可验证源提交标签；二进制 revision 不在本地 kernel Git 对象中。当前 kernel checkout 另有 26 个未提交项。已对该隔离空库执行旧镜像适配的迁移 `0001`–`0009`，未执行 `0010`–`0013`；不能称为最新 Encore 代码或迁移验收。
- 前端临时副本使用 Vite `5.4.19`、loopback `127.0.0.1:5179`；源于主 checkout 的 dirty `master`（79 个提交落后 `origin/master`），因此只作局部可视读回证据。
- 新证据放 `/Volumes/KIOXIA/archive/evidence/2026-11-05-<任务号>/`，不要入仓库。当前 KIOXIA 挂载可用，约 403 GiB 空闲。Go 命令逐任务显式设置 `GOCACHE=/Volumes/KIOXIA/.gocache-<任务号>`、`GOMODCACHE=/Volumes/KIOXIA/.gomodcache-<任务号>`；按需建立独立目录，不依赖或改写全局 Go 环境，不复用/覆盖共享缓存。已有 P3 任务缓存为 `.gocache-p3-real-broker-20261105`、`.gomodcache-p3-real-broker-20261105`。
- 所有日志和证据不得包含令牌或密码；不要在命令行、提交或本文件写入凭据。隔离配置只在 KIOXIA 私有目录维护。

## 阻碍、风险与后续边界

- **主要阻碍是版本一致性**：MVP 读数和重启存续来自旧后端镜像 `a29fecc1`，P3 代码及真实 Broker 测试则在本地 develop `dc5fc8e7`。需在同一明确新镜像版本上复验核心路径，才能报第一阶段通过。
- **HA 一键闭环未验收**：旧 T7b 流程依赖人工迁移、Broker scope refresh 和重启；P1 对安装器的补救有审查结果，但新镜像漏打包 `33.sql` 的识别问题仍未解决。不要把历史真实 HA 上下行误报为当前新装闭环。
- 当前默认栈仍有未知项，包括 Encore 当前源码版本连接、服务间设备/数据一致性、当前 P3 镜像行为和 HA 端到端。现有证据确认隔离栈已存数据在一次环境重启后仍可读；不把该结果扩大成完整整栈或升级验收。下一执行人只修复阻止默认路径、产生错误结果或触及安全底线的问题。
- 暂停 MySQL/TDengine 扩展、数据库切换、非核心清理及完整发布矩阵；缺失路由补齐工作仍暂停。社区版不新增 OTA 代码，不开匿名插件入口，不为测试放宽租户鉴权。

## 交接入口

先读 `docs/DISPATCH-PROMPTS-2026-11-04.md` §0、§2，再读 `docs/PROJECT-AUDIT-2026-11-04.md` §7.9；具体运行证据与边界见上表。运行栈、分支和证据均保留；不删除容器、卷、目录，不推送、不合并，等待负责人审查。
