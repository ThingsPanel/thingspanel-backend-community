# R3 交付说明：备份（2026-11-12）

安装器分支 `wp/R3-backup`（从安装器 develop `ba64837` 切出，最后提交 `23d662f`，**未合并未推送**）。本文件在后端分支 `wp/R3-done`（从后端 `origin/develop` `aebaed52` 切出）。

## 做了什么
`tp-agentctl system backup create|list|verify|delete`（system 角色新增 4 个固定动作，仍是独立 socket/token，请求里只有 `name`，且名字必须匹配 `tp-backup-<时间>-<版本>-full.tar`，不能带路径）。全部 Go（`agent/backup.go`、`docker_stream.go`，改了 `system_service.go`、`system.go`、两个 cmd）。

- **备份包**：一个 tar：`db/<库名>.dump`（`pg_dump -Fc`，**服务器上所有非模板库逐个导出**，含 TimescaleDB 历史数据）、`files/files.tar`（backend 容器里上传文件目录，用 Docker 的 archive 接口，容器停着也能取）、`config/…`（只有白名单文件：compose、nginx、`config/native` 下的 prepare/update/runtime*.json/tick.sh）、`plugins/instances.json`（插件实例 ID、代次、资源名，不含请求键/负载哈希/密钥）、最后一个条目 `manifest.json`。manifest 记录格式版本、平台版本、Agent 版本、触发方式、PG 与 TimescaleDB 版本、`sys_version`、各 Encore 库的 `schema_migrations` 最大值、每个文件的 sha256 和大小、`includes.history=true/secrets=false`。目录 `<安装目录>/backups/`，权限 0700。
- **PG Adapter**：`BackupAdapter` 接口（`Name/Describe/Estimate/Dump`），第一期只实现 PostgreSQL，`pg_dump` 在 postgres 容器里执行（版本与服务端一致）。`Restore` 方法留给 R4。
- **一致性**：在线备份（各库逐个 `pg_dump`，库与文件之间允许秒级偏差；升级/恢复前的停机快照在 R4/R5b 做）。
- **原子性**：先把每个库导出到 `<名>.partial.d/`，再写 `<名>.tar.partial`，`fsync` 后 `rename` 成 `.tar`，再写同名 `.json` 侧文件；任何时候被杀，只会留下 `*.partial*`，它们不会被列出、不能被 verify，下一次备份开始时清掉。`list` 只读取 `.tar` 里的 manifest。
- **空间预检**：可用空间必须 ≥ 2 倍估算（各库大小之和）+ 64 MB，否则 `insufficient_space`，什么都不写。
- **保留**：非手动触发（pre-upgrade / pre-restore / scheduled）只保留最近 `keep_auto`（默认 3）份；手动备份不自动删除。
- **verify**：只靠这个文件就能校验：逐条目重算 sha256 与大小，对照包内 manifest；拒绝符号链接、路径穿越、非白名单前缀、含密钥文件名的条目、`includes.secrets=true`、多余或缺失的文件。
- **不进包的东西**：`.env`、broker token、`services.json`、`owner-tokens.json`、Agent `secrets.json`、私钥/证书（配置白名单在系统配置里校验，含这些名字会拒绝启动）。

## 测试
**单元测试**（Linux 容器，`-race`）：创建后可 verify 且无密钥条目；dump 失败不留任何文件；陈旧 `.partial` 不被列出、不能 verify、下次备份清理；篡改一个字节/截断都被 verify 拒绝；空间不足提前拒绝；保留策略（手动全留，自动留最新 N）；名字校验与 `delete` 不会碰目录外的文件；配置里放 `.env`/`services.json`/`../` 被拒绝；文件卷不存在仍能产生有效备份；Docker 流解复用（stdout/stderr）。

**真实演练**（隔离栈 `r3`，含 Encore；脚本 `stack.sh` 与日志在 `/Volumes/KIOXIA/archive/evidence/2026-11-12-R3/`）：
- 数据：`telemetry_datas`（TimescaleDB hypertable）先 5000 行，后加到 300 万行（库约 800 MB）；一个租户和设备。
- 创建耗时：同一个约 800 MB 的库（300 万行），**一次约 2 分钟、另一次只要 4 秒**，包都是 21 MB。第一次发生在刚插完大批量数据之后、磁盘很忙的时候，所以**没有可靠的“每 GB 耗时”数字**，我之前写的“≈2.5 分钟/GB”不成立，已删除；合成数据压缩得很好，真实数据的包不会这么小。
- 独立校验：把包解开在宿主机上，用 Python 逐个重算 sha256（13 个文件全对）；`pg_restore -l` 能读出 143 个表数据条目和 `telemetry_datas`；包内没有 `.env`、`services.json`；搜索 13 个 `.env` 里的密钥**值**，只命中 `TP_DATABASE_PASSWORD`/`TP_NATIVE_DB_PASSWORD`，经查是仓库里公开的默认值字符串 `postgresThingsPanel`（写在 `docker-compose.yml` 和 `prepare.py` 默认值里），不是该栈生成的密钥。**注意：如果运维把数据库口令改成自定义值而又写进 compose，它会在包里。**
- `kill -9`：① 在 dumping 阶段 `docker kill -s KILL` Agent：只留下 `*.partial.d`，没有 `.tar`，重启后 `list` 只显示完整的包，下一次备份清掉残留；② 在出现 `.tar.partial` 时杀：这次杀的时机正好在 rename 之后，得到的是完整、`verify` 通过的包；**没有抓到“恰好在 rename 之前”的瞬间**，这一条只有单元测试（失败/中断后无 `.tar`）和 ① 作证；③ 演练中 OrbStack（Docker 引擎）自己停了一次，等于整机掉电：Agent 与备份被一起杀掉，现场与 ① 一致（只有 `.partial.d`，不被列出）。
- 重启 Agent 后三个包全部 `verify` 通过。

## 没做 / 注意
- **恢复**不在本卡（R4）；**升级前自动快照**（trigger=pre-upgrade/pre-restore 的调用、停控制面再备份）在 R4/R5b；本卡 API 只有手动备份。
- 未做：备份下载、界面、定时备份、加密、不含历史数据（均在计划的第二期）。
- **平台版本取不到时**（backend 容器没在运行）manifest 里是 `unknown`，包名里也是 `unknown`；演练里 Docker 重启后的那一份就是这样。R4 的版本兼容规则必须把 `unknown` 当作“不能自动判断”处理；更好的办法是从镜像标签取版本，待 R4/R5b 一起定。
- manifest 的 `images`（镜像摘要）字段计划里有，本卡**没有写**（需要 Docker inspect 镜像，R5b 升级回退才用得到）。
- 备份目录默认在安装目录里（与数据同一块盘）：整机故障时一起丢。异地存储是第二期。
- 仍未接入生产：Agent 的 system 参数、备份配置块由谁生成，并入 R5a。
- `files/files.tar` 的体积没有计入空间估算（只算了数据库），文件很大时预检偏乐观。
- Agent 访问 Docker 的方式与 R1/R2 相同（挂载 docker.sock）；备份功能没有增加新的 Docker 权限类别，但新增了“读容器内文件（archive）”和“在 postgres 容器里执行 pg_dump/psql”，二者的命令都是写死的。

## 演练现场（保留给 R4，R4 结束后清理）
容器 `r3-*`（含 `r3-agent`）、卷 `r3_*`、网络 `r3_thingspanel_network`、镜像 `r3-backend:dev`、目录 `/Volumes/KIOXIA/tmp/r3-work`（含临时凭据和 3 个备份包）。
