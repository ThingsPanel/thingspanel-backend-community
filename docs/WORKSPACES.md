# 工作目录登记（2026-11-04 重写，T0 清场后状态）

> 规则：新建 worktree 当天登记在此；没登记的目录视为可清理。删除前先列清单给用户确认。
> 本表是 T0 执行时的实测快照，其他会话新增的 worktree 请自行补一行。

## 1. 后端 thingspanel-backend-community

主目录：`/Users/junhong/Downloads/code/thingspanel-backend-community`，**detached 在 develop（49b7f278）**，不在这里改代码。

| 路径 | 分支 | 任务 | 负责人 | 状态 / 处置 |
|---|---|---|---|---|
| `.claude/worktrees/phase1-progress-handoff` | `wp/phase1-progress-handoff` | PHASE1 产品进度交接文档 | Codex | 2026-11-05 登记；待审查，删除需用户确认，2026-11-12 后再评估 |
| `.claude/worktrees/wp-kioxia-config-20261105` | `wp/kioxia-config-20261105` | 新盘路径配置 | 总监 | 2026-11-05 登记；保留，删除待用户确认 |
| `.claude/worktrees/wp-mvp-dispatch-20261105` | `wp/mvp-dispatch-20261105` | 第一阶段派工调整 | 总监 | 2026-11-05 登记；验收合并后保留，删除时间待用户确认 |
| `~/.codex/worktrees/thingspanel-develop-backend/thingspanel-backend-community` | `develop` | 共享开发线 | 总监 | 保护，不动 |
| `.claude/worktrees/wp-db-compat-f6f630` | `wp/d1-dbx-returning` | 数据库兼容（D1） | 进行中会话 | 保护，不动 |
| `.claude/worktrees/wp-plugin-install-e5e1f9` | `claude/wp-plugin-install-e5e1f9` | 插件安装 | 进行中会话 | 保护，不动 |
| `.claude/worktrees/wp-missing-api-5613cb` | `claude/wp-missing-api-5613cb` | 补全缺失 API（已暂停认领） | — | 先保留，待总监决定 |
| `.claude/worktrees/wp-t1-0-relational-b140f7` | `claude/wp-t1-0-relational-b140f7` | T1-0 关系库底座设计 | 进行中会话 | 进行中，不动 |
| `.claude/worktrees/wp-plugin-install-ui-8bf83e` | `claude/wp-plugin-install-ui-8bf83e` | PLUGIN-UI 插件一键安装界面（第一阶段设计） | 进行中会话 | 2026-11-05 登记；设计待总监审查，合并后当天清理 |
| `.claude/worktrees/wp-ha-plugin-target-docs-20261106` | `wp/ha-plugin-target-docs-20261106` | HA 插件目标分析与任务拆分（文档） | Codex | 2026-11-06 登记；提交待审查，不推送不合并 |
| `.claude/worktrees/wp-t7-ha-catalog-renewal` | `wp/t7-ha-catalog-renewal` | T7 HA 共享安装与续期稳定 | Codex | 2026-11-06 登记；进行中，预计审查合并后清理，删除前确认 |
| `.claude/worktrees/wp-t1-a1-platform-start` | `wp/t1-a1-platform-start` | T1-A1 平台启动与连接 | Codex 子会话 | 基线 `develop@85b04e7b339cc0b7d4ff78ba6ec5e7de1640e792`；已提交待审查，审查后清理 |
| `.claude/worktrees/wp-t4-plugin-identity-3cf9d2` | `claude/wp-t4-plugin-identity-3cf9d2` | T4 插件身份 | 进行中会话 | 进行中，不动 |
| `.claude/worktrees/wp-p2-fix` | `wp/p2-fix-redaction` | P2-fix 凭据脱敏回归修复 | Codex 子任务 | 进行中；审查合并后当天清理 |
| `.claude/worktrees/wp-p2-fix2-form-brackets` | `wp/p2-fix2-form-brackets` | P2-fix2 原始方括号 form 字段脱敏 | Codex 子任务 | 进行中；审查合并后当天清理 |
| `.claude/worktrees/wp-p3-device-ack-timeout` | `wp/p3-device-ack-timeout` | P3 Broker 设备回执超时可见 | Codex 子任务 | 进行中；审查合并后当天清理 |
| `.claude/worktrees/wp-p3-real-broker-20261105` | `wp/p3-real-broker-20261105` | P3 真实 Broker ACK / 无 ACK 验收 | Codex 子任务 | 2026-11-05 登记；完成待审查，清理时间待负责人确认 |
| `~/.codex/worktrees/native-command-validation/thingspanel-backend-community` | `codex/thingmodel-upgrade-v1` | 已归档（标签 `archive/cleanup-20261104-codex-thingmodel-upgrade-v1`） | — | 干净，已移除，分支已删 |
| `~/.codex/worktrees/thingspanel-runtime-gateway/thingspanel-backend-community` | `codex/runtime-tenant-source-binding` | 已归档 | — | 已移除，分支已删 |
| `~/.codex/worktrees/thingspanel-runtime-read-context/thingspanel-backend-community` | `codex/runtime-read-context` | 已归档 | — | 已移除，分支已删 |
| `/Volumes/80G/tp-develop-integration/sources/backend-api-1efd4878` | detached `1efd4878` | 18080 本地栈 fixture | — | 保护，不动 |
| （C7 完成后）`.claude/worktrees/wp-t0-work-rules` | `wp/t0-work-rules` | T0 工作规则 | T0 会话 | 等待审查；合并后当天删 |

### 后端本地分支

| 分支 | 用途 | 处置 |
|---|---|---|
| `main` | 主线 | 保留 |
| `develop` | 开发线 | 保留 |
| `rescue/main-worktree-20261104` | 主目录 2026-11-04 原样快照（未审查），提交 a06b81d1 | 保留，待总监决定拆分/丢弃 |
| `wp/d1-dbx-returning` | D1 数据库兼容 | 进行中 |
| `wp/p2-fix-redaction` | P2-fix 凭据脱敏回归修复 | 进行中 |
| `wp/p2-fix2-form-brackets` | P2-fix2 原始方括号 form 字段脱敏 | 进行中 |
| `wp/p3-device-ack-timeout` | P3 Broker 设备回执超时可见 | 进行中 |
| `claude/wp-plugin-install-e5e1f9` / `claude/wp-missing-api-5613cb` / `claude/wp-t1-0-relational-b140f7` / `claude/wp-t4-plugin-identity-3cf9d2` | 见上表 | 进行中 / 保留 |
| `codex/thingmodel-upgrade-v1`、`codex/runtime-tenant-source-binding`、`codex/runtime-read-context` | 已归档，worktree 待移除 | 已删除（2026-11-04） |
| `codex/community-roles-superadmin-device-read`、`codex/northbound-community-20261021`、`feature/market-solution-bundle-v1`、`thingmodel`、`wip/d1-dbx-returning` | 已归档，无 worktree | 已删除（2026-11-04） |
| `wp/t0-work-rules`（C7 后） | 工作规则 | 等待审查 |

### 归档标签
49 个 `archive/*`（35 个 `archive/cleanup-20261104-*` 之前批次 + 8 个本次补打 + 6 个 `archive/rescue-cleanup-20261104-*`）。本次补打的 8 个：
`community-roles-superadmin-device-read`、`northbound-community-20261021`、`feature-market-solution-bundle-v1`、`thingmodel`、`wip-d1-dbx-returning`、`codex-thingmodel-upgrade-v1`、`codex-runtime-tenant-source-binding`、`codex-runtime-read-context`（前缀均为 `archive/cleanup-20261104-`）。

## 2. 归档与证据位置（仓库外）

| 位置 | 内容 |
|---|---|
| `/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/` | 2026-11-06 创建的 HA 插件独立 UI 安装验收栈；使用独立 Compose 项目、网络和外置盘绑定数据目录，候选签名为临时测试签名。待负责人审查；本任务未删除目录、容器或数据卷。 |
| `/Volumes/80G/archive/evidence/gmqtt-2026-10.tgz` | `docs/gmqtt-evidence-2026-10-19/`、`-20/`（196 项，sha256 `b5904e9b…3744b`） |
| `/Volumes/80G/archive/secrets-hold/c2-configs-20261104/` | 主目录 `configs/conf*.yml/.conf`、`.telemetry_state.json` 副本（600） |
| `/Volumes/80G/archive/secrets-hold/{main-worktree,clean-worktrees,dirty-worktrees,codex-worktrees}-20261104/` | 清场早期备份 |

## 3. 前端 thingspanel-frontend-community（本次只登记，不处理）

| 路径 | 分支 | 未提交 | 备注 |
|---|---|---:|---|
| `~/Downloads/code/thingspanel-frontend-community`（主目录） | `master` | 37 | 保留；需另行救援/确认 |
| `~/.codex/worktrees/ce71/thingspanel-frontend-community` | detached `2384ab85` | 9 | 待用户决定 |
| `~/Downloads/code/thingspanel-frontend-develop-merge-20261103` | `codex/frontend-develop-merge-20261103` | 0 | 待用户决定 |
| `~/Downloads/code/tp-automation-frontend` | `feat/automation-rewrite-20261023` | 4 | 待用户决定 |
| `/Volumes/80G/tp-develop-integration/sources/frontend-api-b7318932` | detached `b7318932` | 0 | fixture，保护 |
| `~/Downloads/code/thingspanel-frontend-community.worktrees/` | — | — | 空目录 |

## 4. kernel iot-api-encore-kernel（本次只登记）

| 路径 | 分支 | 未提交 | 备注 |
|---|---|---:|---|
| `~/Downloads/code/iot-api-encore-kernel`（主目录） | `main` | 26 | 本地 11 个分支，待盘点 |
| `~/Downloads/code/iot-api-encore-kernel.worktrees/encore-thingsmodel` | （未登记为 worktree） | ? | 待用户决定 |

## 5. 安装器 thingspanel-installer（本次只登记）

| 路径 | 分支 | 未提交 | 备注 |
|---|---|---:|---|
| `~/Downloads/code/thingspanel-installer`（主目录） | `feat/thingmodel-api` | 1 | 保留 |
| `~/Downloads/code/thingspanel-installer-develop-review` | `develop` | — | 总监验收、合并工作树；2026-11-04 登记，待用户确认后清理 |
| `~/Downloads/code/thingspanel-installer-develop-review/.claude/worktrees/wp-mvp-config` | `wp/mvp-config` | — | 默认 PG 配置对齐，2026-11-05 登记；保留，删除待用户确认 |
| `~/Downloads/code/thingspanel-installer-wp-pui` | `wp/pui-agent` | PLUGIN-UI Agent 目录模式 + 安装器收敛 | 进行中会话 | 2026-11-05 登记；待审查，合并后当天清理 |
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-t6-ha-public-release` | `wp/t6-ha-public-release` | T6 HA 正式制品与安装分发 | Codex | 2026-11-06 登记；进行中，预计审查合并后清理，删除前确认 |
| `~/Downloads/code/thingspanel-device-connector-homeassistant/.claude/worktrees/t6-public-release` | `wp/t6-ha-public-release` | T6 HA 插件正式制品与受管身份 | Codex | 2026-11-06 登记；进行中，预计审查合并后清理，删除前确认 |
| `~/Downloads/code/thingspanel-device-connector-homeassistant/.claude/worktrees/t6-public-artifact` | `wp/t6-ha-public-artifact` | T6 HA 公开源码制品 | Codex | 2026-11-07 登记；进行中，预计审查合并后清理，删除前确认 |
| `~/Downloads/code/thingspanel-device-connector-sdk-go-wp-t5-runtime-identity` | `wp/t5-sdk-runtime-identity-20261106` | T5 SDK 兼容上游的受管身份 | Codex | 2026-11-06 登记；进行中，预计审查合并后清理，删除前确认 |
| `~/Downloads/code/thingspanel-device-connector-sdk-go-wp-t5-public-release` | `wp/t5-sdk-public-release` | T5 SDK 公开发布源码候选 | Codex | 2026-11-07 登记；进行中，预计审查合并后清理，删除前确认 |
| `~/Downloads/code/thingspanel-frontend-wp-pui`（前端仓库） | `wp/pui-frontend` | PLUGIN-UI 前端安装页 | 进行中会话 | 2026-11-05 登记；待审查，合并后当天清理 |
| `~/Downloads/code/thingspanel-installer-wp-p1-fix` | `wp/p1-fix-retry-lock` | — | P1 补救，2026-11-04 登记；待审查完成及用户确认后清理 |
| `~/Downloads/code/thingspanel-installer-wp-i1` | `wp/i1-pg-profiles` | 0 | 进行中 |
| `~/Downloads/code/thingspanel-installer-wp-t6` | `wp/t6-ha-catalog` | 0 | 进行中（T6 HA 打包） |

## 6. 其他散落目录（未处理，需用户决定）

`~/Downloads/code/` 下带日期/handoff/closeout/takeover 的目录：`thingspanel-closeout-internal-20261102`、`thingspanel-closeout-work-20261102`、`thingspanel-takeover-evidence-20261102`、`thingspanel-takeover-final-20261102`、`thingspanel-merged-worktree-uncommitted-20261028`、`thingspanel-notification-schema-handoff-20261102`、`notification-preserved-work-20261028`、`thingspanel-modbus-protocol-plugin-handoff`、`thingspanel-installer-manifest-20261102`、`thingspanel-frontend-typecheck-20261102`。`~/.codex/worktrees/` 下的 team-b-*、thingspanel-takeover-20261102 等非后端目录。均未量大小、未处理。

## PH1-S1（2026-11-05 登记）
| 路径 | 分支 | 备注 |
|---|---|---|
| `.claude/worktrees/wp-ph1-s1-core-c02f8c`（后端） | `claude/wp-ph1-s1-core-c02f8c` | PH1-S1 交付；待总监审查 |
| `/Volumes/KIOXIA/tmp/ph1-s1-work/frontend`（前端 worktree，分离头 `1da66eca`） | — | PH1-S1 前端构建来源；审查后可删 |
| `/Volumes/KIOXIA/tmp/ph1-s1-work/ph1-s1`（安装器 `4a8f286` 副本 + compose 项目 `ph1-s1`） | — | **运行中，保留给 HA 卡**；含凭据在 `../secrets/` |

## T3（2026-11-09 登记）
| 路径 | 分支 | 任务 | 负责人 | 预计清理 |
|---|---|---|---|---|
| `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack` | — | 独立安装器副本与 `dbp1-tdengine` Compose 项目（含本地配置）；A2 独立启动探针已移除，遗留匿名 Mosquitto 卷 `b61b4bdc2d16`、`c2e1008577a1` 保留 | Codex | 验收后留存，卷删除前确认 |
| `/Volumes/KIOXIA/archive/evidence/2026-11-09-T3-tdengine/stack` | — | 不含凭据的安装器源码快照（早期初始化副本） | Codex | 保留，删除前确认 |
| `/Volumes/KIOXIA/archive/evidence/2026-11-09-T3-tdengine` | — | A1 观察、后端日志和 TDengine A2 测试结果；历史/统计已验证，导出文件待用户回到电脑后确认，不含凭据 | Codex | 交付证据 |

## 数据库第一阶段清理（2026-11-10）
已删除：T1-A2/A3/A4、T3 的 worktree 与本地分支（均已合入 origin/develop `82ad259e`）、`dbp1-smoke-*` 与 `dbp1-tdengine` 已退出容器。
负责人于 2026-11-10 确认后，已删除 `dbp1-tdengine-*` 验收栈（14 个容器、6 个数据卷、2 个匿名 Mosquitto 卷、网络）和 `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack`。证据目录保留。

## R0–R7 重启/备份/升级（2026-11-12 登记）
| 路径 | 分支 | 任务 | 负责人 | 预计清理 |
|---|---|---|---|---|
| `.claude/worktrees/wp-r0-restart-plan-179c91`（后端） | `wp/R0-restart-plan` | R0 盘点与审定 | Claude 会话 | 审查合并后当天清理，删除前确认 |
| `.claude/worktrees/wp-r1-health-gate`（后端） | `wp/R1-health-gate` | R1 健康门（后端部分） | Claude 会话 | 审查合并后当天清理，删除前确认 |
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r1-system-role`（安装器） | `wp/R1-system-role` | R1 Agent system 角色 | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认 |
| `.claude/worktrees/wp-r2-check-config`（后端） | `wp/R2-check-config` | R2 `--check-config`（叠在 `wp/R1-health-gate` 上） | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认 |
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r2-restart`（安装器） | `wp/R2-restart`（叠在 `wp/R1-system-role` 上） | R2 重启 | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认 |

## R2 演练现场（2026-11-12 登记，已停止未删除，待用户确认清理）
| 路径/对象 | 说明 | 负责人 |
|---|---|---|
| `/Volumes/KIOXIA/tmp/r2-work`（含 `r2/` 安装器副本与 `.env`、`agent/`、构建上下文） | R2 隔离栈（compose 项目 `r2`），含演练用临时凭据 | Claude 会话 |
| 容器 `r2-*`（11 个）、卷 `r2_go_configs/r2_go_files/r2_mosquitto_data/r2_postgres_data/r2_redis_data`、网络 `r2_thingspanel_network`、镜像 `r2-backend:dev` | 同上，已停止 | Claude 会话 |
| `.claude/worktrees/wp-r2-drill` | `wp/R2-drill`：本说明 | Claude 会话 |
## R2E Encore 镜像构建（2026-11-12 登记）
| 路径 | 分支 | 任务 | 负责人 | 预计清理 |
|---|---|---|---|---|
| `.claude/worktrees/wp-r2e-encore-build-4e9274`（后端） | `wp/R2E-encore-build` | R2E 查明 `encore build docker` 卡死并给出可用构建方式 | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认。本机另有镜像 `r2e-encore:dev`（只含我自己建的 `r2e-` 资源） |
| `.claude/worktrees/wp-r2g-scope-encore-15bd9f`（后端） | `wp/R2G-scope-encore` | R2G 把 `/internal/broker-scope` 从老 Gin 改写为 Encore 服务 | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认 |

## R4S dump/restore 之后 schema 校验（2026-11-12 登记）
| 路径 | 分支 | 任务 | 负责人 | 预计清理 |
|---|---|---|---|---|
| `.claude/worktrees/wp-r4s-schema-contract-95ecf9`（后端） | `wp/R4S-schema-contract` | R4S 还原后 schema 校验稳定化 | Claude 会话 | 2026-11-12 登记；审查合并后当天清理，删除前确认。`r4s-` 容器/网络已删除 |

## R3（2026-11-12 登记）
| 路径/对象 | 分支 | 任务 | 预计清理 |
|---|---|---|---|
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r3-backup`（安装器） | `wp/R3-backup` | R3 备份 | 审查合并后当天清理 |
| `.claude/worktrees/wp-r3-done`（后端） | `wp/R3-done` | R3 交付说明 | 审查合并后当天清理 |
| 容器 `r3-*`、卷 `r3_*`、网络 `r3_thingspanel_network`、镜像 `r3-backend:dev`、`/Volumes/KIOXIA/tmp/r3-work` | — | R3 演练栈，**保留给 R4**，R4 结束后清理（含临时凭据） | Claude 会话 |

## R4（2026-11-12 登记）
| 路径/对象 | 分支 | 任务 | 预计清理 |
|---|---|---|---|
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r4-restore`（安装器） | `wp/R4-restore` | R4 恢复 | 审查合并后当天清理 |
| `.claude/worktrees/wp-r3-done`（后端，现分支 `wp/R4-done`） | `wp/R4-done` | R3+R4 交付说明 | 审查合并后当天清理 |
| R3/R4 演练栈（容器 `r3-*`、卷 `r3_*`、网络、镜像 `r3-backend:dev`、`/Volumes/KIOXIA/tmp/r3-work`） | — | **已于 R4 结束时删除**，证据在 `/Volumes/KIOXIA/archive/evidence/2026-11-12-R3` 和 `-R4` | 已清理 |

## R3/R4 清理（2026-11-12）
R3、R4、R4S 已合并推送到两个仓库的 develop；`wp-r3-done`、`wp-r4s-schema-contract-95ecf9`（后端）和 `wp-r3-backup`、`wp-r4-restore`（安装器）worktree 及对应分支已删除。

## R5a（2026-11-12 登记）
| 路径/对象 | 分支 | 任务 | 预计清理 |
|---|---|---|---|
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r5a-txn`（安装器） | `wp/R5a-txn` | R5a 配置事务移植 + Agent 生产接线 | 审查合并后当天清理 |
| `.claude/worktrees/wp-r5a-done`（后端） | `wp/R5a-done` | R5a 交付说明、镜像脚本补 brokerscope | 审查合并后当天清理 |
| 镜像 `r5a-encore:dev`（含 brokerscope 的 Encore 镜像） | — | **保留给 R5b**，R5b 结束后删除 | Claude 会话 |
| 演练栈 `r5a-*`、`/Volumes/KIOXIA/tmp/r5a-work`、`r5a-bin` | — | 已于 R5a 结束时删除，证据在 `/Volumes/KIOXIA/archive/evidence/2026-11-12-R5a` | 已清理 |

## R6（2026-11-13 登记）
| 路径/对象 | 分支 | 任务 | 预计清理 |
|---|---|---|---|
| `.claude/worktrees/wp-r6-system-api`（后端） | `wp/R6-system-api` | R6 Encore 管理 API + 交付说明 | 审查合并后当天清理 |
| `~/Downloads/code/thingspanel-installer/.claude/worktrees/wp-r6-agent`（安装器，叠在 `wp/R5b-upgrade` 上） | `wp/R6-agent` | R6 Agent 接口、nginx 维护页、修复 | 审查合并后当天清理 |
| `~/Downloads/code/thingspanel-frontend-community/.claude/worktrees/wp-r6-frontend`（前端，单独会话） | `wp/R6-frontend` | R6 前端界面 | 审查合并后当天清理 |
| 镜像 `r6-encore:dev`、`r6-frontend:dev` | — | 保留给 R7 整栈演练，R7 结束后删除 | Claude 会话 |
| 演练栈 `r6-*`、`r6-backend:*`、`docker:27-cli`、`r5a-encore:dev`、`/Volumes/KIOXIA/tmp/r6-work` | — | 已于 R6 结束时删除，证据在 `/Volumes/KIOXIA/archive/evidence/2026-11-13-R6` | 已清理 |

