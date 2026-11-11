# 任务书 T0：清理工作现场（2026-11-04）

> 交给一个人（或一个 Claude/Codex 会话）**单独**完成。清场期间别人可以照常在自己的 worktree 里开发，但**不要和清场的人同时改同一个目录**。
> 依据：`docs/PROJECT-AUDIT-2026-11-04.md` 第 1 节、第 3 节的实测数据。

## 0. 执行状态（2026-11-04 复查）

| 步骤 | 状态 | 实测 |
|---|---|---|
| C1 盘点 | 🟡 初稿已出 | `docs/WORKSPACES.md` 是清理前的快照，需要按当前状态重写，并补上安装器、前端、kernel 三个仓库 |
| C2 主目录救援 | ❌ 未做 | 主目录仍在 `main`，有 255 个未提交改动 |
| C3 分支收口 | 🟡 大部分完成 | 本地分支 48→13；`archive/*` 标签 41 个。还剩 `codex/community-roles-superadmin-device-read`、`codex/northbound-community-20261021`、`feature/market-solution-bundle-v1`、`thingmodel`、`wip/d1-dbx-returning` 没删，以及 3 个仍被 worktree 检出的归档分支 |
| C4 worktree 收口 | 🟡 大部分完成 | 25→9；还剩 `~/.codex/worktrees/native-command-validation`、`thingspanel-runtime-gateway`、`thingspanel-runtime-read-context` |
| C5 外部目录 | 未复查 | — |
| C6 Docker | 🟡 | 系统盘可用空间 12→23 GiB，**还没达到 30 GiB 的目标** |
| C7 写入规则 | ❌ 未做 | `CLAUDE.md` 已写在主目录根部，但还没进 develop |

剩余工作也列在 `docs/PROJECT-AUDIT-2026-11-04.md` 的 T0 中。C2 有一点要注意：主目录根部的 `CLAUDE.md`、`docs/PROJECT-AUDIT-2026-11-04.md`、`docs/CLEANUP-TASKBOOK-2026-11-04.md`、`docs/WORKSPACES.md` 这 4 个文件要放进 C7 的规则分支，不要只留在 rescue 分支里。

## 1. 目标

把散落的分支、worktree、临时目录、证据文件、Docker 卷收回来，让下面三条都成立：

1. 后端仓库本地分支 **≤ 15 个**，每个都能说出用途。
2. 每个仓库只保留**1 个主目录 + 正在做的任务的 worktree**；所有工作目录都登记在 `docs/WORKSPACES.md`。
3. 系统盘可用空间 **≥ 30 GiB**，而且**源码、证据都没有丢**（删分支前打 tag，删目录前先归档）。

## 2. 铁律（违反任何一条就停下来问）

- **删任何东西之前，先把清单发给用户确认**，确认一批删一批。不要一次性批量删。
- **不推送、不删远端分支**，不碰 `origin/*`；不发布生产。
- **不要用 `git stash`**（stash 栈是所有 worktree 共用的）；需要暂存就用临时提交。
- 有未提交改动的目录，**先做 rescue 提交再处理**，不能直接删。
- 发现密码、token、私钥：**不提交、不打包进归档**，单独告诉用户位置。
- 保护清单（**不许动**）：
  - `.claude/worktrees/wp-db-compat-f6f630`（分支 `wp/d1-dbx-returning`，数据库兼容，正在做）
  - `.claude/worktrees/wp-plugin-install-e5e1f9`（分支 `claude/wp-plugin-install-e5e1f9`，插件，正在做）
  - `.claude/worktrees/wp-missing-api-5613cb`（分支 `claude/wp-missing-api-5613cb`，先保留）
  - `~/.codex/worktrees/thingspanel-develop-backend/...`（`develop` 的共享工作树）
  - `/Volumes/80G/tp-develop-integration/`（18080 本地栈的 fixture）
  - Docker 卷 `thingspanel-community-v130_postgres_data`（参考安装库）、`thingspanel-install_*`（安装器测试数据）
  - 分支 `main`、`develop`

## 3. 步骤

### C1 盘点（只读，产出清单）

1. 后端：`git worktree list`、`git for-each-ref refs/heads`，以及每个分支对 develop 的 `git cherry develop <分支>`（`+` 表示还没合入）。
2. 列出 `~/.codex/worktrees/*`、`~/Downloads/code/` 下名字带日期或 `handoff`/`takeover`/`closeout`/`preserved`/`merged-worktree` 的目录、`/Volumes/500G/code/*`。每个目录记：属于哪个仓库、哪个分支、有没有未提交改动（`git status --porcelain | wc -l`）、大小（`du -sk`，**一次量一个目录，放后台**，别扫整个 `~/Downloads/code` 或 `/private`）。
3. 前端 `thingspanel-frontend-community`、`iot-api-encore-kernel`、`thingspanel-installer` 也照此盘点（这三个仓库**本次只盘点、不删**，清单交给用户决定）。
4. 产出 `docs/WORKSPACES.md`：一行一个目录或分支，列为 路径 / 分支 / 用途 / 状态 / 处置建议（保留、归档、删除）。

**发给用户确认后再做 C2。**

### C2 救援主目录的未提交改动

主目录 `/Users/junhong/Downloads/code/thingspanel-backend-community` 停在 `main` 上，有 250 个未提交改动。其中 21 个已跟踪文件和若干新文件**在任何提交里都没有**（清单见审核报告 §3.1）。

1. 先检查 `configs/conf.yml`、`configs/conf-dev.yml`、`configs/conf.demo.conf`、`configs/.telemetry_state.json` 里有没有真实密码。有的话，这个文件复制到仓库外（`/Volumes/80G/archive/secrets-hold/`，权限 600），**不提交**，并告诉用户。
2. 把证据目录 `docs/gmqtt-evidence-2026-10-19/`、`docs/gmqtt-evidence-2026-10-20/`（约 22 MB）打包到 `/Volumes/80G/archive/evidence/gmqtt-2026-10.tgz`，并校验 `tar -tzf` 能列出内容。
3. `git switch -c rescue/main-worktree-20261104`，`git add` 其余全部改动（排除第 1、2 步处理过的文件），提交说明写清楚“主目录 2026-11-04 原样快照，未审查”。
4. `git switch develop`（如果提示 develop 已经被别的 worktree 检出，就用 `git switch --detach develop`，然后告诉用户）。
5. 验收：`git status` 干净；`git log rescue/main-worktree-20261104 -1 --stat` 能看到这些文件。

### C3 分支收口

1. **已全部合入 develop 的，直接删**（`git branch -d`。如果 `-d` 拒绝，就用 `git cherry` 复核，确认全是 `-` 后再用 `-D`）：
   - A/B 组：`codex/team-a-t04-telemetry-winner-20261102`、`codex/team-a-t05-config-gate-20261102`、`codex/team-a-telemetry-read-guard-20261102`、`codex/team-a-template-owner-20261102`、`codex/team-a-ws-device-auth-20261102`、`codex/team-b-compensation-fail-closed-20261102`、`codex/service-access-tenant-20261102`
   - 其余：`codex/a08-mysql-created-at-20261102`、`codex/closeout-backend-20261102`、`codex/closeout-integrate-develop`、`codex/command-metadata-integration`、`codex/community-cli-completion-20261101`、`codex/delegated-protocol-integration`、`codex/history-store-20261103`、`codex/kernel-results-integration-20261101`、`codex/local-develop-runtime`、`codex/market-install-idempotency`、`codex/metadata-menus-native-completion`、`codex/metadata-menus-on-v1.3.0`、`codex/notification-community-bridge`、`codex/revert-community-tenant-permission`、`codex/takeover-backend-20261102`、`codex/takeover-final-backend-20261102`、`codex/thingmodel-dev-integration-20261018`、`codex/thingmodel-market-source-reuse-20261020`、`codex/thingmodel-template-binding-20261018`、`codex/unified-closeout-develop-20261102`、`codex/xiaomi-service-gateway-20260502`、`feat/automation-rewrite-20261023`、`feature/detached-work-backup`、`market-bundle-v1-clean`、`workbuddy/main-2f2afec6`
   - 分支被 worktree 检出时会删不掉，先做 C4 移除对应的 worktree。
2. **有未合入提交的，先打 tag 归档再删**（`git tag archive/<原分支名去掉斜杠> <分支>`，然后 `git branch -D`）：
   `codex/runtime-tenant-source-binding`、`codex/community-runtime-scoped-gateway`、`codex/native-runtime-tenant-fence`、`codex/runtime-read-context`、`codex/thingmodel-upgrade-v1`、`thingmodel`、`codex/northbound-community-20261021`、`codex/community-roles-superadmin-device-read`、`feature/market-solution-bundle-v1`、`wip/d1-dbx-returning`
3. 验收：`git branch | wc -l` ≤ 15；`git tag -l 'archive/*'` 有 10 个。

### C4 worktree 收口

1. 对 C3 中删掉的每个分支，以及 detached HEAD 的 worktree（`team-a-automation-mode-20261102`、`team-a-parent-create-fence-20261102`、`team-a-t03-device-delete`、`team-a-ws-device-auth-20261102`、`/Volumes/80G/tp-develop-integration/sources/backend-api-1efd4878` 除外，它属于 fixture），逐个执行：
   - `git -C <路径> status --porcelain`：为空就 `git worktree remove <路径>`；不为空就先 `git -C <路径> switch -c rescue/<目录名>`，提交后再移除。
2. `git worktree prune`（清理 `prunable` 的那一条）。
3. `~/.codex/worktrees/` 下不属于后端仓库的目录（前端、kernel、thingsvis 等），按 C1 清单交给用户决定。

### C5 外部散落目录归档

对 `~/Downloads/code/` 下 `thingspanel-closeout-*`、`thingspanel-takeover-*`、`thingspanel-*-handoff*`、`thingspanel-merged-worktree-uncommitted-*`、`notification-preserved-work-*`、`thingspanel-frontend-*-2026110x`、`thingspanel-installer-manifest-*` 等目录，逐个处理：

1. 如果是 git worktree：按 C4 处理。
2. 如果是证据或交接材料：`tar czf /Volumes/80G/archive/<目录名>.tgz -C ~/Downloads/code <目录名>`，校验后**等用户确认再删原目录**。
3. 注意：`/Volumes/500G` 之前报过 APFS 错误，归档目标用 `/Volumes/80G`；要用 500G 之前先跑 `diskutil verifyVolume /Volumes/500G`。

### C6 Docker 空间

按 `develop:docs/DISK-CLEANUP-PLAN-2026-11-03.md` 的 S1→S3 执行（匿名卷、`sqldb-*`、Go 缓存卷）。S4/S5 先归档。做完后用 `docker system df` 和 `df -h /` 复查；Docker Desktop 里还要回收磁盘镜像空间，才能真正释放到系统盘。

### C7 写入防乱规则（需用户同意提交 develop）

1. 主目录根部的 `CLAUDE.md`（已经写好）就是规则正文。
2. 在 `develop` 上新开 `wp/t0-work-rules` 分支：把这份 `CLAUDE.md` 加进去，再在 `AGENTS.md` 末尾加一节“工作现场规则”，内容引用 `CLAUDE.md`，这样 Codex 也会读到；同时加入 `docs/WORKSPACES.md`。
3. 仓库根目录的过程文档（`HANDOVER-MERGE-TO-1.3.*`、`MERGE-AUTOMATION-REWRITE.md`、`automation-rewrite-progress.md`、`task-book-walkthrough.md`）用 `git mv` 移到 `docs/archive/2026-10/`。
4. 交给用户审查后再合入 develop。

## 4. 交付物

- `docs/WORKSPACES.md`（最终状态）
- 一份清场记录：删了什么、归档到哪里（含 tgz 路径与大小）、rescue 分支和 archive tag 列表、清理前后 `df -h /` 的数字
- 没处理的项目及原因

## 5. 完成标准

- [ ] 后端本地分支 ≤ 15，`archive/*` tag 10 个
- [ ] 后端 `git worktree list` ≤ 6 条（主目录、develop、3 个 wp、fixture）
- [ ] 主目录在 develop 上，`git status` 干净
- [ ] `docs/WORKSPACES.md` 已经覆盖所有保留的目录
- [ ] `df -h /` 可用 ≥ 30 GiB
- [ ] 规则文件已提交到 `wp/t0-work-rules`，等待用户审查
