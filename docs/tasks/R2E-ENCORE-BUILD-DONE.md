# R2E-ENCORE-BUILD 交付说明

**结论：** `encore build docker` 卡死的根因是 Encore CLI v1.58.6 的 bug，在 git worktree 里必现；与仓库体积、`.encoreignore`、网络、Docker 都无关。
本机已用 `scripts/build-encore-image.sh` 在约 30–50 秒内构建出镜像 **`r2e-encore:dev`**（linux/arm64，98 MB，`scratch` 基础镜像），并验证可启动，`/v1/health` 返回 `status: ok, database: ok`。

## 根因（有证据）
- 复现：在本 worktree 的 `encore-api/` 执行 CI 同款命令，CLI 进程 100% CPU，无输出，无子进程，运行 70 秒仍无进展（`-v -v` 也无输出）。
- `sample` 的主线程调用栈里，唯一的业务帧是 `encr.dev/cli/cmd/encore/cmdutil.WorkspaceRoot`，下面是 `os.Stat` / `filepath.Join` / GC，也就是一个死循环在反复 stat 同一路径。这与上一次会话看到的“路径遍历 / GC”一致。
- 读 v1.58.6 源码（`cli/cmd/encore/cmdutil/cmdutil.go`）：`WorkspaceRoot` 从 app 目录往上找 `.git`；`.git` 不存在才往父目录走，**`.git` 存在但是文件（worktree、submodule 都是文件）时只 `continue`，不更新 `dir`，永远循环**。
- 所以：目录大小、`node_modules`、符号链接都不是原因（`encore-api/` 只有 6.9 MB、680 个文件）；`encore check` 不走这段代码，所以能通过。
- CI 用普通 clone（`.git` 是目录），所以不受影响。上次的 4 次尝试（含 `--config`、`--base`、`--services`）都在 worktree 里，全部掉进同一个循环。

## 做法：从干净导出构建
`scripts/build-encore-image.sh [IMAGE_TAG] [ARCH]`：
1. `git archive HEAD` 导出整个仓库到临时目录（`encore-api/go.mod` 有 `replace ... => ../pkg/relational`，只导出 `encore-api` 会编译失败）；
2. 在导出目录 `git init` 并提交一次：有真实的 `.git` 目录，`WorkspaceRoot` 能结束；也让 `go build` 的 VCS 标记能工作（空 `.git` 目录会报 `error obtaining VCS status`，而且 Encore 不转发 `GOFLAGS=-buildvcs=false`）；
3. 用 `pwd -P` 解析真实路径：macOS 的 `$TMPDIR` 在 `/var/folders`（符号链接到 `/private/var`），Encore 会报 `encore_internal/main: directory not found`；
4. 参数与 `.github/workflows/encore-image-build.yml` 一致：`--services=authn,thingmodel,devicemetadata,fileassets,connectorcatalog,automation,notification,marketinstall,northbound,platformconfig --skip-config`，架构默认取宿主机。

构建的是 `git HEAD`，**未提交的改动不会进镜像**。

```bash
scripts/build-encore-image.sh r2e-encore:dev        # Apple 芯片默认 arm64
```
若不想用脚本：在主目录（非 worktree）的 `encore-api/` 里直接执行脚本里的 `encore build docker` 命令也可以。

## 运行（接 Postgres）
镜像只有一个二进制（入口 `/artifacts/0/build/encore_app_out`，`scratch`，没有 shell、curl，健康检查要从外面做）。必须提供基础设施配置（`--skip-config`）。下面是已验证的最小跑法（容器均以 `r2e-` 开头，数据库为一次性）：
```bash
docker network create r2e-net
docker run -d --name r2e-pg --network r2e-net -e POSTGRES_PASSWORD=r2e-test-pw postgres:16
for d in device_metadata automation notification file_storage connector_runtime; do docker exec r2e-pg psql -U postgres -c "create database $d"; done
# runtime.json 格式见 deploy/local-develop/wiring.py（sql_servers[0].host 写 r2e-pg:5432，password 用 {"$env":"TP_LOCAL_DB_PASSWORD"}）
docker run -d --name r2e-encore-api --network r2e-net -p 127.0.0.1:34000:14000 \
  -v /绝对路径/runtime.json:/run/config/runtime.json:ro \
  -e ENCORE_INFRA_CONFIG_PATH=/run/config/runtime.json -e PORT=14000 -e ENCORE_LISTEN_ADDR=0.0.0.0:14000 \
  -e TP_LOCAL_DB_PASSWORD=r2e-test-pw -e TP_AUTH_MODE=legacy-jwt -e TP_JWT_KEY=<≥32位随机串> \
  -e TP_COMMUNITY_BASE_URL=http://<backend>:9999 -e TP_AUTOMATION_SERVICE_TOKEN=<≥32位随机串> -e TP_SOURCE_DEPLOYMENT_ID=r2e \
  r2e-encore:dev
curl -s http://127.0.0.1:34000/v1/health
```
完整栈（encore-api + notification-core 两个容器、密钥环、MQTT、前端）的环境变量和配置，用 `deploy/local-develop/wiring.py` 生成的 compose 作参考，镜像换成 `r2e-encore:dev` 即可。notification-core 用同一镜像，配置变量是 `NOTIFICATION_ENCORE_INFRA_CONFIG_PATH`（见该文件）。

## 结果
| 项 | 结果 |
|---|---|
| worktree 内直接构建 | 卡死（复现，已杀进程） |
| 脚本构建 `r2e-encore:dev` | 成功，arm64，98 MB，约 30–50 秒（编译缓存热） |
| 无配置启动 | `no encore runtime or infra config provided`（符合预期） |
| 带配置 + `r2e-pg` 启动 | `automation owner starting`，`/v1/health` = ok，database = ok |
| 清理 | `r2e-encore-api`、`r2e-pg`、`r2e-net` 已删；镜像 `r2e-encore:dev` 保留供父会话使用 |

## 没有验证
- 只启动了 `encore-api`（`hosted_services` 含 authn/thingmodel/devicemetadata/fileassets/connectorcatalog/automation），**没有**起 `notification-core`、没有做迁移（空库，只验证了启动与健康），没有接 Community 后端和 MQTT；完整栈演练仍由父会话做。
- 只构建了 arm64；amd64 交叉构建没试。
- 未升级 Encore CLI（有 v1.58.8）；未确认新版是否已修这个 bug，也未向上游报告。
- `--services` 用的是 CI 的 10 个服务；`deploy/local-develop/build.py` 里只列了 7 个（见 TODO）。

## 证据索引
`/Volumes/KIOXIA/archive/evidence/2026-11-12-R2E/`：卡死时的日志与 `sample1.txt`（调用栈）、各次构建日志（`build-fixed*.log`、`build-final.log`）、`health.json`、`run.log`。（`/Volumes/KIOXIA` 在本机存在；任务书写的 500G 卷不在。）

## 提交
单个提交，分支 `wp/R2E-encore-build`：`scripts/build-encore-image.sh`、本文件、`docs/WORKSPACES.md`、`docs/TODO.md`。未推送、未合并。
