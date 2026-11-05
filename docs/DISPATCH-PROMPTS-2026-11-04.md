# 剩余任务派工提示语（2026-11-04 晚）

> 用法：把**第 1 节的通用前言**和**第 3 节中某一张卡的提示语**一起复制给执行人（人或新的 AI 会话），一张卡对应一个执行人。
> 执行人完成后会写 `docs/tasks/<卡号>-DONE.md`，然后停下。你只需要告诉总监会话“检查 <卡号>”，总监按第 4 节的清单验收，合格后合入 develop。
> 进度总表和决策记录见 `docs/PROJECT-AUDIT-2026-11-04.md` §7。

> **2026-11-05 用户调整优先级：第一阶段先让默认系统跑起来。** 本页第 0 节和第 2 节是当前派工依据；第 3 节保留原卡片作后续参考，不代表现在要全部开工。已经完成的成果和进行中的工作树保留，不能据此说 MySQL、TDengine 或整套任务已经完成。

---

## 0. 第一阶段：默认栈可运行（2026-11-05）

**目标**：以 PostgreSQL + TimescaleDB 的默认组合，在隔离环境里启动平台，让用户能登录、接入设备、看到真实上报及历史，并验证命令和 Home Assistant 接入的核心链路。先用当前已存在的成果；只修复阻止这条路径运行、产生错误结果或触及安全底线的问题。

**最小验收，以实际运行证据为准：**

1. 使用明确的本地版本和默认配置，在独立测试环境启动数据库、Redis、平台及必要的 Encore/Broker 服务；记录版本、启动方式和健康状态。优先复用已验证的安装流程，不能改动用户已有数据。启动和迁移失败必须明确报错。
2. 用户能登录；按现有最短流程创建设备或绑定模板。用两个测试租户证明设备不能被另一租户查看或操作；插件入口继续要求受管身份，不能为了冒烟放宽权限。
3. 使用真实 MQTT 或真实 HA 测试设备上报，平台能看到当前数据和历史；核对默认 TimescaleDB 的真实写入与读回。执行一次命令并收到真实回执；故意不返回回执时，界面/API 不得显示成功。历史、命令状态和实际结果不一致算失败。
4. 按当前安装器接入 HA，验证上报与下发；若外部 HA 环境暂不可用，记录具体障碍和已有证据，该项标记未验证，不把整个闭环写成通过。重启必要服务后再次确认关键数据和配置仍可用。
5. 只记录最少、可复核的证据：具体版本、命令/操作、结果与失败原因；日志和截图放仓库外，证据不得包含令牌。单元测试、局部 fixture 或旧版本测试不能代替本次整栈验证。

**必须保持**：现有认证、租户隔离、受管插件身份、真实凭据不进日志；PG 已执行迁移不改；不新增 SQL 翻译器、API 桥或 OTA 代码。不以关闭鉴权、伪造设备回执、mock 替代真实闭环。若发现当前版本在真实路径泄露凭据或误报成功，修最小根因并加直接回归；不扩展成通用攻防项目。

**先暂停，不删除成果**：MySQL 全量适配（T1-A2/A3/A4、B/C/D）、TDengine 转正及语义矩阵（T3）、双库切换/MySQL 界面冒烟（T2）、P2b 加密设计、P5 优化、非核心清理 E3，以及 T8 中不影响默认栈启动的更换组件指南、完整四组合和干净机器升级矩阵。第一阶段通过后再按产品优先级恢复；这些项目没有完成，不能宣传为已支持。A1 已合入的配置变更应检查安装器与实际栈是否匹配，修复启动阻塞即可，不借机扩大数据库改造。

**进行中工作如何处理**：

- `wp/t1-a2-platform-dal-service`：暂停新增 MySQL 全矩阵工作；保留现有提交、未提交改动和测试证据，不清理、不合并。仅当实测证明其中某个修复是默认 PG 闭环的阻塞项，才单独评估并以最小变更处理；不得整包合入未经审查的 WIP。
- `wp/p3-device-ack-timeout`：保留现有现场，优先验证并完成“不回执不显示成功”这一条。只实现必要的持久状态、正常回执和超时竞争处理，避免迟到回执造成无限重试；真实 PG 和一个真实 Broker 或可控 Broker 场景验收。界面若只能显示通用失败，应明确文案和限制，不宣称完整 UX 优化。
- P1/P2 已合入的安全和安装器修复保留；不再主动扩展通用脱敏器和罕见输入矩阵。P1 新镜像缺 `33.sql` 时无法区分旧镜像的风险先记录；若第一阶段实际镜像缺表或迁移无法完成，视为启动阻塞并修复。

**执行纪律**：每张当前任务只写一段交付目标、最多 3–5 个关键验收场景和预计耗时；超出预计时间或遇阻，报告已验证结果、阻碍和下一步，不无限扩项。范围外问题记待办，除非会阻止启动、造成核心链路误报或破坏安全底线。已通过且未受改动影响的测试不重复运行；涉及权限、持久数据和状态竞争的必要验证不能跳过。每次向产品经理汇报用户能做什么、还做不到什么、阻碍及下一步，不用代码行数或测试数量冒充产品进度。

---

## 1. 通用前言（每张卡都要附上）

```
你在 ThingsPanel 项目里执行一张任务卡。后端仓库位于 /Users/junhong/Downloads/code/thingspanel-backend-community，主线是 develop 分支。

开工前先读三样东西：
- 仓库根目录的 CLAUDE.md（工作规则）
- docs/PROJECT-AUDIT-2026-11-04.md 的 §1（架构规则 A1–A5）和 §7（当前进度）
- 你这张卡引用到的文档

只做下面这一张卡。

工作方式：
- 从 develop 新建自己的 worktree，命令：git -C <仓库> worktree add .claude/worktrees/<卡号> -b wp/<卡号>-<短名> develop。不要在主目录或别人的 worktree 里改东西。
- 测试容器的名字以 <卡号小写>- 开头；不使用共享的 scripts/dev-databases.sh 容器。删除任何容器、卷或目录前列清单等用户确认。
- 新证据放到 /Volumes/KIOXIA/archive/evidence/2026-11-<日>-<卡号>/，不进仓库。Go 缓存使用 KIOXIA 上的独立任务目录；复制完成并核验前不覆盖已有缓存、不清理旧盘。不要使用 /Volumes/80G（写入很慢）。

禁止事项：
- 不推送、不合并、不发布镜像，不用 git stash。
- 不 kill 不是你自己启动的进程。
- 令牌、密码不能写进日志、命令行和提交。
- 关系库只用 GORM，禁止新增对 pkg/sqldialect、encore-api/pkg/dbx 的调用。PG 已执行过的迁移一字不改。
- 社区版不包含 OTA，不要改 OTA 代码。

范围控制：
- 发现范围外的问题，写进 docs/TODO.md，不要顺手去修。
- 一张卡做了 3 天还没完，就停下来，把它拆小。

交付：
- 提交到你自己的分支。
- 写 docs/tasks/<卡号>-DONE.md，包括：分支名；git log --oneline develop..HEAD 的输出；测试命令和结果（跳过的测试要写明原因）；没做完的部分。
- 不要把单元测试通过写成端到端验收通过。
- 写完后停下，回复“<卡号> 已交付，等待总监审查”。
```

---

## 2. 当前施工顺序与并行规则（2026-11-05 覆盖旧排期）

**限额：后端仓库同一时间最多 2 条改代码的线。** 安装器仓库、HA 插件仓库各算独立的一条线。

| 顺序 | 工作 | 完成条件 |
|---|---|---|
| 1 | 冻结 A2 等非默认栈工作，保留现场；盘点可用的隔离默认栈、实际版本与配置 | 明确启动方式、所需外部服务及与 A1 配置变更的差异，不改用户数据 |
| 2 | 启动默认栈并完成登录、设备、真实遥测/历史最小闭环 | 按第 0 节逐项记录真实结果；仅修复阻塞问题 |
| 3 | 完成 P3 的必要状态修复，并验证正常回执与无回执；验证 HA 接入 | 界面/API 不误报成功，真实路径有证据；已有成功路径不退化 |
| 4 | 对照第 0 节复核、记录遗留并形成可运行交付 | 明确未验证和未发布事项；之后再决定是否恢复 MySQL/TDengine 等卡片 |

冲突提醒（恢复后续卡片时仍有效）：
- A2、A3 与 T3 都会改 `internal/dal`、`internal/storage`，**不能并行**。
- P2、P3 会改 `internal/api`、日志中间件和 downlink，与 A 系列可能碰到同一批文件，开工前先 `git diff develop --stat` 确认没有重叠。

---

## 3. 各卡提示语

### P3 Broker 丢弃命令时，平台要能看到失败

```
卡号 P3。前置：P2 已合入 develop。

背景：
T7b（docs/tasks/T7b-DONE.md 第 5 节第 3、4 条）发现：当平台服务账号的 Broker 权限范围里没有插件设备时，平台下发的命令会被 Broker 静默丢弃，但命令状态仍然停在 broker_acknowledged，界面显示“成功”，没有任何报错。

任务：
在命令发出后约定的时间内（时长可配置，默认值写明理由）没有收到设备回执，就把命令标记为超时失败，界面和 API 能看到这个状态。先读 internal/brokercommands、internal/downlink、命令状态机，弄清现有的状态流转；已有的成功路径不能改变行为。

验收：
1. 复现“平台账号范围里没有插件设备”的场景（可以用 mock 的 broker 或真实 Broker），命令最终状态是失败或超时，不是成功。
2. 正常路径下回执照常写入。
3. 有单元测试，并在真实 PG 上跑通集成测试。
```

### P2b voucher 加密存储（只出设计，不写代码）

```
卡号 P2b。前置：P2 已合入 develop。

背景：
service_access.voucher 目前明文存储了第三方凭据（例如 Home Assistant 的长期访问令牌）。

任务：
写设计文档 docs/VOUCHER-AT-REST-DESIGN.md，内容包括：
- 加密方案：信封加密或平台主密钥；
- 密钥从哪里来、怎么轮换、丢失后怎么处理；
- 已有明文数据怎么迁移；
- 插件取配置时怎么解密；
- PG 和 MySQL 两边的影响；
- 对安装器的要求。
给出 2–3 个方案的对比和一个推荐方案。不写代码。

交付：设计文档，以及 docs/tasks/P2b-DONE.md。
```

### T1-A1 至 T1-A4（平台根模块，后端线 2，按顺序做）

```
卡号 T1-A<n>（n = 1、2、3、4，按顺序做，上一张合入 develop 后再开下一张）。

任务：
执行 docs/T1-CARDS.md 中的卡片 T1-A<n>。范围、要改的文件和验收标准都以那张卡为准。

先读：
- docs/RELATIONAL-ADAPTER-DESIGN.md
- docs/T1-CARDS.md 的 §0、§1、§7
- docs/tasks/T1-A0-DONE.md 及已完成的 A 卡的 DONE 文档

总监已做的决定：
- 旧配置键（db.driver、db.psql.*、db.mysql.*、TP_*_DB_DRIVER）不保留兼容；
- 一个部署只有一个 relational.driver；
- 每个 Encore 资源一个 MySQL 库；
- 咨询锁一律用 pkg/relational.LockTx。

差分测试场景从分支 wp/d1-dbx-returning（worktree .claude/worktrees/wp-db-compat-f6f630）移植过来，只拿测试场景，不拿翻译器。

各卡的特别要求：
- A1：更正设计文档 4.4 的锁范例（改成 INSERT … ON DUPLICATE KEY UPDATE）；写出结论：平台是否读写 automation 表；为“MySQL 配置下 broker 控制面拒绝启动”写测试。不要对根模块执行 go mod tidy，除非已经有代码引用 pkg/relational。
- A2、A3：不能和 T3 同时进行。
- A4：在 PG 和 MySQL 上分别跑 scripts/smoke_mvp1.sh，并跑 develop 的全部 PG 测试。
```

### T1-B1 至 T1-B6（devicemetadata，后端线 1，同一条分支上按顺序做）

```
卡号 T1-B<n>（按 B1、B2、B3、B4、B5a、B5b、B6 的顺序）。全部 B 卡都在同一条分支 wp/t1-b-devicemetadata 上依次提交。每做完一张，写对应的 docs/tasks/T1-B<n>-DONE.md，然后停下等审查；审查通过后，在同一分支上继续下一张。

前置：T1-A1 已合入 develop。

任务：
执行 docs/T1-CARDS.md §3 中的卡片 T1-B<n>。验收用的差分场景来自 wp/d1-dbx-returning，数量如下：存储 19、分组 35、设备 23、模板 18、凭据 17、导出 22、模拟器 19、回执 15、命令 19、跨租户 52、网关守卫 20、模板选择 15、校验函数 74 个取值。

要求：
- MySQL 上没有行级安全（RLS），跨租户保护必须在 repository 里显式加 tenant 条件。
- 触发器和 CHECK 的替代实现，写在 MySQL 迁移文件里手工维护，放在 encore-api/devicemetadata/migrations-mysql/。
- Encore 的 sqldb 声明和 PG 迁移保持原样，一字不改。
- encore test ./devicemetadata/... 必须全部通过。
```

### T1-C1 至 T1-C11（其余 Encore 模块，每张卡单独一条分支）

```
卡号 T1-C<n>。

前置：
- 所有 C 卡：T1-A0 已合入（已满足）。
- C1（thingmodel）：还要等 T1-B1 合入。
- C2（platformconfig）、C11（authcore）：还要等 T1-A1 合入。

任务：
执行 docs/T1-CARDS.md §4 中的卡片 T1-C<n>。验收用的差分场景来自 wp/d1-dbx-returning：物模型 31、北向 24、文件 48、通知 31、connectorcatalog 15、automation 76 步加 80 步，另外还要通过空库启动测试。

要求：
- C9 和 C10（automation）：在 DONE 里写清 automation schema 的唯一迁移执行者是谁（总监决定 O6：只由 automation 服务执行）。
- 注意：encore-api/fileassets/recovery.go 用普通 go build 会报 undefined，这是 Encore 服务方法写法导致的，不是 bug。请用 encore test 或 encore build 验证。
```

### T1-D1 至 T1-D3（删除翻译器，最后做）

```
卡号 T1-D<n>（按 D1、D2、D3 的顺序）。

前置：A、B、C 系列全部合入 develop。

任务：
执行 docs/T1-CARDS.md §5 中的卡片 T1-D<n>：
- D1：删除 pkg/sqldialect、encore-api/pkg/sqldialect、encore-api/pkg/dbx、initialize/platform_driver.go 中的翻译驱动、copy_in_sync_test.go、ownermigration 的转发层。
- D2：加一个检查，禁止这些包或同类“运行时改写 SQL”的代码再次出现。
- D3：在四种组合上跑 scripts/smoke_channels.sh 和 scripts/smoke_history_chain.sh。四种组合是 PG、MySQL、PG+TDengine、MySQL+TDengine；其中 broker 控制面仍然用 PG，要写明。

完成后在 DONE 里提醒负责人：把 wp/d1-dbx-returning 打 archive 标签后删除。
```

### T3 TDengine 去掉实验开关

```
卡号 T3。前置：T1-A3 已合入 develop。

任务：
按 docs/PROJECT-AUDIT-2026-11-04.md §5 的 T3 节执行：
1. 列出所有读写遥测历史的入口（历史、导出、统计、聚合、清理、看板），确认它们都经过同一个 history backend；凡是绕过 adapter 直接查 PG 的，改成走 adapter。
2. 在完整的 PG+TDengine 栈上，用前端走一遍遥测历史页面、导出和统计。
3. 把 TimescaleDB 和 TDengine 的语义做差分对比：毫秒精度、UTC、同一 key 同一时间的冲突、空桶、diff 定义、区间端点。
4. 去掉 storage.tdengine.experimental，并写配置说明。
```

### T2 双库切换文档与 MySQL 栈界面冒烟

```
卡号 T2。前置：T1-D3 已合入 develop。

任务：
按 docs/PROJECT-AUDIT-2026-11-04.md §5 的 T2 节执行：
1. 写 docs/DATABASE-SWITCH.md：列出全部 relational.* 和 storage.history_backend 开关；说明新装时怎么选；写明本期不支持已有数据从 PG 搬到 MySQL。
2. 在 MySQL 栈上用浏览器走一遍：登录、设备管理、设备模板、设备分组、物模型、设备遥测，逐页记录有没有 404 或 500。
3. 照着文档从零搭一次 MySQL 栈，确认文档没有遗漏的步骤。
```

### T8 安装器单一发行版（PG+TimescaleDB）

```
卡号 T8（在安装器仓库 /Users/junhong/Downloads/code/thingspanel-installer 中做，从它的 develop 切分支）。前置：P1、T2、T3 都已合入。

任务：
按 docs/PROJECT-AUDIT-2026-11-04.md §5 的 T8 节执行：
1. 默认安装 PG+TimescaleDB；TDengine 和 MySQL 只作为配置选项，默认关闭。
2. 复核 F01：下载失败时不能破坏旧的配置文件集合。
3. 配置键改用 relational.*，与后端 T1 的决定一致。
4. Encore 镜像用 --base 和 --config 构建（参考 T1-A0-DONE 中的实测），记录镜像摘要。
5. 写《更换组件指南》。
6. 在干净的机器上各做一次新装和升级，然后跑一遍 HA 插件安装和上下行测试。

不包括 Windows 包。
```

### P5 HA 插件小改进

```
卡号 P5（在 HA 插件仓库中做）。前置：用户已把 SDK v0.2.0 推到 GitHub，并且 HA 插件已删除 go.mod 里指向本地 SDK 的 replace。

任务：
1. 插件启动时推送一次实体的当前状态（现在只在状态变化时才上报，新接入的设备在第一次变化前平台没有数据）。
2. 周期同步时处理设备删除（现在只处理新增和修改）。
3. 补单元测试，并用 demo HA 做一次回归。
```

### E3 合并后清理 worktree 和分支（环境类，不改代码）

```
卡号 E3。可以随时做，但不要和其他会话同时改同一个目录。

任务：
按 docs/PROJECT-AUDIT-2026-11-04.md §7.7 列出所有已合入 develop 的 worktree 和分支，包括后端 .claude/worktrees/ 下的 T0、T1-0、T4、T7b、T1-A0，安装器的 -wp-i1、-wp-t5、-wp-t6。

做法：
1. 每一项都用 git cherry 或 merge-base 确认已经合入。
2. 把要删除的清单发给用户确认，确认后再删除。
3. 更新 docs/WORKSPACES.md。

不要动正在运行的会话使用的 worktree（先用 git worktree list 和 git status 检查）。
```

---

## 4. 总监验收清单（执行人交付后，总监逐条检查）

1. 分支基于最新 develop，能干净合并（`git merge-base --is-ancestor develop <分支>`；如果不是，先要求 rebase）。
2. `git diff develop..<分支>` 中没有新增对 `pkg/sqldialect`、`dbx` 的引用，没有改动 PG 已执行的迁移，没有出现 OTA 代码，没有令牌或密码。
3. 改动范围与卡片一致；范围外的问题写进了 `docs/TODO.md`。
4. 总监自己重跑 DONE 里列出的关键测试，至少重跑单元测试和编译。需要数据库的测试，抽查一项。
5. DONE 写得诚实：跳过的测试有原因；单元测试没有被写成端到端；遗留问题都列出来了。
6. 合格后用 `--no-ff` 合入本地 develop，**不推送**；更新 `docs/PROJECT-AUDIT-2026-11-04.md` §7 的进度表。
7. 不合格时，把问题清单发回执行人，在原分支上继续修改。

---

## 5. 只能用户本人做的事

1. 把 SDK `v0.2.0`（`/Users/junhong/Downloads/code/thingspanel-device-connector-sdk-go`）推到 GitHub；发布 Broker、后端和 HA 插件的多架构镜像；建立正式信任根。
2. 确认是否删除：`~/.codex/worktrees/` 下的 3 个空目录，以及主目录中已归档的两个证据目录。
3. `~/.zshrc` 第 61 行有明文的 `MCP_BEARER_TOKEN`，而且文件权限是 644（其他用户可读）。建议删掉这一行（令牌已经改由 `~/.claude/settings.json` 的 `env` 提供，文件权限是 600），或者至少执行 `chmod 600 ~/.zshrc`。
4. 检查 `/Volumes/80G` 盘的健康状况。
5. 把 OTA 补丁包（`/Volumes/500G/archive/handoff/OTA-enterprise-patch-20261104.zip`）交给企业版同事。
