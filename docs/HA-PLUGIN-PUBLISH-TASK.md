# HA 插件一键安装 — 镜像发布任务书

> 给负责发布的人。读完这一页就能做。配套入口文档：[`HA-PLUGIN-HANDOVER.md`](HA-PLUGIN-HANDOVER.md)（里面有流程图和整体说明）。
> 日期 2026-11-09。**状态会变，动手前先核对各仓库当前提交。**

## 一、为什么要发

公开安装脚本（`curl -fsSL https://install.thingspanel.io/install.sh | sh`）装出来的平台，默认用的是 **v1.3.0** 的三个镜像。HA 一键安装需要的东西，v1.3.0 里**没有**：

| 镜像 | v1.3.0 里缺什么 | 怎么确认的（2026-11-09 实测） |
|---|---|---|
| 后端 `thingspanel-go` | 没有插件安装引擎、没有 `sql/34.sql`、`35.sql`（没有安装接口） | `docker run --rm --entrypoint sh <镜像> -c 'ls /go/src/app/internal/plugininstall /go/src/app/sql/3*.sql'`，v1.3.0 报“不存在”，只有 `3.sql` |
| 前端 `thingspanel-vue` | 没有“插件安装”页面和租户的 HA 接入点入口 | 在 v1.3.0 静态资源里搜不到 `plugin/installs` |
| Broker `thingspanel-mosquitto` | 插件设备连不上 | **对照实验**：同一个栈、同样的设备范围、同一个 HA 插件（重启过），只换 Broker 镜像。v1.3.0：插件所有连接都 `not authorised`，HA 数据进不了平台，平台开关灯 HA 没反应；develop 线镜像：温度 93.3 进了平台，灯 off→on→off，回执 `result:0` |

**注意**：HA 插件本身（Agent 和镜像包）**已经**发布了，在 GitHub Release `plugin-ha-v1.0.0`，目录签名也上线了，**不用再发**。可以用 `docs/ha-plugin-handover/tools/verify-public-release.sh` 自己核对。

## 二、要发什么（三个镜像 + 一处改动）

| # | 内容 | 仓库 | 怎么发 |
|---|---|---|---|
| 1 | 后端镜像 `thingspanel-go:<新版本>` | `ThingsPanel/thingspanel-backend-community` | 工作流 `.github/workflows/docker-image-build.yml`：在**已有的 Release Tag** 上运行，`version` 填 Tag |
| 2 | Broker 镜像 `thingspanel-mosquitto:<新版本>` | 同上（同一个工作流的 `broker` 矩阵项，Dockerfile 为 `deploy/mosquitto-community/Dockerfile.production`） | 同 1，一次构建出两个 |
| 3 | 前端镜像 `thingspanel-vue:<新版本>` | `ThingsPanel/thingspanel-frontend-community` | 该仓库自己的 `.github/workflows/docker-image-build.yml`，同样按 Tag |
| 4 | 安装器里的默认镜像版本 | `ThingsPanel/thingspanel-installer`（公开站点 `install.thingspanel.io` 现在由分支 `codex/ha-plugin-public-site-20261108` 发布） | 把 `docker-compose.yml` 里写死的 `v1.3.0`（前端第 3 行、后端第 98 行附近、mosquitto 两处）改成新版本，重新发布站点 |

镜像会同时推到 **ghcr.io** 和 **阿里云 `registry.cn-hangzhou.aliyuncs.com/thingspanel/`**（工作流里已经配好，用的是仓库的 Actions Secrets，所以需要有这个仓库的写权限）。要求 **amd64 + arm64 两个架构**。

## 三、开工前必须先定的两件事（需要负责人答复，发布的人别自己决定）

1. **被测/被发布的代码基线**。要打 Tag，Tag 对应的提交必须**包含**这次的改动（`release.py select-tag` 会检查）。现在这些改动**都还没合入**远端 `develop` / `main`：
   - 后端：本地 develop `c0ab14d1`（含续租修复）；远端 develop 是 `9d071b7`，两者不是一条线；发布线的候选是 `wp/t7-ha-release-backport`（`91b1e958`）。
   - 前端：`wp/pui-frontend` `c40ad217`（工作区还有 4 个未提交路由文件，不要覆盖）。
   - 具体选哪个、怎么合、版本号叫什么（例如 `v1.3.1`），**由负责人定**。
2. **签名用的信任根**。公开的 HA 目录现在用“候选公钥”签（指纹 `6f6e902e807b86949e9dc3090de82eb932c27c6bd7f95987c8f44de00cca4fe8`），公钥写在安装器 `config/native/update.py` 里。这把钥匙算不算正式的，需要负责人确认；如果要换，目录要重新签、安装器里的公钥要同步改，HA 目录和 Release 也要重发。

## 四、步骤（基线和版本号定下来之后）

1. 在各仓库把确定的提交合进发布分支，打 Tag，创建 GitHub Release。
2. 运行后端仓库的 `docker-image-build.yml`（填 Tag），等两个矩阵项（backend、broker）× 两个架构都成功。
3. 运行前端仓库的 `docker-image-build.yml`（填 Tag）。
4. 改安装器 `docker-compose.yml` 的镜像版本，更新公开站点（站点从哪个分支发布，见上表；注意不是从 `main` 自动同步）。
5. **验收（必做，不能省）**，在一台**干净的机器**上（最好 amd64 和 arm64 各一台）：
   - 先确认镜像里有东西：
     ```bash
     docker run --rm --entrypoint sh <后端镜像> -c 'ls /go/src/app/internal/plugininstall; ls /go/src/app/sql/3[345].sql'
     ```
     应列出 `engine.go` 等文件和 `33.sql 34.sql 35.sql`。
   - 运行安装脚本，装完平台后，用超管账号走入口文档 **A 组**（安装），再用租户账号走 **B 组**（接入点、上行、下行）。
   - 同时核对公开制品：`docs/ha-plugin-handover/tools/verify-public-release.sh both` 应“全部通过”。
6. 在记录里写清：版本号、三个镜像的 digest（amd64、arm64）、验收机器、每一步的结果。

## 五、别做的事

- 不要用 `latest`、`dev` 当版本；不要覆盖已有的 `v1.3.0` Tag 或镜像。
- 不要为了赶时间跳过验收第 5 步；“镜像推上去了”不等于“能装”。
- 不要把任何令牌、密码写进工作流日志、Release 说明或文档。
- 不要自己决定第三节的两件事。

## 六、已知的、发布后仍会遇到的问题

见入口文档 §7。其中和发布直接相关的是：插件在 Broker 拒绝它一阵之后，**Broker 恢复了它也不会自己重连，要重启插件容器**（2026-11-09 实测发现）。这是插件的问题，不是镜像问题，但发布后现场排障会碰到。
