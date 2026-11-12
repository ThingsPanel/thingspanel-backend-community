# R5a 交付说明：配置事务移植到 Go，并接上 Agent 生产启动（2026-11-12）

- 安装器分支 `wp/R5a-txn`（从安装器 develop `1c281cc` 切出，最后提交 `ccb97fb`，4 个提交），**未合并未推送**。
- 后端分支 `wp/R5a-done`（从后端 develop `efa5e596` 切出）：本说明、`scripts/build-encore-image.sh` 补上 `brokerscope`、`docs/WORKSPACES.md`、`docs/TODO.md`。
- 演练证据：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R5a/`（`go-test.log`、`wiring-checks*.log`、`scope-endpoint.log`、`agent-up.log`、`encore-build.log`）。

## 做了什么

### 1. `update.py` 的配置事务移植到 Go（`agent/config_fs.go`、`config_txn.go`、`release_verify.go`）
- **同一个锁、同一个 journal**：`<root>/.thingspanel-update/lock`（flock）、`journal.json`（字段、阶段、校验规则、备份/暂存目录布局与 Python 一致）。任何一边中断的事务，另一边能恢复；两边同时跑，后到的一方被锁拒绝。
- 语义对照：先把整个负载下载到私有暂存目录并校验，期间不碰任何目标文件；逐个文件备份（内容、权限、属主都记录）；替换前先写 journal；每次替换前后重新核对被监视文件（`.env` 等）和目标文件，被人改过就停下，不覆盖；中断后恢复按“先检查全部目标、再倒序还原”，缺备份或有外部修改一律拒绝并保留 journal；`activation_started` 之后不回滚（不声称回滚过数据库或服务）；已完成的安装可搬迁，未完成的绑定原路径；符号链接目标和符号链接状态目录不跟随。
- **签名 release 校验**（Go 版 `release_manifest.py`）：ECDSA P-256 + SHA-256 DER；公钥只来自调用方给的 PEM，没有则关闭失败；发布者、版本、有效期、撤销（带外 + 清单内）、文件大小和 SHA-256、清单没列的文件、符号链接，原因代码与 Python 相同，不回显清单内容。
- 默认下载器：只接受 HTTPS（含重定向）、180 秒、16 MiB。

### 2. Agent 生产接线
- `tp-agent -provision-system DIR -project P`：由 Agent 自己写出已审定的 `system.json`（组件启动顺序、备份范围——不含任何密钥文件、版本命令、范围同步配置）和系统 token（仅首次生成，0600，已有的保留，过短的替换）。
- `tools/agent_host.py up --system`：调用上面的命令，把系统角色放在 `.tp-agent/system/`（**和后端容器挂载的 `.tp-agent/run/` 是不同目录**），挂载栈目录、备份目录、状态目录，让 Agent 加入栈网络（健康探测要按服务名访问），并给 Agent 传 `-system-socket/-system-token-file/-system-config`。
- Broker 范围同步交给 Agent：`--system` 时停掉 Python 看门狗，`scope_sync.py ensure` 在系统角色已配置时直接让位，不会再起第二个看门狗。
- `update.py`：激活时给 `agent_host.py` 加 `--system`；`PLUGIN_HELPER_PROFILES` 新增一组摘要（旧的一组保留）；四个入口脚本（`install.bash`、`upgrade.sh`、`install.core.ps1`、`upgrade.ps1`）的 `UPDATE_ENGINE_SHA256` 换成新的 `update.py` 摘要（测试 `test_entrypoint_helper_pins_match_reviewed_bytes` 校验）。
- **部署上必须配套的改动（R2G 交付说明里列过、但没人做过，这次在演练里发现并补上）**：`docker-compose.yml` 的 `encore-api` 加 `GOTP_BROKER_SCOPE_TOKEN`（`${TP_BROKER_SCOPE_TOKEN:-}`，空则路由不存在）和 `TP_RELATIONAL_*_PLATFORM_*`（平台库）；`runtime.encore.json` 的 `hosted_services` 加 `brokerscope`；`prepare.py` 首次生成 `TP_BROKER_SCOPE_TOKEN`（已有则保持）；Agent 的范围 URL 改为 `http://encore-api:14000/internal/broker-scope`；本地 Encore 镜像构建脚本补上 `brokerscope`（CI 工作流 R2G 已补）。

## 怎么验证的

| 内容 | 命令/方法 | 结果 |
|---|---|---|
| Go 全部测试（含 R1–R4 原有） | `docker run golang:1.22`（装 python3、openssl）`go vet ./... && go test -race -count=1 ./...` | 通过（`go-test.log`） |
| 事务场景（Go 版，对照 `test_update_transaction.py`/`test_signed_update.py` 逐项） | `config_txn_test.go` | 通过：暂存先于任何替换；任一文件下载失败整体不变；坏 JSON/空文件/版本不符在提交前拒绝；**每个文件的替换前、替换后各失败一次，原文件和权限逐字节还原**；原本不存在的文件和新建目录被清掉；用户在校验后改 `.env`、替换中途改受管文件，改动都保留；激活失败保留已提交配置并拒绝下一次；完成的 journal 不回滚、可搬迁；损坏 journal、缺备份、符号链接失败关闭；真实 SIGKILL 子进程（3 个中断点）由新进程恢复，恢复本身幂等；锁被活进程持有时拒绝，进程死后释放 |
| 签名校验矩阵 | 11 种输入（合法、被篡改、他人密钥、清单未列、两种撤销、过期、未生效、他人发布者、版本不符、无信任根） | 通过，拒绝原因与预期一致 |
| **与 Python 实现对照（运行真的 `update.py`/`release_manifest.py`）** | `config_interop_test.go` | 通过：Python 被 SIGKILL 的事务由 Go 恢复（3 个点）；Go 被 SIGKILL 的由 Python 恢复；Go 完成的 journal Python 能接着做、Python 完成的 Go 能接着做；Go 持锁 Python 被拒、Python 持锁 Go 被拒且不改任何文件；两个校验器在 11 种输入上给出相同结果 |
| Python 安装器测试 | `python3 -m unittest discover -s tests` | 155 个里 7 个错误，**与 develop 完全相同的 7 个**（`test_plugin_bundle_activation`，“Downloaded plugin helper does not match the installer pin: tools/plugin_catalog.py”，develop 上就存在，未修，写入 TODO）；新增测试和 `test_update_transaction`、`test_reconcile_activation` 全过 |
| `prepare.py` 生成范围令牌 | 临时目录里连跑两次 `configure()` | 生成 64 位十六进制一次，重跑不变 |
| **整栈演练（真实）** | 隔离栈 `r5a`（后端、Encore×2、broker-owner/mosquitto、前端、PostgreSQL、Redis），**用真正的 `tools/agent_host.py up --system` 启动 Agent** | 见下 |

整栈演练结果（`wiring-checks*.log`）：
- `system status` / `verify`：8 个组件全部 `ok`，退出码 0。
- 令牌隔离：系统令牌打插件 socket → 404（系统路由不存在）；插件令牌打系统 socket → 401。后端容器里 `/run/tp-agent` 只有 `agent.sock` 和 `token`，看不到系统 socket、`/run/agent`。
- `system backup create` → 约 1 秒完成，`backup list` 显示包 `valid:true`，平台版本从运行中的后端容器读到。
- `system restart scope=control` → `done`，100%，整栈健康门通过。
- **范围同步经 Encore 真实跑通**：Agent 启动即 `applied`；建一台设备后下一轮 `applied`，`services.json` 里出现该设备；这是 R2G 的 Encore 端点**第一次在真实 Encore 进程里被请求**：带令牌 200，无令牌 401，经前端 nginx 访问返回 SPA 页面而不是 JSON（没有对外转发）。
- 演练栈（容器、卷、网络、`r5a-backend:dev`、旧的 `r2e-encore:dev`、busybox、临时目录）已删除；保留 `r5a-encore:dev`（含 `brokerscope` 的 Encore 镜像，给 R5b 用）。

## 演练中发现并已处理的缺陷
1. **R2G 的 Encore 端点按原交付物根本不会生效**：`runtime.encore.json` 没有托管 `brokerscope`（404）、`encore-api` 没有令牌和平台库环境变量、本地镜像构建脚本没带这个服务。已在安装器和后端脚本里补上（见上）。
2. `tp-agentctl system backup/create`（写成一个词）会 panic（切片越界）。已修，会回到正常的 `system backup create` 解析。
3. 我自己的演练里 Agent 二进制是旧的，导致 `-provision-system` 写出旧 URL，`scope_fetch_failed`；重建后恢复。这是演练操作问题，不是代码缺陷，但说明：**配置由 Agent 二进制生成，升级 Agent 后需要重新 `up --system`** 才会刷新 `system.json`。

## 没做 / 没验证（如实）
- **没有移植**：激活尾段（`prepare.py --bootstrap`、`compose up`、验证）、`write_activation_file`（Go 只在读写 journal 时原样保留 `activation_files`/`activation_dirs`，并校验它们）、`ast.parse` 暂存的 `.py`、入口脚本对 `update.py` 的摘要互锁检查（Go 没有对应物，由签名清单替代）。这些留给 R5b 的升级编排。
- Go 版版本一致性检查是对暂存 `docker-compose.yml` 做文本扫描找 `backend` 的 `image:`，不是 `docker compose config` 渲染（Agent 容器里没有 docker CLI）。
- 默认 HTTPS 下载器没有对真实网络测过（测试用内存下载器）。
- 互操作测试需要 `python3`（和 `openssl`），没有时会跳过并说明；我是在 Docker 里用 apt 装上跑的，不装就跑不了这几个测试。
- **没有走完整的 `install/upgrade` 流程**：`update.py` 里 `start_plugin_agent` 加 `--system` 只有单元测试（命令参数断言）；真正执行的是我手动用同一个 `agent_host.py` 启动。没有在全新机器上装过，没有验证“旧版本升级上来”时 `.env` 里新增 `TP_BROKER_SCOPE_TOKEN` 的全过程（只验证了 `prepare.py` 的生成逻辑）。
- 维护页（nginx）、主机重启后自动恢复仍未做；`.tp-agent/status` 目录在 0700 的父目录里，将来给 nginx 读要调整（R6）。
- 用户自己的 `tp-base` 栈上有一个 Python `scope_sync.py watch` 常驻进程，**没有动**。

## 需要你做的
- 审查并决定是否合并安装器 `wp/R5a-txn`；后端 `wp/R5a-done` 只含文档和一个脚本词。
- 合并后下一次发布需要：重新构建并发布含 `brokerscope` 的 Encore 镜像（CI 工作流已加；`deploy/community-test/deploy_encore.py` 的 `native_services` 另见 TODO）。
