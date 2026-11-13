# R5b 交付说明：升级 + 自动回退（2026-11-13）

- 安装器分支 `wp/R5b-upgrade`（从安装器 develop `cfa5b7f` 切出，最后提交 `407f3a7`，5 个提交），**未合并未推送**。
- 后端分支 `wp/R5b-done`（本说明、`docs/WORKSPACES.md`、`docs/TODO.md`）。
- 演练证据：`/Volumes/KIOXIA/archive/evidence/2026-11-13-R5b/`（每个场景一个日志；`INVALID-*` 是我自己演练脚本的问题造成的无效记录，保留并在下面说明）。

## 做了什么（全部 Go，在 Agent 的 system 角色里）
`tp-agentctl system upgrade to=v1.3.1`（只有目标版本来自请求；必须与签名清单和暂存 compose 里的后端镜像标签一致）。

1. **预检**（不改任何东西）：版本号格式、必须比当前新、数据服务在运行、compose 辅助镜像在本地（没有就拉）。
2. **暂存**：用 R5a 的 Go 配置事务下载整个发布包到私有暂存目录，**签名必须验过**（没有信任根就不允许升级），校验通过后**提前拉取新发布 compose 里所有镜像**——此时旧版本还在运行，发布被拒绝或镜像拉不到都不会代价一次快照或停机。
3. **强制快照**（R3 备份，触发器 `pre-upgrade`），做不了就放弃，什么都没动。
4. 维护标记 → 停控制面 → 替换受管文件（与 `update.py` 共用锁和 journal）→ 在一次性辅助容器里运行 `docker compose up -d --no-deps --pull never <已部署的服务>`（compose 只重建配置或镜像变了的服务）→ **健康门**（后端 `/health` 含数据库版本号）→ 提交。
5. **失败自动回退**：停控制面 → 还原配置文件（`Revert`，只回滚本次事务）→ 从快照还原数据库和文件 → 用旧配置 `compose up` → 再过一次健康门 → `upgrade_failed_rolled_back`。回退也失败：`needs_human`，维护标记保留，什么都不删，之后拒绝新的升级。
6. **崩溃恢复**：每个阶段先写状态文件再干活（`preflight, staging, snapshot_done, stopping, applying, applied, starting, verifying, committed | aborted | rolling_back, rolled_back | needs_human`），每个阶段也写 Agent 日志。Agent 启动时读取：到 `applying` 之前只清理并拉起控制面（`upgrade_aborted`），从 `applying` 起回退；先删掉死掉的 Agent 留下的辅助容器。
7. 生产启动：`agent_host.py up --system --release-source URL --release-trust-root PEM [--release-ca PEM]`，Agent 自己生成含 `upgrade` 块的 `system.json`；新增 helper 摘要组和四个入口脚本的 `update.py` 摘要。

## 怎么验证的

**单元测试**（Linux 容器，`-race`；不是整栈验收）：`upgrade_test.go` 用假的“站点”模型，断言最终状态（配置、数据库、运行中的镜像、维护标记）。覆盖：成功提交；7 种拒绝；`preflight/stage/snapshot/stop` 任一步失败 → 中止且**不还原快照**；`apply/up/verify` 失败 → 回退到旧站点（含被新版本迁移过的数据库）；回退的每一步失败 → `needs_human` 并拒绝下次升级；**9 个阶段各被 kill 一次（模拟无清理的死亡）→ 新进程恢复到旧站点或干净中止**，恢复幂等；回退中连续崩溃两次仍恢复；持锁时拒绝；状态文件损坏拒绝。Go 全套（含 R5a 对照真 `update.py`）和 80 个相关 Python 测试通过。安装器 7 个 `test_plugin_bundle_activation` 失败是 develop 上本来就有的（已在 TODO）。

**真实整栈演练**（隔离栈 `r5b`：后端、Encore×2、broker-owner/mosquitto、前端、PostgreSQL+TimescaleDB、Redis；TLS 发布服务器（自签 CA）限速；真实 `agent_host.py --system --release-source` 启动 Agent；设备 1 台 + 20 万行遥测；每个版本一个签名发布包，镜像为同一后端代码按不同版本号/迁移/入口构建）：

| 场景 | 结果（均看了升级前后的镜像、`sys_version`、迁移表、设备数、遥测行数、配置哈希、维护标记、journal、`system verify`） |
|---|---|
| 成功 v1.3.0→v1.3.1（含一条迁移） | `committed`，**52 秒**，迁移表出现，`sys_version` 25→26，设备和 20 万行不变，journal `complete`；之后 v1.3.2、v1.3.5、v1.3.8、v1.3.10、v1.3.12、v1.3.14、v1.3.16 又各成功提交一次（其中几次是我的演练脚本没拦住的“意外成功”，见下） |
| ① 迁移报错（v1.3.6，待执行迁移 `SELECT 1/0`） | `rolled_back`，86 秒，站点回到旧版本，半成品表不存在，数据行数不变 |
| ② 新镜像起不来（v1.3.3，入口 `/bin/false`） | `rolled_back`，89 秒，回到旧版本 |
| ②’ 迁移成功但健康门不过（v1.3.7：后端迁移到 27 + 起不来的新 Encore 镜像） | `rolled_back`，55 秒；**迁移表被回退掉、`sys_version` 回到 26**，数据不变 |
| ③ kill -9：`preflight` | 恢复 → `aborted`，站点没变 |
| ③ kill -9：`staging` | 恢复 → `aborted`，站点没变 |
| ③ kill -9：`snapshot_done`（实际死在 `stopping`，控制面已停） | 恢复 → `aborted`，控制面重新拉起，维护标记关闭 |
| ③ kill -9：`applying` | 恢复 → `rolled_back`，28 秒 |
| ③ kill -9：`applied`（实际死在 `starting`，compose 辅助容器还在跑，杀死时后端已是新镜像） | 恢复 → 清掉辅助容器 → `rolled_back`，56 秒，回到旧镜像、旧 `sys_version` |
| ③ kill -9：`rolling_back`（回退进行中被杀） | 恢复 → 继续回退 → `rolled_back`，25 秒 |

## 演练里发现的问题
1. **没有发现 Agent 的代码缺陷**；但这几件事如实记下：
   - 我的演练数据用了 2023 年的时间戳，被后端自己的“数据保留 15 天”定时清理任务在 07:02:00 删光，造成一次“回退后遥测变 0”的假象；已用 `data_policy.last_cleanup_time` 证实，换成当前时间戳重做，行数全程 20 万。（原日志保留为 `scenario-2-first-run-rows-deleted-by-retention-job.log`。）
   - 我的触发杀进程脚本三次出错（轮询漏掉了很短的阶段，导致升级意外成功；后来 `docker logs --since` 的时区写错，旧日志行误触发）。这些记录改名为 `INVALID-*`/`scenario-1b/1c`，没有拿来当证据。
   - OrbStack 的 Docker 引擎在收尾清理时又停了一次，重启后清理完成。
2. 我设计的第一版“迁移报错”和“迁移成功但门不过”场景，因为数据库版本号已经等于目标，**根本没执行迁移**（`scenario-2-FLAWED…`、`scenario-4-FLAWED…`）；发现后用待执行的迁移重做。

## 没做 / 没验证（如实）
- **`needs_human` 没有在真实栈上演练**（回退失败的路径只有单元测试：回退每一步注入失败）。
- **kill -9 在 `verifying` 阶段没有在真实栈上打中**：`compose up` 会等被依赖的后端健康（前端 `depends_on`），所以进入 `verifying` 时整栈已经健康，门瞬间通过，杀进程总晚了一步。该点只有单元测试（该阶段被杀 → 回退，与已演练的 `applying/starting` 同一条回退路径）。`stopping` 没有恰好打中，只打中了 `snapshot_done` 紧后。
- 辅助容器的 `docker:27-cli` 镜像在预检时从 Docker Hub 拉（演练里就是这么拉的）；没有离线模式；`thingsvis-*` 服务（演练栈里没部署）没验证；没有测 Windows；没有测“发布包里 compose 的服务集合变化”（新增/删除服务）。
- **健康门只看“某一刻整栈健康”**，不监视之后的崩溃；回退用快照还原数据，**快照之后到回退之间写入的设备数据会丢**，维护窗口里设备上报没有缓冲，也没有测量丢了多少条（和 R4 一样，第二期）。
- **update.py 没有把 `--release-source/--release-trust-root` 传给 `agent_host.py`**（要把发布源和信任根的来源接进 `install/upgrade` 脚本，涉及签名发布链）；现在要启用升级，运维需手动用这些参数重跑 `agent_host.py up --system`。真正的“生产一键”留给负责人决定。
- Agent 升级自身（`system.json` 由 Agent 二进制生成）：升级后要重新 `up --system` 才刷新配置。

## 需要你做的
- 审查并决定是否合并安装器 `wp/R5b-upgrade`（合并后需要发布新版 Agent 二进制才有 `system upgrade`）。
- 决定发布源/信任根怎么接进安装脚本（见上）。
- `r5a-encore:dev`（含 `brokerscope` 的 Encore 镜像）仍保留给 R6 的整栈演练；其余 r5b 的容器、卷、网络、镜像、临时目录已删除。
