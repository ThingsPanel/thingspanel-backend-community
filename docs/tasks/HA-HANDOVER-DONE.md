# HA 插件一键安装 — 移交缺口分析与补齐（交付说明）

分支 `wp/ha-handover`（基于本地 develop `c0ab14d1`），worktree `.claude/worktrees/wp-ha-handover`。**未提交、未推送、未合并。**
同事的入口文档是 [`../HA-PLUGIN-HANDOVER.md`](../HA-PLUGIN-HANDOVER.md)；本文给**负责人**看：移交前缺什么、补了什么、还要你做什么。

## 1. 结论

材料本身很全（代码、制品、签名、单测、真实 HA 的历史验收），但**不能直接交给同事**，原因不是缺功能，而是：没有一份串起来的入口、现成环境是坏的、被测代码没有稳定基线、有几件事只有你能做。这次补了前者能补的部分，下面第 4 节是只有你能办的事。

## 2. 发现的缺口（按严重程度）

| # | 缺口 | 证据 | 处理 |
|---|---|---|---|
| G1 🔴 | **验收栈当前是坏的**：`plugin_installations` 为 `failed / renewal_failed`，generation 5，三代身份全部吊销；事件表 2026-11-08 16:01–16:09 连续 `renewal_failed`，最后转 `failed`。 | `tools/check-test-env.sh tp-ha-ui-acceptance-20261106 58999`（只读，已实跑） | 没动栈（不是我的）。入口文档 §4.1 写明，A13 专门测“失联多久会坏”。**需要你决定：Retry 恢复，还是用 develop 重建后端镜像。** |
| G2 🔴 | **栈跑的后端代码在任何分支上都找不到**：镜像取自提交 `96d1964c`（链末端 `67f4afe9`），`git for-each-ref --contains` 为空，原分支 `claude/wp-plugin-install-ui-8bf83e` 与其 worktree 都已不存在，文档却还在引用它们。 | `git for-each-ref --contains 96d1964c` 无输出；`git worktree list` 无该路径 | **已加保护**：打本地标签 `archive/ha-ui-acceptance-stack-20261109` → `67f4afe9`（只新增一个标签，没改任何分支）。 |
| G3 🔴 | **栈的后端代码和 develop 不一致**：`reconcile.go` 的 `maintain()` 在栈镜像里**续租要求目录可用**，develop 已有修复（`4bf3ee22` “renew running instance without catalog”、`5707e9bc`）。`git diff develop 96d1964c` 在 `internal/plugininstall` 有 3 个文件差异。 | `git diff develop 96d1964c -- internal/plugininstall` | 入口文档 §4.1 标明。**与 G1 的 `renewal_failed` 是否有因果没有验证**，不要当结论。develop 的 `go test ./internal/plugininstall ./internal/pluginidentity` 通过（只是单测，假 Agent，不是端到端）。 |
| G4 🔴 | **干净机器路径走不通**：公开安装器默认后端/前端镜像是 v1.3.0。实测后端镜像无 `internal/plugininstall`、无 `sql/34.sql`/`35.sql`；前端静态资源里没有 `plugin/installs`。带功能的镜像（后端、前端、`thingspanel-mosquitto`）没发布（总监任务 P4，只有你能做）。 | `docker run … ls /go/src/app/internal/plugininstall`（v1.3.0：不存在；`thingspanel-backend-ha-ui:20261106`：存在） | 入口文档 §4.2 如实写“被阻塞”。 |
| G5 🟠 | **没有“起栈”说明**：验收栈是 compose + override + 手工 agent-proxy + `agent2/` 拼的，没有 README，也没有一键脚本。 | 栈目录内容 | 只写了现状和位置，**没有**重写成一键脚本（那要碰栈、重建镜像，不在我这次范围）。 |
| G6 🟠 | **基线不清**：本地 develop（`c0ab14d1`）≠ 远端 develop（`9d071b7`）；各候选（后端 `91b1e958`、前端 `c40ad217`、HA `0b3240e`）都没合入。后端主目录停在 detached HEAD 且**有一个没做完的 cherry-pick**（`91b1e958`，多个冲突文件）。 | `git status`（会话开始时的快照）；`ThingsPanel-HA各仓库develop合入审计-2026-11-08.md` | 入口文档 §2、§4.3 列出，并警告同事别碰主目录。**需要你拍板基线。** |
| G7 🟠 | **HA 仓库 `main` 目录是脏的**，有 `ha_actions.go`、`capabilities_test.go` 等未提交新改动，不属于已发布的 `0b3240e`；已发布版本在本地分支 `wp/t6-ha-public-artifact`，没有同名远端分支。 | `git status`；审计文档 | 入口文档 §2 明确“别拿 main 目录测”。 |
| G8 🟠 | **租户一半没验证**：租户账号在页面里新建接入点、选实体、建设备从未走通；历史上下行靠 API 模拟。 | `PLUGIN-UI-DONE.md` §4.8；教程文档“没有在本机用租户账号完成现场验证” | 做成了 B 组用例，并标明“没验证过”。 |
| G9 🟠 | **没有可用账号**：教程文档自己写“没有可安全确认并交付的演示账号”。 | 同上 | 需要你准备（第 4 节）。 |
| G10 🟡 | **没有测试用 HA 配方**：历史上用的是负责人的真实 HA 或一次性脚本，脚本里有硬编码路径。 | T7b、`pui-stack/ha-onboard.py` | **已补** `tools/ha-demo/`。 |
| G11 🟡 | **文档互相打架**：`T7-DONE`（失败）被 `T7b`（通过）推翻；`PLUGIN-UI-DONE` 说租户管理员安装，现在是平台超管；`WORKSPACES.md` 里的插件条目指向已不存在的 worktree。 | 对照阅读 | 入口文档 §10 给了“哪些有效、哪些过期”表；**没有改旧文档**，也没有改 `WORKSPACES.md` 里的旧条目（只追加自己的）。 |
| G12 🟡 | 证据目录规则过时：`CLAUDE.md` 写 `/Volumes/80G/...`，实际因写入慢已改用 `/Volumes/KIOXIA/archive/evidence/`。 | 总监决定 §7.3-4 | 入口文档用 KIOXIA；`CLAUDE.md` 没动。 |
| G13 🟡 | 公开制品此前没人从头到尾自己校验过。 | — | **已补** `tools/verify-public-release.sh`，实跑通过（amd64、arm64：签名、篡改拒绝、Agent/镜像摘要、平台、未过期）。 |

## 2.1 补充处理（2026-11-09，应负责人要求“该修的修”）

- **G3 已修**：用本地 develop `c0ab14d1` 构建 `thingspanel-backend-ha-ui:develop-c0ab14d1`（ID `sha256:018b6b5d94f7…`），验收栈只替换后端容器，健康检查通过；覆盖文件已备份（`.bak-20261109`），旧镜像保留可回滚。
- **G1 已修**：负责人提供本机测试超管账号后，在页面点 Retry，generation 6、`enabled/ok`、身份未吊销、容器重启 0 次（`check-test-env.sh` 核对）。稳定性（A10/A13）未测。
- **更正**：我上一轮说“1.3 认不出三方接入设备”说得太满，T7b 自己写了没做对照。已补做干净的对照实验（见入口文档 §4.1）：结论仅限“同栈同范围同插件，只换 Broker 镜像，v1.3.0 连不上插件设备，develop 线镜像可以”。
- **新发现**：插件被 Broker 拒绝后不会自动重连，需重启插件容器（入口文档 §7 的 7d）。
- **Broker 已处理**：栈里 Broker 原是 v1.3.0 镜像（历史上会让插件设备 CONNACK 5）。换成本机 `thingspanel-mosquitto:ph1-520a3222`（develop 线，T7b 用的同一个镜像；Broker 相关目录 `deploy/mosquitto-community`、`cmd`、`internal/brokercontrol`、`sql/broker` 自 `520a3222` 起在 develop 上无变化）。换后 broker-owner 健康。
- **端到端实测（演示 HA，租户 A）**：租户页面建接入点、选 3 个实体建设备 ✅；`prepare.py --sync-scope` 把平台账号范围从 0 同步为 3 台设备 ✅；上行 ✅（43.3）；下行 ✅（off→on→off，回执 `result:0`）；租户 B 隔离 ✅。起了主机后台 `scope_sync` 监视进程。新发现见入口文档 §7 的 7a–7c。
- **镜像发布没做**：本机 docker 只登录了 Docker Hub，没有阿里云 / ghcr 凭据，我不会输入凭据；同事在这台 Mac 上测试也用不着发布。发布（P4）留给你。
- 本文件之前的提交：`05c08404`（本地，未推送）。
- `WORKSPACES.md` 里有 19 条指向已不存在目录的旧登记（多为已合并清理的工作线），**没改**，它们是你的记录，清理前该先对账。

## 3. 这次补了什么（都在 `docs/` 下，未提交）

| 文件 | 作用 | 验证情况 |
|---|---|---|
| `docs/HA-PLUGIN-HANDOVER.md` | **入口文档**：目标、链路图、被测对象表、红绿灯、环境、测试 HA、怎么测、已知问题、规则、资料索引 | 所有内部链接目标已确认存在；事实性断言（镜像内容、栈状态、目录/公钥）均已实测，实测不了的在文中标了“未验证” |
| `docs/ha-plugin-handover/TEST-CASES.md` | P0 准备 + A 组 13 条安装 + B 组 9 条租户接入与数据，含期望结果、在哪看、证据与失败记录模板 | 步骤由历史验收记录和代码路径推出，**没有实际执行 B 组**（见下） |
| `docs/ha-plugin-handover/tools/verify-public-release.sh` | 只读校验公开制品 | 实跑通过；脚本内置公钥并核对指纹 |
| `docs/ha-plugin-handover/tools/check-test-env.sh` | 只读体检栈的安装记录、身份、服务登记、事件 | 对验收栈实跑，读出了 G1 的状态 |
| `docs/ha-plugin-handover/tools/ha-demo/*` | 演示 HA 的 compose（可选并入栈网络）+ 配置 | 实跑：容器起来、`/api/onboarding` 200、demo 集成加载；已 `down -v` 清掉。**没走完向导和创建令牌** |
| 本文件 | 缺口分析 | — |

仓库外：给 `67f4afe9` 打了标签 `archive/ha-ui-acceptance-stack-20261109`（本地）。没有改任何容器、卷、栈目录、其他仓库。

## 4. 只有你能办的事（移交前）

1. **决定被测后端基线**：用本地 develop `c0ab14d1`，还是发布线的 `wp/t7-ha-release-backport`（`91b1e958`）？本地与远端 develop 不是一条线，后端主目录还有未完成的 cherry-pick。
2. **决定验收栈怎么处理**：Retry 恢复现状（会测到失联恢复），或用基线重新构建后端镜像（推荐）后重装。重建时要在记录里写镜像 ID。
3. **准备账号**：平台超管 1 个、租户账号 2 个（含接入点管理权限），用完轮换或停用；**不要写进任何文档**。
4. **决定同事在哪测**：在这台 Mac 上（同局域网用 `http://<Mac IP>:58800`，注意资源中心链接里的 `127.0.0.1` 只指向访问者自己的电脑），还是各自的机器（则路线 B，阻塞于 P4）。
5. **P4**（只有你能做）：推 SDK、发后端/前端/Broker 多架构镜像、建正式信任根。不做的话干净机器路径一直走不通。
6. 决定是否把以下合并/清理排期：后端主目录未完成的 cherry-pick；各候选分支合入；`WORKSPACES.md` 里已不存在的 `wp-plugin-install-ui-8bf83e` 等旧条目。

## 5. 没做 / 没验证

- **没有实际执行 A、B 两组用例**（那是同事的活）。B 组路径（租户页面新建接入点）是否真通，我没有试，所以用例里标了“没验证过”。
- 没有启动或修复验收栈，没有重建任何镜像，没有 Retry。
- 演示 HA 没走完向导和令牌创建；实体清单取自 T7b 记录。
- `G1` 与 `G3` 的因果没验证；`develop` 的测试只是单测。
- 没有跑 amd64 实机，没有测 MySQL。
- 没有改旧文档、`CLAUDE.md`、`WORKSPACES.md` 里的旧条目；没有改代码。
- 没有提交；文件都在 worktree 里待你决定。

## 6. 工作区

`.claude/worktrees/wp-ha-handover` ← 分支 `wp/ha-handover`。合并（或决定不合并）后当天删除分支和 worktree，并更新 `docs/WORKSPACES.md`。

## 7. 清理清单（2026-11-09，逐项取证；**7.1 已于负责人确认后执行**：5 个容器、1 个网络、16 条登记行已删，7.2 全部保留）

### 7.1 可以删（已确认）

| 对象 | 为什么可以删（证据） | 删完的影响 |
|---|---|---|
| 容器 `tp-agent-prepared-288f94e3…`、`…4afe2038…`（镜像 r2 候选，网络 `tp-codex-ha-net-20261106`，2026-11-06 退出码 0） | 已退出；所有 Agent 的状态文件和 compose 文件里都搜不到它们；所属网络里没有任何运行中的容器；r2 候选已被公开 1.0.0 取代 | 无。日志本来就读不出（日志驱动不支持读取） |
| 容器 `tp-agent-prepared-c6acd118…`、`…97a87a08…`、`…8481b53a…`（2026-11-08 的公开分发冒烟，退出码 0） | 已退出；它们所在的三个网络（`ha-updater-nov8c`、`ha-public-nov8b`、`ha-smoke-nov8a`）**已经不存在**，容器没法再启动；引用它们的只有 `/Volumes/KIOXIA/CodexTests/ha-public-dist-review-42436565819/…/agent/state/state.json`，那是冒烟的证据快照，**保留不动**；对应的 Agent 没有在运行 | 无。冒烟证据（状态文件、`PUBLICATION-MANIFEST.md`）不受影响 |
| Docker 网络 `tp-codex-ha-net-20261106`（要在上面两个容器删除之后） | 0 个容器在用、无标签、没有任何 compose 文件或运行中的 Agent 引用它 | 无 |
| `docs/WORKSPACES.md` 里 16 条登记行：`phase1-progress-handoff`、`wp-kioxia-config-20261105`、`wp-mvp-dispatch-20261105`、`wp-plugin-install-e5e1f9`、`wp-t1-0-relational-b140f7`、`wp-plugin-install-ui-8bf83e`、`wp-ha-plugin-target-docs-20261106`、`wp-t7-ha-catalog-renewal`、`wp-t1-a1-platform-start`、`wp-t4-plugin-identity-3cf9d2`、`wp-p2-fix`、`wp-p2-fix2-form-brackets`、`wp-p3-device-ack-timeout`、`wp-p3-real-broker-20261105`、`wp-t0-work-rules`、`wp-ph1-s1-core-c02f8c` | 这些目录**都已不存在**，对应分支**也都已不存在**；develop 的合并记录里能找到对应合并（P2、P2-fix、P3、T0、T4、T1-0、T1-A1、PH1-S1、plugin-install-ui、t7-ha-catalog-renewal、kioxia、phase1-progress、ha-plugin-target、mvp dispatch），`docs/tasks/` 下有相应 DONE 文件。只是文字登记，删行不丢任何代码。**例外**：`wp-plugin-install-e5e1f9` 我没找到对应的合并记录，只有总监审计里“已被 T4 取代，可以归档”这一句，置信度低一档 | 登记表变干净；没有登记的目录“视为可清理”这条规则不受影响，因为这些目录本来就没有了 |

### 7.2 不能删（看着像垃圾，其实不是）

| 对象 | 为什么不能删 |
|---|---|
| 容器 `tp-agent-prepared-28560728…`（mvp-ha 栈，退出码 137） | 正在运行的 `tp-agent-mvp-ha-20261105` 的状态文件 `/Volumes/KIOXIA/tmp/mvp-ha-resume-20261105/agent/state/state.json` 还引用它；删了 Agent 下一轮巡检可能报错或重建 |
| 后端 worktree：`wp-db-compat-f6f630`（领先 develop 29 个提交，未合并）、`wp-missing-api-5613cb`（领先 32，未合并）、`wp-t1-a2-platform-dal-service`（领先 1，未合并，有 2 个未提交文件）、`wp-t1b-ota-fix-f44bb3`（领先 1，等企业版接收）、`wp-t7-ha-release-backport`（候选 `91b1e958`，发布要用）、`wp-t7-plugin-install-test-deploy`（领先 17，未合并）、`mvp-run-default-stack`（已合并，但有 2 个未提交文件，且 mvp 栈还在运行） | 都有未合并的独有提交或未提交文件，或者还有运行中的栈在用 |
| 镜像 `thingspanel-backend-ha-ui:20261106`、覆盖文件备份 `*.bak-20261109`、`*.bak2-20261109` | 回滚用，等同事验证新后端、新 Broker 没问题后再说 |
| `/Volumes/KIOXIA/archive/evidence/2026-11-09-HA-HANDOVER/` | 里面有本次的测试账号和演示 HA 令牌（0600），同事要用 |
| 演示 HA 容器 `ha-demo`、Broker 范围监视进程 | 同事测试要用 |
| 标签 `archive/ha-ui-acceptance-stack-20261109` | 保护那个不在任何分支上的验收提交，删了它可能被垃圾回收 |
| 后端主目录里没做完的 cherry-pick | abort 会丢暂存状态，需要你决定 |

### 7.3 怎么执行（你确认后）

```bash
docker rm tp-agent-prepared-288f94e3600b4bd6575c5315351facd5be646a7f tp-agent-prepared-4afe203862f32020563cff98f6c865f5d286efc5
docker rm tp-agent-prepared-c6acd118461359da8d5d5331b5d34eba29b4c196 tp-agent-prepared-97a87a08900e63bf1ead6bb1e59a49144f2420c9 tp-agent-prepared-8481b53a9556887f8ddc0c65f6752eb1bfac8cd0
docker network rm tp-codex-ha-net-20261106
```

## 8. 冒烟结果（2026-11-09，验收栈，基线：后端本地 develop `c0ab14d1`、Broker `ph1-520a3222`、前端 `web/dist`、公开 HA 1.0.0）

| 步骤 | 结果 |
|---|---|
| 超管点“安装”，装好显示“已启用 / 健康 ok” | ✅ 第 7 代、第 8 代各一次（第 7 代是“重试”，第 8 代是卸载后的全新安装，约 5 秒） |
| 租户页面建接入点、选实体建设备，上行、下行 | ✅ 租户 A，3 台设备；上行 HA 温度 93.3、103.3 进平台；下行灯 off→on→off，回执 `result:0`；租户 B 隔离 ✅ |
| 超管点“卸载” | ✅ 状态 `uninstalled/absent`，第 7 代身份吊销，插件容器退出码 0；设备 3 台、接入点 1 个、服务登记 1 行、历史 106 条全部保留；伪造令牌调心跳被拒（503）。没有单独测“真实的旧令牌” |
| 卸载后重装 | ✅ 第 8 代 `enabled/ok`，Agent 自己回收了第 7 代的旧容器，只有 1 个运行容器，服务登记没有重复；重装后上下行再验一次 ✅ |

**过程中的意外**：开始卸载前发现安装记录是 `failed / renewal_failed`——Agent 容器 09:21 退出（退出码 135）没有被重启，11:21 Postgres 也异常重启，后端续租 5 次失败后转 `failed`。手工 `docker start` 了 Agent 并点“重试”后恢复。已记入 `docs/TODO.md`。冒烟不依赖它，但同事测的时候如果页面一直转圈、目录接口 503，先 `docker ps -a | grep tp-agent-tp-ha-ui`，Agent 退出了就 `docker start` 它。
