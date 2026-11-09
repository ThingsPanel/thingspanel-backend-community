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

## 三、2026-11-09 已定：先做“冒烟”，基线用 develop

负责人决定：**发布到 main 之前，都用 develop 做测试；目标只是证明这套东西“能用”**——能上传代码、生成制品、点击一键安装、一键卸载，卸载后清干净再装一遍成功。其余小问题记在 `docs/TODO.md`，不挡冒烟。

| 部分 | 冒烟用的基线 | 说明 |
|---|---|---|
| 后端 | 本地 `develop` @ `c0ab14d1` | 远端 `develop` 是 `9d071b7`，本地是它的**快进**（领先 183 个提交，没有分叉）。推上去就是把这 183 个提交公开到远端 develop |
| 前端 | 远端 `origin/develop` @ `76298db6` | 已含插件安装页、租户菜单、资源中心跳转（我核对过文件和路由）。本地 develop 和它分叉（各有几个提交，内容等价但哈希不同），**以远端为准，不用推** |
| Broker | 同后端仓库（`deploy/mosquitto-community`） | 随后端 Tag 一起构建 |
| HA 插件 | 已发布 `1.0.0`，不重做 | 用 `verify-public-release.sh` 核对 |
| 签名公钥 | 现有候选公钥 | 是否算正式，进 main 之前再定 |

**怎么生成制品（不发布 Release）**：`release.py select-tag` 只要求远端有这个 **Git Tag**，不要求 GitHub Release。所以可以只推一个预发布 Tag（如 `v1.3.1-rc.1`），再手动运行 `docker-image-build.yml`（workflow_dispatch，version 填该 Tag），不会触发 `release.yml` 那条正式发布流水线。后端仓库和前端仓库各做一次。

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
