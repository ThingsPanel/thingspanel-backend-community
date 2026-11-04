# T0 交付说明：清场收尾（2026-11-04）

分支 `wp/t0-work-rules`（从 develop 49b7f278 切出）。**未推送、未合并，等待负责人审查。**

## 做了什么

1. **C2 主目录救援**
   - 密码检查：`configs/conf.yml`、`conf-dev.yml`、`conf.demo.conf` 里的数据库/Redis 口令是已在 HEAD 里的开发默认值，本次工作区差异只有 `mqtt_server: gmqtt → mosquitto` 一处，没有新增密钥；`configs/.telemetry_state.json` 只是运行态（已被 .gitignore）。四个文件仍各备份一份到 `/Volumes/80G/archive/secrets-hold/c2-configs-20261104/`（目录 700、文件 600）。其余未跟踪文件扫描只有测试夹具口令（`fixture-only-*` 等）。
   - 证据：`docs/gmqtt-evidence-2026-10-19/`、`-20/` 打包为 `/Volumes/80G/archive/evidence/gmqtt-2026-10.tgz`（3.3 MB，196 项，`tar -tzf` 与 `find` 条数一致，sha256 `b5904e9bf7644fce7a6f9440e484953a2d883670e8648331e41cf0344c73744b`）。
   - 其余全部改动提交到 `rescue/main-worktree-20261104`，提交 `a06b81d1`（427 个文件，主目录原样快照，未审查）。
   - 主目录已 `git switch --detach develop`（develop 被 `~/.codex/worktrees/thingspanel-develop-backend` 检出，所以用 detached）。4 个规则文件已原样放回主目录（未跟踪，总监继续编辑）。
2. **C3/C4 标签**：核对时发现 8 个待删分支**一个都没有对应的 archive 标签**（含 3 个 codex worktree 的分支），已补打 8 个 `archive/cleanup-20261104-*`（见下）。`archive/*` 现共 49 个。
3. 重写 `docs/WORKSPACES.md`，补登前端、kernel、安装器三个仓库。
4. C7：本分支含 `CLAUDE.md`、`docs/PROJECT-AUDIT-2026-11-04.md`、`docs/CLEANUP-TASKBOOK-2026-11-04.md`、`docs/WORKSPACES.md`，`AGENTS.md` 末尾新增“工作现场规则”一节。
   - 注意：规则文件是 T0 执行时的副本；总监之后在主目录的编辑需要再同步一次。

## 新补的归档标签（均指向分支原提示）

| 标签（前缀 `archive/cleanup-20261104-`） | 提交 |
|---|---|
| `codex-community-roles-superadmin-device-read` | 175953cb |
| `codex-northbound-community-20261021` | 32f89e55 |
| `feature-market-solution-bundle-v1` | a48837e5 |
| `thingmodel` | dd2b59e9 |
| `wip-d1-dbx-returning` | b65688b8 |
| `codex-thingmodel-upgrade-v1` | 0853da31 |
| `codex-runtime-tenant-source-binding` | ef37d40c |
| `codex-runtime-read-context` | ddd5c1e4 |

## 磁盘（`df -h /`）

- 开工时：可用 25 GiB；收尾时：24 GiB（本任务没有删除任何东西，波动来自其他会话）。目标 30 GiB 未达到。
- Docker 卷（只读盘点）：`docker system df` 显示卷合计仅 2.7 GB、可回收 1.6 GB。S1 匿名无引用卷只剩 2 个（约 75 MB）；S2 `sqldb-*` 无引用卷 0 个；S3 `tp-closeout-*` 卷 2 个都在用且很小。因此**删 Docker 卷已不能解决问题**。无引用的命名卷（`thingspanel-install_*`、`tp-cb-v130-isolated-*`、`tp-takeover-b-tv-*` 等）属保护或测试数据，未建议删除。要再腾 6 GiB 以上需看镜像（11.5 GB）、`/Volumes` 以外的大目录或旧 worktree 的 `node_modules`/Go 缓存，另行测量。

## 没做 / 待用户确认

- **未执行任何删除**。待确认清单：
  - 先移除 3 个干净的 worktree（status 为空，无忽略文件）：`~/.codex/worktrees/{native-command-validation,thingspanel-runtime-gateway,thingspanel-runtime-read-context}/thingspanel-backend-community`
  - 再删 8 个分支（均已有标签）：上表 8 个对应的原分支名。
  - 删后本地分支应为 8 个（main、develop、rescue、5 个 claude/wp-*、wp/d1-dbx-returning、wp/t0-work-rules），worktree 为 10 条（>6，因其他会话仍在开发，见 WORKSPACES）。
- 主目录 `git status` 不是完全干净：还留着 4 个规则文件（按要求）和两个 `docs/gmqtt-evidence-2026-10-*/` 证据目录（已归档，删除需确认）。
- rescue 提交未审查；另外 `docs/gmqtt-evidence-2026-10-20-*.md/json` 等零散证据文件按任务书只处理了两个目录，仍在 rescue 提交里。
- 前端 master 有 37 项未提交、kernel main 有 26 项未提交，本次只登记。
- `docs/WORKSPACES.md` 第 6 节的散落目录未量大小、未处理。
