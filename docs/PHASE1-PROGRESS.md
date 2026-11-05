# 第一阶段产品进度与交接（2026-11-05）

## 给产品经理：现在能做什么

第一阶段目标是让默认 PostgreSQL + TimescaleDB 组合在隔离环境可运行。当前已验证：在本机独立栈登录、建测试设备、按租户隔离设备读写；真实 MQTT 客户端经 Mosquitto 上报后，平台 API 和临时前端副本能显示当前值及历史值。一个测试设备显示 `temp=23.5`，24 小时历史页读到一条记录，平均/最大/最小均为 23.50。证据来自测试数据，不能代表物理设备、客户网络或生产负载。

命令超时的代码已合入本地 develop；另有真实 PostgreSQL + Mosquitto 受控设备测试通过：正常 ACK 显示成功，无 ACK 最终显示失败并有原因。该验收在独立分支完成，尚未合入；MVP 栈镜像也早于 P3，所以不能说当前 MVP 栈已经具备并验证了这项行为。

## 当前不能承诺

- 当前可见的整条产品路径尚未完成验收：登录/设备/遥测是在社区后端镜像上验证；Encore 使用旧镜像和局部旧迁移，虽确认其独立数据库连接可建立且设备列表 API 可读，但不是当前 kernel 源码构建，也没有证明服务间数据一致。
- 当前 MVP 前端是从有未提交改动的源码临时复制后启动的。验证了设备列表及当前/历史读数显示；不是干净发布版前端验收。一个设备有读数，另一个没有读数且显示离线。
- 真实 Home Assistant 上下行曾在 2026-11-04 的 T7b 独立栈通过；当时还需手工执行迁移、刷新 Broker 设备范围和重启服务。它不是当前默认 MVP 栈上的一键安装闭环，也不能证明这些机器步骤已自动化。
- 当前 MVP 证据没有覆盖：P3 超时行为在这套 MVP 镜像中的表现、设备重启后的持久性、完整 Encore/前端/HA 组合、生产发布或升级。
- MySQL 全量适配、TDengine 正式支持、MySQL UI 冒烟、完整四组合及发布均不属于本阶段完成项。

## 已验收范围与证据

| 范围 | 结论 | 证据 / 精确版本 |
|---|---|---|
| 最新后端基线 | 本地 `develop` HEAD `3f3efa2ccec7e146c6f8d9d2da95ac667d52fd19`（`origin/develop` `9d071b77`，本地领先 132 个提交）。该基线含 P3 状态修复和 2026-11-05 派工调整；不是远端已发布版本。 | `docs/DISPATCH-PROMPTS-2026-11-04.md` §0、§2；`docs/PROJECT-AUDIT-2026-11-04.md` §7.9 |
| 独立默认栈 | Compose 项目 `mvp-run-20261105`；TimescaleDB `2.31.0-pg16` / PostgreSQL `16.15`，Timescale 扩展 `2.31.0`，Redis 7，Mosquitto 2。后端镜像 `thingspanel-go:mvp-run-a29fecc`，源码 `a29fecc1c07611c669d633e01030bb8ace22b11c`，不是最新 develop/P3。 | `/Volumes/KIOXIA/archive/evidence/2026-11-05-MVP-RUN/summary.md`、`2026-11-05-MVP-RUN-resume/status.md` |
| MVP 用户路径 | API 初始化管理员、登录、建立两个租户、建设备；已验证跨租户设备详情/更新/当前值读取被拒；独立 MQTT 身份上报，API/SQL 读回当前值和历史。浏览器读数使用临时前端副本。 | 上述 MVP evidence；backend 仅验证到设备/遥测核心路径 |
| P3 真实 Broker 回归 | 最新 develop 已包含超时失败处理代码。独立分支 `wp/p3-real-broker-20261105` HEAD `e9aaa550` 用真实 Mosquitto + Paho 客户端和 PostgreSQL 验证正常 ACK 成功、无 ACK 超时失败及列表投影；日志记录 `TestDurableCommandNativeMosquittoPostgresE2E PASS`。新增测试尚未合入；未验证完整 HTTP/UI、真实一分钟调度周期和强制 ACL 拒绝。 | 分支 `wp/p3-real-broker-20261105` 的 `docs/tasks/P3-real-broker-DONE.md`；`/Volumes/KIOXIA/archive/evidence/2026-11-05-P3-real-broker/p3-native-e2e-run3.log` |
| HA 历史成果 | 真实 HA 上上行、下行曾通过，包含 ESP-01S 灯 on→off→on 和回执；验收现场需要多项手工步骤。 | `docs/tasks/T7b-DONE.md`；旧证据原始路径记录在该 DONE 文件中 |

## 正在进行与下一步

1. **默认栈运行线**：worktree `.claude/worktrees/mvp-run-default-stack`，分支 `wp/mvp-run-default-stack`，HEAD `a29fecc1`。工作区仍有 `docs/WORKSPACES.md` 修改和未跟踪 `deploy/mvp-run/compose.yaml`；保留现场。栈的容器当前在运行，状态以该旧镜像为准。下一步应基于明确的最新源码版本决定是否重建/升级隔离栈，然后补验证命令超时、无回执不报成功和必要服务重启后的数据可用性；不要将旧镜像结果贴到新源码上。
2. **P3 真实 Broker 测试线**：worktree `.claude/worktrees/wp-p3-real-broker-20261105`，分支 `wp/p3-real-broker-20261105`，HEAD `e9aaa550`，测试证据已在 KIOXIA；待审查。当前证据足以支持独立真实 Broker 回归结论，不等同于 MVP 镜像验收。
3. **整栈验收**：按派工第 0 节顺序补齐当前版本、服务健康、租户边界、真实遥测历史、命令 ACK/无 ACK、HA 接入及必要重启。外部 HA 无法接入时把该项保留为未验收，不阻塞记录其他已证事实。

## 环境、路径与版本风险

- 当前运行中的 MVP 项目为 `mvp-run-20261105`，端口仅绑定 loopback：PG `25432`、MQTT `28883`、后端 `29999`、Encore 兼容服务 `24200`；数据库和 Broker 使用独立命名卷。不要把它与同时运行的旧 `thingspanel-community-v130-*` 栈混为一谈；截至本记录，旧栈的 `broker-owner` 和 ThingsVis 容器在重启，后端健康检查仍在启动中。
- Encore 镜像 `tp-local-encore:bb60421f8b06c3e2d1160e7980312aa540a79e62` 构建于 2026-11-02，镜像无可验证源提交标签；二进制 revision 不在本地 kernel Git 对象中。当前 kernel checkout 另有 26 个未提交项。已对该隔离空库执行旧镜像适配的迁移 `0001`–`0009`，未执行 `0010`–`0013`；不能称为最新 Encore 代码或迁移验收。
- 前端临时副本使用 Vite `5.4.19`、loopback `127.0.0.1:5179`；源于主 checkout 的 dirty `master`（79 个提交落后 `origin/master`），因此只作局部可视读回证据。
- 新证据放 `/Volumes/KIOXIA/archive/evidence/2026-11-05-<任务号>/`，不要入仓库。当前 KIOXIA 挂载可用，约 403 GiB 空闲。Go 命令逐任务显式设置 `GOCACHE=/Volumes/KIOXIA/.gocache-<任务号>`、`GOMODCACHE=/Volumes/KIOXIA/.gomodcache-<任务号>`；按需建立独立目录，不依赖或改写全局 Go 环境，不复用/覆盖共享缓存。已有 P3 任务缓存为 `.gocache-p3-real-broker-20261105`、`.gomodcache-p3-real-broker-20261105`。
- 所有日志和证据不得包含令牌或密码；不要在命令行、提交或本文件写入凭据。隔离配置只在 KIOXIA 私有目录维护。

## 阻碍、风险与后续边界

- **主要阻碍是版本一致性**：目前“用户看见读数”“P3 代码在 develop”“真实 Broker 测试通过”来自不同源码/镜像组合。合并/重建后需在同一明确版本上串起验收，才能报第一阶段通过。
- **HA 一键闭环未验收**：旧 T7b 流程依赖人工迁移、Broker scope refresh 和重启；P1 对安装器的补救有审查结果，但新镜像漏打包 `33.sql` 的识别问题仍未解决。不要把历史真实 HA 上下行误报为当前新装闭环。
- 当前默认栈仍有未知项，包括 Encore 当前源码版本连接、服务间设备/数据一致性及重启持久性。下一执行人只修复阻止默认路径、产生错误结果或触及安全底线的问题。
- 暂停 MySQL/TDengine 扩展、数据库切换、非核心清理及完整发布矩阵；缺失路由补齐工作仍暂停。社区版不新增 OTA 代码，不开匿名插件入口，不为测试放宽租户鉴权。

## 交接入口

先读 `docs/DISPATCH-PROMPTS-2026-11-04.md` §0、§2，再读 `docs/PROJECT-AUDIT-2026-11-04.md` §7.9；具体运行证据与边界见上表。运行栈、分支和证据均保留；不删除容器、卷、目录，不推送、不合并，等待负责人审查。
