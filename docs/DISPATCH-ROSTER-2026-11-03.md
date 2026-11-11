# 分派总表（2026-11-03）：谁先开工、谁等谁

使用方法：给每个人（或每个 Claude 会话）发 `DISPATCH-PROMPTS-2026-11-03.md` 里的「通用前言」加下表某一行所列包的提示。
一行就是一个“人”（一条并行线），同一行里的包按顺序做。包的细节在 `WORK-PACKAGES-FINE-2026-11-03.md`，进度与决定在 `PROGRESS-2026-11-03.md`。

用户已决定：E1 已授权；**B1–B5（broker 控制面）和 W1–W2（Windows 包）暂缓，不要分派**。

## 第一批：现在就能同时开工（互不依赖）
| 线 | 人员任务 | 包（按顺序） | 备注 |
|---|---|---|---|
| L1 | devicemetadata 语句改写 | D1 → D2 → D3 → D4 → D5 | D1 的起点：分支 `wip/d1-dbx-returning`（仓库 `/Users/junhong/Downloads/code/thingspanel-history-store-20261103`），MySQL 路径目前报错、测试是红的；D1 解决后 D2–D5 才能少重复劳动。这一线改的文件最多，**不要再拆给两个人改同一文件**。 |
| L2 | devicemetadata 守卫触发器与校验 | D6 → D7 → D8 | 只碰 `mysqlschema/` 和 `scripts/pg2mysql_metadata_guards.py`，与 L1 不冲突 |
| L3 | 其他 Encore owner（小） | O1（thingmodel）→ O2（northbound）→ O5（fileassets） | 用 `pkg/dbx` + 翻译器，照 devicemetadata 的 README-mysql.md |
| L4 | 通知 owner | O3 → O4 | l3-core，66 个文件，最大的一块之一 |
| L5 | 平台后端收尾 | C1 → C6 → C2 | `internal/dal`、`internal/service`、`cmd` |
| L6 | 平台通道与策略 | C4 → C3 | 属性/事件/命令/OTA 通道冒烟；casbin 基础策略来源（需要一个参考安装来导出，找不到就先报告） |
| L7 | 安装器（先做不依赖 MySQL 的两种） | I1 → I2 | 仓库 `/Users/junhong/Downloads/code/thingspanel-installer`（develop） |
| L8 | 验收台账 | V1 | 35 包 / 100 API 卡 / 357 路由，不依赖开发 |
| L9 | 前端 | F1 | 乙队工作在前端 develop 上重做，用 worktree |
| L10 | 环境（**已授权**） | E1（重建 18080 栈） | 先确认数据卷来源，只动 `tp-develop-local` 这套栈 |
| L11 | 磁盘清理 | E2 | 单独一个人，**每一步删之前向用户确认**，方案见 `DISK-CLEANUP-PLAN-2026-11-03.md`；不要和别人同时改同一目录 |

## 第二批：等前置完成再开
| 线 | 包 | 等什么 |
|---|---|---|
| L1 续 / L12 | D9（行级安全替代 + 跨租户测试）、D10（MySQL 档位启动与冒烟） | D1–D5 完成 |
| L6 续 | C5（TDengine 作历史后端 + MySQL 平台整链） | 无硬依赖，但建议 MVP-1 栈稳定后做 |
| L7 续 | I3（MySQL+TDengine profile）、I4（升级/中断/搬迁恢复） | I3 等 C5 和平台启动；**broker 控制面暂缓后它仍依赖 PG，只能做到“平台与 Encore owner 在 MySQL”，不能宣称无 PG 可跑** |
| L9 续 | F2（MySQL 栈界面冒烟） | D10、F1 |

## 第三批：验收（等开发与环境）
| 线 | 包 | 等什么 |
|---|---|---|
| L8 续 | V2（J-G0 插件安装）→ V3（J-G1 设备上报）→ V4（J-G2 故障恢复与旧实例围栏） | E1（18080 栈恢复）或新的集成栈 |
| L8 续 | V5（J-G3 三种 profile 矩阵）、V6（J-G4 installer）、V7（甲乙独立报告 + 联合验收报告） | I1–I4、C5；MySQL+TDengine 因 broker 暂缓无法在 PG 不可用时通过，必须在报告里写成已知缺口 |

## 暂缓（不要分派）
B1–B5（broker 控制面 MySQL 化）、W1–W2（Windows 包）。

## 所有人共同的规则（前言里也有）
- 从 `develop` 切分支 `wp/<编号>-<短名>`，用自己的 git worktree；**不要在 develop 共享工作树里直接改，不要碰主目录（main 上有用户未提交的修改）**。
- 不推送、不碰 main、不发布生产；不改已执行的 PG 迁移；`pkg/sqldialect` 有两份必须字节一致；不把密码写进文件或提交。
- 测试要诚实：被跳过的要说明；PG 现有行为不能变；每处改动附 PG 对 MySQL 的差分测试。
- 交付：分支名、`git log --oneline develop..HEAD`、测试名与结果、没做完的部分。不要自己合进 develop，等负责人确认。
