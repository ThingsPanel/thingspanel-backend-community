# 磁盘与工作现场清理盘点（只读，2026-11-09）

> **前半部分是执行前的盘点；执行记录见最后的“H. 已执行”。** 每一类给出：是什么、多大、凭什么说能删、删了的后果、需要你确认什么。
> 依据项目 `CLAUDE.md` 第 12 条和 `/Volumes/KIOXIA/AGENTS.md`：删除之前先列清单，用户确认后再动手；KIOXIA 上的缓存“也一样先问”。
> 大小是 `du -sk -x` 实测，带超时；超时的标“未量”，不估算。

## 0. 先看结论

| 盘 | 状态 | 说明 |
|---|---|---|
| 内置盘 `/System/Volumes/Data` | **已用 176 GB / 228 GB（90%），只剩 21 GB** | **真正紧张的是这里** |
| 外置盘 `/Volumes/KIOXIA` | 已用 79 GB / 466 GB，剩 386 GB | 不紧张。**它是混用盘**，有影视、财务、个人资料，只清开发相关的，个人内容一律不碰 |

内置盘上和开发相关的大头（实测）：

| 项 | 大小 | 备注 |
|---|---|---|
| OrbStack（Docker 虚拟机数据） | 27 GB | 其中镜像 16.5、构建缓存 7.2、卷 3.6 |
| `~/Downloads/code` | 24.6 GB | 前端两个仓库各 6 GB+（node_modules 为主）、closeout 目录 3.6 GB、后端 2.2 GB |
| Claude 桌面应用自己的数据 `vm_bundles` | 11.1 GB | 应用自己管，**不在清理范围**，只是让你知道它占地 |
| `~/.codex` | 4.5 GB | 会话历史 1.5 GB、worktrees 1.0 GB（含 develop 集成线），**不动** |
| pnpm / npm / `~/.cache` 缓存 | 2.8 + 1.3 + 2.7 GB | 可重建 |

## A. 内置盘：能腾出地方的项（按“收益 ÷ 风险”排序）

| # | 对象 | 大小 | 凭什么说能删（已核实） | 删了的后果 | 需要你确认 |
|---|---|---|---|---|---|
| A1 | **Docker 构建缓存**（`docker builder prune`） | 7.2 GB，其中可回收 5.7 GB | 是纯缓存；`docker system df` 显示 237 条、**活跃 0 条** | 下次构建变慢（重新拉层） | 无，只需知会 |
| A2 | **Docker 里没人用的镜像**（`docker image prune`，只删悬空和未被任何容器使用的） | 约 2.0 GB 可回收 | 只删“未被任何容器（含已退出的）引用”的镜像；运行中的栈用的镜像不受影响 | 以后要用得重新拉或重新构建。本机构建的 `thingspanel-backend-ha-ui:*`、`thingspanel-mosquitto:*` 如果没有容器引用也会被算进去——**我会先列出清单，把要保留的排除** | 要你看一眼镜像清单 |
| A3 | **前端 6 份 `node_modules`**（只删依赖，不删代码） | 每份约 2.0 GB，共约 12 GB（主仓库 1、`wp-pui` 1、其下 `.claude/worktrees/` 4） | `pnpm install` 可重建，锁文件在仓库里；不涉及任何未提交的源码 | 下次要在那个目录跑前端时得重新安装（几分钟）。我建议**先只清 4 个 `.claude/worktrees/` 下的**（约 8 GB），主仓库和 `wp-pui` 暂时留着 | 同意清哪几份 |
| A4 | **pnpm 全局缓存** `~/Library/pnpm`、**npm 缓存** `~/.npm` | 2.8 GB + 1.3 GB | 纯下载缓存，丢了会重新下载 | 下次安装依赖要联网、稍慢 | 无 |
| A5 | `~/.cache` 下的 `puppeteer`、`chrome-devtools-mcp` | 0.55 + 0.42 GB | 浏览器自动化的下载缓存，会按需重新下载 | 下次用到时重新下载 | 无。**`codex-runtimes`（1.6 GB）是 Codex 的运行时，不动** |
| A6 | 两个 `thingspanel-closeout-*-20261102` 目录 | 2.45 + 1.2 GB | **不能确认**：不是 git 仓库，里面是 11-02 收尾时的后端/前端/安装器快照和同步状态；`WORKSPACES.md` 已登记为“未处理，需用户决定”。我没找到它们被任何容器或符号链接引用，但**不知道里面有没有独有的证据** | 如果里面有独有证据，删了就没了 | **要你决定**：确认已归档才删；或我先逐文件算哈希、对照 `/Volumes/KIOXIA/archive`，再给结论 |

**A1 + A2 + A3（只清 4 份）+ A4 + A5 合计约 17–18 GB**，内置盘可用空间会从 21 GB 升到约 38 GB，都是“可重建”的东西。

### 不列入清理的内置盘大项
- Claude 应用自己的 `vm_bundles`（11 GB）和 `Application Support`（共 12.7 GB）：应用管理，不是项目垃圾。
- `~/.codex/sessions`（1.5 GB）：会话历史，是你的记录。`~/.codex/worktrees` 里有 **develop 集成线**，不能动。
- `~/Documents/工作`（4.8 GB）、`foxmaildata`、`Downloads` 下其他个人文件。
- `~/Downloads/code/candidate-generator-training`（1.9 GB）：不是 ThingsPanel 项目，不归我判断。

## B. 外置盘 KIOXIA：开发相关

外置盘不缺空间，所以这部分**不急**，收益是整洁，不是腾地方。

| # | 对象 | 大小 | 核实结果 |
|---|---|---|---|
| B1 | **12 个按任务建的 Go/pnpm 缓存**：`.gocache-pui`(2.2)、`.gocache-pui-linux`、`.gocache-director`(1.4)、`.gocache-ph1-s1`(1.4)、`.gocache-p3-real-broker-20261105`(0.8)、`.gocache-p3-resume-20261105`(0.7)、`gocache-tp`(0.15)、`gocache-dbcompat`、`go-cache`、`.gomodcache-pui`(1.8)、`.gomodcache-ph1-s1`(1.8)、`.gomodcache-p3-real-broker-20261105`(0.8)、`.pnpm-store-ph1-s1`(1.5)、`.pnpm-store-takeover-20261102`(1.1) | 约 15 GB | **核实**：① 全局 Go 配置只用 `.gocache`、`.gomodcache`（`go env` 实测）；② 当前没有任何进程在用它们（`ps` 实测）；③ 各栈、临时目录里的脚本没有引用它们（搜过，唯一命中是依赖包 `patrickmn/go-cache` 的名字，误报）；④ 前端当前用的 pnpm 仓库是 `~/Library/pnpm/store/v10`，不是这两个 `.pnpm-store-*`。**保留**：`.gocache`(6.0)、`.gomodcache`(0.8)、`.pnpm-store`(1.9)——它们在 `AGENTS.md` 第 4 节的“不能动”清单里或被全局配置使用。**需要你同意**：`AGENTS.md` 要求缓存删除也要先问 |
| B2 | 已完成测试栈：`t5-stack`、`t7-stack`、`t7b-stack`、`t7c-stack`、`p1-stack`、`tp-i1`（1.9 GB）、`tp-a-access-test`（3.3 GB） | 约 6.5 GB | **未核实**：它们是历史测试现场，证据多半已归档（`archive/evidence/` 里有 T5/T7/T7b/T7c/P1 的目录），但**我没有逐个比对**。`tp-i1` 里的目录被某些 `git worktree` 引用（安装器仓库 `thingspanel-installer-ha-image-preload-20261106` 在 `KIOXIA/code`） |
| B3 | `pui-stack`（0.11 GB）、`tmp/`（2.5 GB，含 `mvp-ha-resume-20261105`、`ph1-s1-work` 等） | 2.6 GB | **在用**：`pui-stack` 有 3 个运行中的容器绑定它；`tmp/` 里 7 个运行中容器绑定它（mvp-ha 栈）。**不能删** |
| B4 | `docker-data/`（8 GB） | 8 GB | **不能删**：21 个已退出容器绑定它（`tp-develop-local` 的夹具，`AGENTS.md` 第 4 节明确列为“不能动”） |
| B5 | `CodexTests/`（1.3 GB） | 1.3 GB | **在用**：验收栈、演示 HA、发布验证证据都在里面，18 个运行中的容器绑定它 |
| B6 | `KIOXIA/code/`（22.6 GB） | 22.6 GB | 仓库和 worktree，被 `~/Downloads/code` 的 9 个符号链接引用，**第 4 节“不能动”**。里面各 worktree 的合并状态见下文 C，单独盘点 |
| B7 | `ZSIBOT`（4.7 GB）、影视、`财务电脑文件`、`物联网`、`股权设计` 等 | — | **个人资料，不碰** |

## C. worktree 与分支（5 个仓库共 45 个 worktree、79 个本地分支）

**一个重要前提**：下面的“已合并”是指合并进了**本地** `develop`（或前端 `origin/develop`）。后端本地 develop 领先远端 183 个提交、**从没推送过**；安装器本地 develop 和远端 develop 也不是一条线。所以“已合并”的分支删掉后，内容**只剩在本地 develop 上**，没有第二份。**建议在删任何分支之前，先把 develop 推到远端备份，或至少 `git bundle` 一份。**

### 后端（10 个 worktree）
| worktree | 分支 | 状态 | 建议 |
|---|---|---|---|
| `~/.codex/worktrees/.../` | `develop` | 干净 | **保留**（集成线） |
| 主目录 `thingspanel-backend-community` | detached `85b04e7b` | 20 个改动 + 没做完的 cherry-pick | **保留**，等你决定 |
| `mvp-run-default-stack` | `wp/mvp-run-default-stack` | 已合并，2 个未提交文件，mvp 栈还在运行并引用它 | **保留** |
| `wp-db-compat-f6f630` | `wp/d1-dbx-returning` | **未合并，领先 29** | 保留。T1 卡片从它取场景，GORM 工作要用 |
| `wp-missing-api-5613cb` | `claude/wp-missing-api-5613cb` | **未合并，领先 32**，“补接口”线，已暂停 | 打归档标签后可删 worktree |
| `wp-t1-a2-platform-dal-service` | `wp/t1-a2-platform-dal-service` | 未合并，领先 1，2 个未提交 | 保留。**GORM 的 A2 卡** |
| `wp-t1b-ota-fix-f44bb3` | `enterprise/ota-push-self-count-fix` | 未合并，领先 1 | 保留，等企业版接收 |
| `wp-t7-ha-release-backport` | `wp/t7-ha-release-backport` | 未合并，领先 1 | 保留（发布备选） |
| `wp-t7-plugin-install-test-deploy` | `wp/t7-plugin-install-test-deploy` | 未合并，领先 17（通知线） | 保留，需要问是谁的 |
| `wp-ha-handover` | `wp/ha-handover` | 已推送 | 保留到同事测完 |
另有 3 个无 worktree 的未合并分支：`claude/wonderful-swirles-a89fac`、`rescue/main-worktree-20261104`（主目录 11-04 快照）、一个 detached 指针。**都保留**。

### 安装器（17 个 worktree、21 个分支）
- **已合并且干净，可删**（7 个）：`wp-i1`、`wp-p1`、`wp-p1-fix`、`wp-ph1-s2`、`wp-t5`、`wp-t6`（都是 `~/Downloads/code/thingspanel-installer-wp-*`），以及 `develop-review` 下的 `wp-mvp-config`。总监审计 §7.7 里点名的是 `wp-i1`、`wp-t5`、`wp-t6`（“都已合入安装器 develop，可以删除”）；`wp-p1`、`wp-p1-fix`、`wp-ph1-s2`、`wp-mvp-config` 是我用 git 核对出来的。依据：`git merge-base --is-ancestor` 对本地 develop 为真、`git status` 无改动。
- **未合并，保留**：`wp-pui`（领先 8）、`wp-t6-ha-public-release`（21，有 1 个改动）、`wp-t6-ha-develop-validation`（23）、`wp-t6-cloudflare-worker-assets`（22）、`wp-t6-plugin-distribution`（7）、`codex-ha-plugin-public-site`（3，**公网站点从它发布**）、`wp-t6-ha-signing-dispatch`（1）、`ha-image-preload`（8，有 6 个改动）、主目录 `feat/thingmodel-api`。
- 已合并但没有 worktree 的分支 2 个：`codex/fix-compose-network-overlap`、`codex/installer-release-manifest-20261102`，可删。

### 前端（12 个 worktree、28 个分支）
- **已合并且干净**：`ha-platform-origin-pui`、`ha-redirect-permission-20261109`（两个都在 `wp-pui/.claude/worktrees/` 下，各占 2 GB `node_modules`）。⚠️ **后者是今天建的，可能有会话正在用，先问再删**。
- **已不存在的路径**：`/Volumes/80G/.../frontend-api-b7318932` 标记为 `prunable`（目录已没了），`git worktree prune` 只清登记，不碰文件。
- 已合并但没有 worktree 的分支 13 个（`codex/community-roles-thingsvis`、`codex/native-device-metadata`、`codex/notification-frontend` 等），可删。
- **未合并，保留**：`wp-t7-plugin-list-urgent`（5 改动）、`wp-t8-ha-public-frontend`（4 改动）、`wp-pui`（5 改动）、`ha-r-deeplink`、`frontend-develop-merge`、`~/.codex/worktrees/ce71`（9 改动）、`ph1-s1-work/frontend`（4 改动）、主目录 `master`（38 个改动）、`tp-automation-frontend`（4 改动）。

### HA 连接器（5 个 worktree）
- `t6`、`t6-public-release`：未合并，是 T6 更早的迭代，**看起来已被 `t6-public-artifact` 取代，但未确认**，保留到 `0b3240e` 合入 main 之后再说。
- `ha-plugin-readme-install`（我今天用的）：PR #1 已合并，可删 worktree 和分支。

### kernel（`iot-api-encore-kernel`）
11 个分支，其中 2 个已合并、8 个未合并，主目录有 26 个未提交改动（`WORKSPACES.md` 已登记）。**这个仓库不在本次范围，不动。**

## D. Docker 对象

| 对象 | 数量 / 大小 | 核实 | 建议 |
|---|---|---|---|
| 已退出容器 | 67 个无 compose 项目的 + 若干项目内的，总计只占 143 MB | 多数是已结束的测试：`t1a1-*`、`t1a2-*`、`p2-pg`、`p3-device-ack-pg`、`pui-*`、`tp-closeout-*`、`sqldb-*`（Encore 自动建的 9 个 PG）、`tp-takeover-b-horizon-minio`、`tp-codex-ha-agent*` 等 | 容器本身几乎不占地方，**收益小**。它们**可能挂着匿名卷（数据库数据）**，我没有逐个确认卷里有没有要保留的证据，所以**不建议批量 `container prune`**；要删就逐组问你 |
| 运行中的栈 | `tp-ha-ui-acceptance`、`thingspanel-community-v130`、`pui-stack`、`ph1-s1`、`mvp-ha`、`ha-demo`，以及 k8s（OrbStack 自带） | — | **全部不动** |
| 卷 | 88 个，总 3.6 GB，约 1.4 GB 可回收 | 可回收的是“没有任何容器引用”的卷 | 同上，先看清单，不批量删 |
| `exited` 的 `tp-agent-prepared-…285607282d8c…` | 1 个 | **被 mvp 栈运行中的 Agent 引用**（今天已核实） | **不能删** |

## E. 不要动的东西（再强调一次）
- `/Volumes/KIOXIA/AGENTS.md` 第 4 节全部路径，尤其 `.gocache`、`.gomodcache`、`docker-data/tp-develop-integration/…`、`code/*` 里被符号链接引用的仓库。
- 所有运行中的容器和它们绑定的目录。
- 验收栈 `CodexTests/thingspanel-ha-ui-acceptance-20261106` 和 `archive/evidence/2026-11-09-HA-HANDOVER`（里面有测试账号和令牌，同事要用）。
- `~/.codex/worktrees` 里的 develop、`~/.codex/sessions`。
- 个人资料。

## F. 建议的执行顺序（每一步都先给你清单，你确认后再做）

1. **先备份 develop**：把后端本地 develop 推到远端（领先 183 个提交，快进），安装器同理（先确认和远端 develop 的关系）。这样“已合并的分支”删了也有第二份。如果暂时不想推，至少 `git bundle create` 到 `archive/`。
2. **内置盘腾地方（约 17–18 GB）**：A1、A2、A4、A5，再加 A3 里 4 份 `node_modules`。
3. **安装器 7 个已合并 worktree + 2 个分支、前端 13 个已合并分支 + prunable 登记、连接器的 readme worktree**：逐个 `git worktree remove` / `git branch -d`（`-d` 只删已合并的，没合并会拒绝，是一道保险）。
4. **未合并但已暂停的 `wp-missing-api`**：先打 `archive/` 标签，再删 worktree。
5. **KIOXIA 缓存 B1（约 15 GB）**：你同意后再删；这一步对内置盘没有帮助，纯粹是整洁。
6. **A6 closeout 目录、B2 历史栈**：先让我算哈希对照 `archive/` 里有没有独有内容，再给你结论。

**防止再乱**（做完清理之后）：
- 我可以写一个只读脚本，一条命令列出各仓库 worktree 的状态、缓存大小，每周跑一次。
- 规定：每个任务合并当天删自己的 worktree 和分支（`CLAUDE.md` 第 11 条已有）；Go 只用一套全局缓存，不再给每个任务建 `.gocache-<任务名>`；`node_modules` 不放进 `.claude/worktrees` 的长期 worktree。

## G. 我没能核实的
- `~/Downloads/code` 里除上面列出的以外，还有几十个小目录（每个都不到 0.3 GB），没有逐个看。
- `KIOXIA/code`（22.6 GB）里各 worktree 和仓库的详细状态没盘点。
- 已退出容器的匿名卷里有没有要保留的数据。
- 为什么 Agent 容器 09:21 退出、Postgres 11:21 异常重启：内置盘 90% 满是否是诱因，**没有证据**。
- A6 两个 closeout 目录里有没有独有证据。

## H. 已执行（2026-11-09，负责人确认“按建议顺序执行”之后）

| 步骤 | 做了什么 | 结果 |
|---|---|---|
| 1 备份 develop | 后端本地 `develop` 快进推送到远端：`9d071b77..c0ab14d1`（183 个提交；推送前扫过新增行，没有密钥）。安装器：本地 develop 是远端 develop 的**祖先**（远端领先 7），所以已合并的安装器分支内容远端本来就有 | 后端 develop 现在有远端备份 |
| 2 腾内置盘 | Docker 构建缓存 6.2 GB 全清；删 21 个没有容器引用的镜像（约 6.8 GB：`mysql:8`、`tdengine/tsdb`、`tp-i1-*`、3 个悬空镜像等；保留 `thingspanel-homeassistant:t6-*`）；删 3 份前端 `node_modules`（`wp-t7-plugin-list-urgent`、`wp-t8-ha-public-frontend`、`ha-platform-origin-pui`，源码和未提交改动原样在）；`pnpm store prune`（1.5 GB）、`npm cache clean` | 内置盘可用 **21 GB → 32 GB**（已用 90% → 84%）。**没动**：今天新建的 `ha-redirect-permission-20261109` 的 `node_modules`、浏览器自动化缓存、Codex 运行时 |
| 3 worktree 与分支 | 安装器：删 7 个已合并且干净的 worktree 和对应 7 个分支，再删 `codex/installer-release-manifest-20261102`、`codex/fix-compose-network-overlap`（都核对过是 `origin/develop` 的祖先）；前端：`git worktree prune` 清掉 1 条失效登记，删 13 个已合并分支；连接器：删 README worktree 和 2 个分支（PR #1 已合并，文件与 `origin/main` 逐字节一致） | 安装器 worktree 17→10、前端 12→11、连接器 5→4。**被拒的 `-d` 改用 `-D` 是因为 `-d` 按当前 HEAD 判断；我已用 `merge-base --is-ancestor` 对远端 develop 验证过** |
| 4 补接口线 | 给 `claude/wp-missing-api-5613cb`（tip `f3d93500`，领先 develop 32 个提交）打标签 `archive/cleanup-20261109-missing-api`，删 worktree；**分支本身保留** | 后端 worktree 10→9 |
| 5 KIOXIA 缓存 | 删 14 个按任务建的缓存（`.gocache-*`、`.gomodcache-*`、`gocache-tp`、`gocache-dbcompat`、`go-cache`、`.pnpm-store-ph1-s1`、`.pnpm-store-takeover-20261102`），共 13.9 GB；**保留** `.gocache`、`.gomodcache`、`.pnpm-store`；`go env` 仍指向保留的那两套 | KIOXIA 可用 386 → 399 GB |
| 6 closeout 目录 | **只核对，没删**：`thingspanel-closeout-internal-20261102` 下 6 个目录都是 git 仓库，**都有未提交改动**（backend 504 个、installer 37、thingsvis 47、horizon 18、sdk 9、frontend 2）；sdk 的提交（`f183a19c`）不在正式 SDK 仓库里；horizon、thingsvis 没有对应的正式仓库可比对。`closeout-work` 里有 `minio-source`、`test-results`、`browser-fixture`（含 node_modules）和 `*.env` | **不能删**。里面很可能有独有工作，需要你决定是否归档；只有 `closeout-work/gomodcache`（1.08 GB）是纯缓存，可以单独删 |

**没做**：A6 之外的 KIOXIA 历史栈（`t5/t7*/p1-stack/tp-i1/tp-a-access-test`）没动，因为我没逐个比对证据是否已归档；没动 Docker 已退出容器和卷；没动 `ha-redirect-permission-20261109`。

### 清理过程中发生的事故：验收栈 Postgres 崩溃（和清理的关系**没有证实**）
11:44（北京时间）验收栈的 Postgres 写事务日志时 `PANIC: could not write to log file … I/O error`，随后容器里找不到 `base`、`global/pg_control`（宿主机上这些文件一直都在）。这是**今天第三次**同类事故（09:21 Agent 退出码 135、11:21 Postgres 异常重启、11:44 这次）。我 12:17 备份了数据目录（`scratchpad/pgdata-backup-20261109`，111 MB）后 `docker start`，Postgres 自动恢复，安装记录仍是第 8 代 `enabled/ok`，重新跑上下行通过（温度 113.3、回执 `result:0`）。
- **和清理有没有关系**：KIOXIA 的大批量删除可能和这次崩溃同一时段，但前两次事故发生时我并没有在做这类操作，所以**不能下结论**。
- **更像是共同原因**：验收栈的 Postgres 数据目录绑定在外置 USB 盘 `/Volumes/KIOXIA/…/data/postgres`，经 OrbStack 的文件共享挂进容器。容器侧读写出 I/O 错误，宿主机上文件却完好，指向挂载层/USB 抖动，而不是数据损坏。**这是推断，不是证实。**
- **建议**：把验收栈的 Postgres（和 Redis）数据改放 Docker 命名卷（内置盘现在有 32 GB），这样不再经过外置盘的挂载。这会改动栈的 compose，等你决定。
