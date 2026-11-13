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
仍保留待负责人确认删除：运行中的 `dbp1-tdengine-*` 验收栈及其 6 个数据卷、网络、`/Volumes/KIOXIA/tmp/dbp1-tdengine-stack`（含本地凭据配置）、两个匿名 Mosquitto 卷。证据目录不删。

## 2026-11-11 前后端 develop 合入主线

- 后端：F:/coding/thingspanel-backend-community/.claude/worktrees/full-develop-integration，分支 wp/full-develop-integration-20261111；负责人 Codex；本轮前后端配套合并，保留主线通知、角色兼容及发布保护。验收和用户确认清理前保留。
- 前端：F:/coding/thingspanel-frontend-community/.claude/worktrees/full-develop-integration，分支 wp/full-develop-integration-20261111；同一合并任务，验收和用户确认清理前保留。
- 外置证据：F:/coding/artifacts/20261111-full-develop-integration/。

### 2026-11-11 测试服配套升级续办

沿用上述两个 worktree，继续处理 c.thingspanel.cn 与已合并主线不一致的问题。用户明确要求保留全部现有租户和设备，先完成存量迁移再切换。范围包括告警分页、认证能力发现、发布前置检查、私密备份与隔离恢复验证、设备/模板/分组转换及全量对账；当前运行版本尚未切换。新证据放在 F:/coding/artifacts/20261111-coordinated-rollout/；服务器升级演练数据库和私密归档保留，不自动删除。

### 2026-11-11 新设备目标调整

用户随后明确不再处理废弃旧设备，要求保证最新代码与新设备完整链路正常。旧转换工作停止，历史数据和备份保留。后端同一 worktree 改用 `wp/new-device-readiness-20261111`，基于 main 独立修复新设备问题，不携带未合并的旧凭据迁移候选。任务报告和外置证据目录不变；新设备完整链路尚未通过。

### 2026-11-13 Q02 metadata CAS

qa_model: .claude/worktrees/q02-template-metadata-cas, branch wp/q02-template-metadata-cas-20261113, base1af19294; parent授权基于实际stage双路径实现，保留main owner锁语义。仅模板元数据CAS/API/定向测试/精确stage转换；不部署。配对frontend同名worktree base b6a5f9be。证据F:/coding/artifacts/20261111-coordinated-rollout/Q02-*，验收后保留，删除前确认。

Q02续行：同一后端worktree转wp/gin-integrated-candidate-20261113，仅统一Gin构建工具/候选workflow，固定1af+CAS05a6+命令474；不合整stage锚点、不改Encore或Broker、不部署。外置Q02-unified-gin-*源与证据验收后保留，删除前确认。
