# 合并就绪评估：北向服务（数据转发）

写给要合并这个分支的同事。**先看第 0 节**：结论是不能直接合到 `main`，不是因为代码烂，
而是因为上游依赖和几项发行验证没到位。每一节都给了可自己复核的命令。

- 评估时间：2026-10-23
- 后端分支：`codex/northbound-community-20261021`（HEAD `e74619a2`，已推送，工作区干净）
- 前端分支：`codex/northbound-frontend-20261021`（HEAD `613966d7`，已推送，工作区干净）
- 对照基准：`docs/NORTHBOUND-SERVICE-ENCORE-IMPLEMENTATION-PLAN-2026-10-21.md`
  的 NB-01…NB-10 完成标准与 §11.1 交付条件

---

## 0. 结论

**没有到可以直接合并的阶段。** 代码、文档、验收脚本都在分支上，评审可以现在开始；但
按任务书 §11.1 还差 4 项硬条件，其中第 1 条是**合并顺序问题**，不解决会一次性把 22.9 万
行带进 `main`。

| # | 硬阻断 | 后果 | 谁能解 |
|---|---|---|---|
| 1 | mosquitto 驱动这条线还没进任何主线 | 北向分支基线 `1a1ef0a0` 是这条线的 HEAD。`main` 和 `main-1.3.0` 里都没有 `internal/mosquittodriver/`、`internal/brokerledger/`、`telemetry_broker.go`。直接合 = 581 新增 + 41 修改、+229346 行一起进 `main`，等于替整条 broker 线做了一次未经单独评审的合并 | 团队先把 mosquitto 线定版并推成分支 |
| 2 | 前端没构建、没类型检查、没在浏览器点过 | NB-09 完成标准是「页面真实可用」，现在只有静态检查。前端 `build` 会先跑 `vue-tsc`，类型错就红 | 我（需要能装依赖的网络） |
| 3 | 社区库迁移在子目录，自动迁移不执行 | `initialize/pg_init.go:231` 只扫 `sql/<数字>.sql`；`sql/northbound/001…002` 不会被自动执行。合了、重启了、功能启用了，表却不存在 | 合并时必须配套跑 `deploy/northbound/migrate.sh` |
| 4 | 没有发行清单 | NB-10 要求锁定代码/镜像/迁移 checksum/插件 digest。现在镜像也没构建，无从锁 | 我（需要能构建的环境） |

好消息：**北向功能默认关闭**（`northbound.enabled=false`），所以第 1、3 条不会立刻弄坏
线上；但「合并后默认关闭」不等于「可以合并」，因为评审看到的 diff 会混着整条 broker 线。

### 0.1 关于 broker 线：先把名字理清（2026-10-23 更正）

我最初写的是「上游 gmqtt 分支没合进 main，要先合 gmqtt」。方向对，但**名字说错了**，而且
漏查了 `main-1.3.0` 和本地主工作树。实测结果：

| 分支 / 位置 | `internal/mosquittodriver/` | `internal/brokerledger/` | `telemetry_broker.go` | `reliable.go` |
|---|---|---|---|---|
| `origin/main`（10-16） | 0 | 0 | 0 | 0 |
| `origin/main-1.3.0`（10-22，最新发布线） | 0 | 0 | 0 | 0 |
| `origin/codex/gmqtt-community-20261019`（10-20） | **14** | 11 | 1 | 1 |
| 本地主工作树 `main`（`0e3ba828`） | 14（未跟踪） | 11（未跟踪） | 1（未跟踪） | 1（未跟踪） |

三条结论：

1. **gmqtt 确实没了。** `codex/gmqtt-community-20261019` 这个分支名是历史遗留，里面已经是
   mosquitto 实现（`internal/mosquittodriver/` 含 driver/runtime/command/plugin 共 14 个文件）。
   所以「gmqtt 换成 mosquitto」在代码层面成立，只是分支没改名——**看名字会判断错**。
2. **但这套 mosquitto 实现没进任何远端主线。** `main` 与 `main-1.3.0` 里连
   `internal/mosquittodriver/` 都没有；`main-1.3.0` 上只有 `internal/storage/telemetry_writer.go`，
   **没有可信遥测批次事务这个边界**，北向的登记点在那里无处可挂。
3. **主工作树里还有一份更新的、未提交的版本。** `internal/mosquittodriver/`、
   `internal/brokerledger/`、`internal/storage/telemetry_broker.go` 全是 `??` 未跟踪状态，
   且与 `1a1ef0a0` 不同：多了 `delegated_protocol_test.go`，少了 `plugin.go`/`plugin_test.go`，
   `telemetry_broker.go` 差 38 行、`reliable.go` 差 6 行。函数签名
   `commitTrustedTelemetry` / `recordTrustedTelemetry` 仍在，所以北向的登记点可以移植，
   但不能直接套用，需要按新版重新对位。

**对合并意味着什么**：不是「先合一个叫 gmqtt 的分支」，而是**先把 mosquitto 这条线定版、
推成可评审的分支**，再让北向 rebase 上去、重新确认两个登记点的插入位置。北向现在基于 10-20
那版，团队手上可能有更新的版本，两边不一致就合不了。

---

## 1. 任务对照表（应做 / 实际 / 差异）

状态含义：**✅ 有真实运行证据** · **🟡 代码完成但有验证缺口** · **🔴 未做**

| 任务 | 应做（任务书完成标准，压缩） | 实际做到 | 差异 |
|---|---|---|---|
| **NB-01** 基线与范围 | 记录三个仓库 HEAD/状态、成果来源去向、目录 owner、输入 profile、参数表、冻结目录 | 台账 §0 有基线表与「为何不用 main」；CHANGES §2 有合入前置；参数表为测试档并声明非 SLA | ✅ 基本满足。前端基线原先记错（`c6968c77`），已更正为 `e49f49ac`；「受保护既有代码」只列了目录，没逐文件登记 |
| **NB-02** Encore 搬入社区版 | 社区版独立构建+启动、无 Kernel 路径、最小依赖、功能默认关、**真实链路验证身份冻结/注销/租户/服务身份/超管代租户** | `encore-api/` 独立 module；`encore test ./...` 通过；`go vet` 通过；启动后 inbox 真实受理；默认 `Enabled=false`；无 Kernel replace | 🟡 **生产镜像未构建**（`encore build docker` 17 分钟未完成）；**身份链路只用了 `TP_AUTH_MODE=dev`**，冻结/注销/代租户这些没跑过真实签名链路 |
| **NB-03** 统一合同 | northbound/v1 权威合同：API、事件、插件 wire、Schema、错误码、状态机、权限矩阵、示例 | `CONTRACT-v1.md` v1.2，11 章齐；错误码与 gRPC 映射对齐（冲突改 `aborted`→409）；有修订记录 | ✅ 合同本身齐。**「新增第三个插件」的说明缺失**（NB-08 的共同交付项） |
| **NB-04** 数据交接 | 两个既有事务边界登记；独立 owner/配额；事件 ID 稳定；可重试桥接+Inbox 去重；多任务各自结算；**在提交后/桥接后/Inbox 后分别制造中断** | `telemetry_broker.go`、`reliable.go` 各插一处登记；Outbox 与业务行同事务；`EventIDFor` 由回执派生；relay 认领→POST→逐条结果；一条事件命中两任务各自 `delivered` | 🟡 **设备 → Broker → 厂商插件 → `ingress_v1` 这一跳没跑**（`.so` 不在本仓库），登记点是否真被调用只有代码审查；中断恢复只覆盖了租约过期，**没覆盖「source 已提交、relay 未发出」和「Inbox 已提交、source 未确认」两种窗口** |
| **NB-05** 配置与权限 | 连接/任务/版本/预览/启停/重放；租户与动作校验；秘密加密与掩码；版本冲突 409；重复点击不建两份 | 全部 API 实现并真实调用；秘密加密存储、查询只回脱敏；版本冲突返回 409 `aborted`；`test-send` 与真实投递分开 | 🟡 **两租户隔离没测**（journey/recovery 都只用 `t1`）；**无权限请求被拒**没测；**注销/冻结身份**没测。这三条是 NB-05 完成标准原文 |
| **NB-06** 调度与恢复 | 持久认领/租约/attempt；六种状态与 ACK 分离；部分成功；Retry-After；退避抖动；并发与积压限制；死信可查；**有界清理、重启恢复、优雅退出、指标和审计** | 8 个故障场景真实跑过：500→retry_wait、429 尊重 Retry-After、超时→unknown 不自动重试、unknown 重放两级门槛、部分成功不互相清账、401→needs_repair+dead、租约过期自动回收、容量拒绝不留半写 | 🟡 保留期清理有实现（10 分钟 ticker + `pruneExpired`），**没等真实 7/30 天窗口**；**没有 metrics、没有 audit 记录**（NB-06 明确要求）；优雅退出未单独验证 |
| **NB-07** 插件真实运行 | 官方插件构建源与锁定 digest；真实 health/readiness；非 root、限 CPU/内存、只读文件系统、最小 capability、限制出口；秘密按 tenant+connection 隔离 | 两个插件真进程跑；无令牌 401（fail closed）；容器镜像构建并运行验证（distroless + `USER 65532`）；同一插件按 connection 复用实例 | 🟡 **`compose.yaml` 没设 `read_only`、`cap_drop`、`mem_limit`、`cpus`、`tmpfs`**；**没有锁定 digest**（镜像是本地 `:dev`）；停止插件→页面呈现不可用这一条没测 |
| **NB-08** HTTP/MQTT 两插件 | 两插件真实送达；TLS；稳定幂等键；429/401/5xx 分类；本地接收端含故障注入；MQTT QoS1、PUBACK 只算 Broker 接收、禁止指回平台 topic；**manifest、Schema、新增插件说明** | HTTP 与 MQTT 都真实送达并 `destination_acked`；接收端支持 ok/delay/fail500/fail401/failContains；MQTT QoS0 报 `ack_level=none`；TLS12 默认校验 | 🟡 **没有 manifest 文件**；**没有「怎样新增第三个插件」说明**；MQTT 禁止指回平台 topic 只有约定，没做运行时校验 |
| **NB-09** 用户页面 | 路由/权限/页面/Schema 表单/API 客户端/多语言/空白加载失败态；普通用户不看队列就能配一条；**页面与后端每个按钮、每个状态含义完全一致** | 4 个页面 + 路由 + 109 个文案键（中英齐全）；Schema 驱动表单；unknown 重放需勾风险；「等待投递」与「容量未登记」分开显示 | 🔴 **`pnpm build` / `vue-tsc` 没跑过**（`pnpm install` 卡在网络拉取 54 分钟中止，`node_modules` 至今不存在）；**没有在浏览器里真实点过一遍** |
| **NB-10** 发行 | 三入口真实报告；锁定代码/镜像/迁移 checksum/插件 digest 并生成发行清单；**空库新装、旧版升级、重复迁移、中断恢复、应用/插件回退、备份恢复**；无 Kernel 干净环境重建；使用/维护/容量/故障/升级回退文档 | journey 27 + recovery 22 + release 25 = **74 条断言，0 失败 0 跳过**，且在空库冷启动后复跑过；7 份文档 + compose + `migrate.sh` + env 模板 | 🟡 **没有发行清单/checksum**；**空库新装、旧版升级、重复迁移、回退、备份恢复都没实测**；**干净环境（无 Kernel、无个人目录）重建没做**；生产镜像未构建 |

---

## 2. 任务书 §11.1 交付条件逐条对照

| 条件 | 状态 | 说明 |
|---|---|---|
| 10 项都有代码/文档/验证证据，状态明确 | ✅ | 台账逐条记，本节表格即汇总 |
| Encore 服务与必要依赖在社区版，两插件源码与构建说明纳入交付 | 🟡 | 源码与 Dockerfile 在；**构建说明缺 manifest/digest** |
| 前端菜单真实可用，与后端合同一致 | 🔴 | 菜单 SQL 与路由在，**页面没构建、没点过** |
| 三组综合测试通过，关键失败路径没跳过 | ✅ | 74 条断言 0 跳过，报告在 `reports/` |
| 迁移按 owner 划分、增量兼容；回退不删客户数据 | 🟡 | 迁移是增量、独立 schema；**回退步骤只写在文档里，没实测** |
| 旧版升级起点、不能升级的情形、备份恢复要求写清楚 | 🟡 | `DEPLOYMENT.md` §5/§6 与 `MAINTENANCE.md` §7 有写；**没实测**，且未指明从哪个旧版本可以升 |
| 发行清单锁定代码/镜像/迁移 checksum/配置，不依赖个人机器或 Kernel | 🔴 | **没有清单**；验收用的端口、路径都是本机值（文档已注明） |
| 交付记录区分源码检查/单元/合同测试/模拟设备/真实 Broker/接收端/客户目标 | ✅ | 台账与报告分开标注；设备接入跳明确写「未测」 |

---

## 3. 要补什么才能合（按优先级）

### P0：不补就不能合

1. **先给 mosquitto 线定版**：确认团队以哪一份为准——`codex/gmqtt-community-20261019`
   的 `1a1ef0a0`，还是主工作树里那份更新的未提交版本。定版后推成可评审分支并合入主线，
   再让北向 rebase 上去、重新确认 `telemetry_broker.go` / `reliable.go` 两处登记点的位置。
   复核命令：
   ```bash
   # 现在：622 文件 / +229346（相对 origin/main，混着整条 broker 线）
   git diff --stat origin/main...codex/northbound-community-20261021 | tail -1
   # 相对 broker 线本身：142 文件 / +27603 −3（含 tools/；排除 tools/ 是 136 / +27164 −3）
   git diff --stat origin/codex/gmqtt-community-20261019...codex/northbound-community-20261021 | tail -1
   ```
2. **前端真构建一次**（NB-09 的硬条件）：
   ```bash
   cd thingspanel-frontend-community
   pnpm install
   npx vue-tsc --noEmit --skipLibCheck     # 注意 package.json 的 typecheck 脚本是 Windows 语法
   pnpm build
   pnpm dev                                # 打开 /data-forwarding/northbound，逐页点一遍
   ```
   验收：`vue-tsc` 0 error；`build` 成功；浏览器里能建连接、建任务、预览、测试发送、
   看投递、暂停/启用，且禁用功能时不出现假按钮。
3. **合并后必须跑迁移**（否则功能静默不工作）：
   ```bash
   ./deploy/northbound/migrate.sh          # 应用 sql/northbound/001、002
   ```
   验收：`northbound_source_outbox` 等 4 张表存在；页面「源端」不再显示 `schemaReady=false`。

### P1：合并前应补，否则「合了但不能发版」

4. **发行清单**：锁后端/前端/Encore 版本、迁移文件 sha256、插件镜像 digest、配置项清单。
5. **空库新装 + 重复迁移 + 回退**（NB-10 原文）：干净库跑一次 `migrate.sh` 两次（验证
   幂等）、装起来、再按 `DEPLOYMENT.md` §6 回退一次。
6. **两租户隔离 + 无权限拒绝 + 注销/冻结身份**（NB-05 完成标准）：现在一条都没测。
7. **容器安全项**：`compose.yaml` 补 `read_only: true` + `tmpfs: /tmp`、`cap_drop: [ALL]`、
   `mem_limit`、`cpus`，并实测插件在只读根文件系统下仍能跑。
8. **设备接入那一跳**：需要厂商 Mosquitto 插件 `.so`。没有它，NB-04 只能算「两端各自
   验证过」，不能算端到端。

### P2：可以带着已知限制发布

9. 保留期真实窗口（7/30 天）——按 `MAINTENANCE.md` §4 上线第一周核对。
10. 多副本核心——租约与 `SKIP LOCKED` 按多副本写的，但只单副本跑过。
11. metrics / audit 记录（NB-06 要求）——当前完全没有。

---

## 4. 给合并人的操作步骤

```bash
# 1. 后端：先确认 mosquitto 线（分支名仍叫 gmqtt）已经在主线
git ls-tree -r --name-only origin/main-1.3.0 -- internal/mosquittodriver | head -1
# 有输出才说明这条线已合；现在没有输出

# 2. 若尚未合：先把这条线定版、推成分支、走评审
#    注意分支名是历史遗留，内容已经是 mosquitto，别被名字骗了
git checkout -b broker/mosquitto-release origin/codex/gmqtt-community-20261019

# 3. 北向 rebase 到定版后的 broker 线，重新确认两个登记点
git rebase broker/mosquitto-release
# 改完必须重跑：telemetry_broker.go / reliable.go 在新版里差 38 行 / 6 行

# 4. 合北向（此时 diff 应只有 136 文件，排除 tools/）
git merge --no-ff codex/northbound-community-20261021

# 5. 跑迁移（必须，自动迁移不扫子目录）
./deploy/northbound/migrate.sh

# 6. 前端
cd ../thingspanel-frontend-community
git merge --no-ff codex/northbound-frontend-20261021
pnpm install && pnpm build
```

**合之前请确认这 6 个文件的改动是预期的**（它们动的是既有数据面，不是新目录）：

| 文件 | 改了什么 |
|---|---|
| `internal/storage/telemetry_broker.go` | 可信遥测批次事务里加一次登记调用 |
| `internal/uplink/reliable.go` | 可靠属性/事件事务里加一次登记调用 |
| `main.go` | 加 `app.WithNorthboundSource()` |
| `router/router_init.go`、`router/apps/enter.go`、`internal/api/enter.go` | 路由与控制器注册 |

这 6 个文件是唯一会碰到线上主链路的改动。**功能默认关闭**，`enabled=false` 时 relay 不启动、
不写任何行；但登记调用的位置仍在事务里，评审时要重点看它不返回错误、不影响主流程。

---

## 5. 已有证据在哪

| 证据 | 位置 | 怎么看 |
|---|---|---|
| 三个验收入口 | `scripts/qa/northbound/{journey,recovery,release}.py` | 直接 `python3` 跑，需 9 个进程在 |
| 真实报告 | `docs/northbound/reports/*.md` | 27 / 22 / 25 条，0 失败 0 跳过 |
| 逐任务施工记录 | `docs/northbound/EXECUTION-LEDGER.md` | 每步「做了什么/为什么/改了哪些文件/验证/证据/限制」 |
| 合同 | `docs/northbound/CONTRACT-v1.md` | v1.2，§11 有一致性条目→脚本断言映射 |
| 部署/升级/回退 | `docs/northbound/DEPLOYMENT.md` | §8 明列没验证的部分 |
| 使用/维护 | `USAGE.md`、`MAINTENANCE.md` | 值班看什么、故障怎么查 |
| 变更摘要 | `docs/northbound/CHANGES.md` | 合入前置条件、文件清单、不合入内容 |
| 接手须知 | `docs/northbound/HANDOVER.md` | 九个进程怎么起、踩过的坑 |

---

## 6. 一句话给评审

**可以开始评审，但别急着合 `main`。** 先把 mosquitto 那条线定版（分支名还叫 gmqtt，
内容已经是 mosquitto；`main` 和 `main-1.3.0` 都还没有它），北向 rebase 上去重新对位两处
登记点，再让前端真构建一次，合并时记得跑 `migrate.sh`。这几件事做完，代码质量本身没有
问题——74 条断言是真跑的，两个插件是真送达的，账本是真落库的。剩下的缺口集中在
「发行级验证」（新装/升级/回退/清单）和「安全边界」（租户隔离、容器加固），不是功能没写完。
