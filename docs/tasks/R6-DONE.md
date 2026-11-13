# R6 交付说明：系统管理界面（2026-11-13）

R6 分在三个仓库、三个分支（**都未合并未推送**，叠在未合并的 R5b 上）：

| 仓库 | 分支 | 最后提交 | 内容 |
|---|---|---|---|
| 后端（本分支） | `wp/R6-system-api` | 见 `git log` | Encore `system` 服务的管理 API、Agent 客户端、本说明 |
| 安装器 | `wp/R6-agent`（叠在 `wp/R5b-upgrade` 上） | `git log -1` | Agent 的配置检查/更新检查接口、系统 socket 目录拆分、nginx 维护页、compose/nginx 修复 |
| 前端 | `wp/R6-frontend`（从前端 `develop` 切出，由单独会话完成） | `c005631b` | ⋮ 菜单、重启对话框、更新列表、备份页、维护遮罩；说明在该仓库 `docs/R6-FRONTEND-DONE.md` |

演练证据：`/Volumes/KIOXIA/archive/evidence/2026-11-13-R6/`（`screens/` 是界面截图，`watch-*.log` 是升级期间浏览器能看到什么）。

## 做了什么
- **Encore API**（`encore-api/system/api.go`，仅超级管理员；其余角色 403）：`GET /api/system/status`、`POST check-config`、`POST restart {scope}`、`GET updates`、`POST upgrade {to}`、`GET/POST /api/system/backups`、`POST /api/system/backups/:name/restore`、`DELETE /api/system/backups/:name`。每个都是对 Agent 系统 socket 的**固定请求**，Encore 自己不重启/备份/升级任何东西；请求字段先在这里校验（scope 枚举、版本号格式、备份名格式），Agent 再校验一次；错误统一成 `unavailable`（没有系统管理器）、`failed_precondition`+固定码（忙、配置无效等）。契约见共享笔记 `R6-system-api-contract.md`。
- **Agent 新接口**：`/v1/system/check`（配置检查，来自重启预检的同一段代码）、`/v1/system/updates`（读签名清单并按升级同样的规则验证，只回答版本和有效期）。
- **隔离**：系统 socket 和 token 移到专用目录 `.tp-agent/system-api/`，**只有 encore-api 容器只读挂载它**（Gin 后端和插件容器看不到）；Agent 自己的配置和升级状态留在 `.tp-agent/system/`，不挂给任何平台容器。这是对 R1 “系统 socket 不得挂进后端容器”的明确调整：计划 §3.5 的架构就是 Encore 作为调用方，现在把暴露面缩到“一个容器、一个只含 socket 和 token 的目录、仅超级管理员接口、固定动作”。
- **维护页**（nginx，`niginx.conf`）：Agent 的维护标记存在时除 `/system-status/status.json` 外一律 503；`/api`、`/files`、`/automation-core` 返回 JSON `maintenance:true`，页面导航得到内置的静态进度页（轮询状态文件，完成自动刷新，回退/需人工有不同文字）；`/api/system/` 路由到 Encore。
- **前端**：见前端分支说明。

## 怎么验证的

**单元测试**（不等于整栈验收）：Go 全套（含 R1–R5b）`-race` 通过；Encore `system` 和 `internal/systemagent` 通过（用 Unix socket 上的假 Agent：请求路径/令牌、busy/unavailable 映射、非法输入不到达 Agent、每个处理函数对匿名/租户管理员/普通用户 403、管理员放行并记录动作）；Python 83 个相关测试通过（含新增 nginx/compose 回归测试）；前端 24 个单元测试 + typecheck + build（见前端说明）。

**真实整栈演练**（隔离栈 `r6`：后端、Encore×2、broker、PostgreSQL+Timescale、Redis、**新前端镜像**、TLS 发布服务器；20 万行遥测；真实 `agent_host.py --system --release-source` 启动 Agent；真实浏览器登录超级管理员）：
1. **API 全链路**：浏览器同源路径 → nginx → Encore → Agent 的 socket，`status/check-config/updates/backups` 全部返回真实数据；租户用户 403；非法 scope/备份名 400/404。
2. **界面**：系统设置页出现 ⋮ 菜单（检查更新/重启系统）和“备份”标签；检查更新列出“ThingsPanel v1.3.0 → v1.3.2 [升级]”和 tp-agent“请用安装器升级”；重启对话框先做配置检查再允许重启。
3. **完整演示（检查更新 → 升级 → 故意失败 → 自动回退）**：在界面点升级 v1.3.2（迁移 `SELECT 1/0` 的坏发布）→ 遮罩依次显示 preflight/snapshot/…/回退 → 最后“操作失败，系统已自动回退到之前的状态，错误码 upgrade_failed_rolled_back”，站点回到旧版本、数据 20 万行不变（截图 1–5）。后来在新 compose 上又用 API 重复（v1.3.3 迁移失败）并再次回退。
4. **成功路径**：界面升级 v1.3.4（带迁移）→ 遮罩（步骤名已翻译）→ 完成后页面自动刷新，页脚版本变为 1.3.4，迁移表存在，数据不变（截图 6–7）；界面“重启系统（平台服务）”约 37 秒完成，自动刷新。

## 演练里发现并修掉的问题（都是整栈才暴露的）
1. **后端起不来时，前端（和维护页）一起消失**：compose 的 `frontend.depends_on.*.restart: true` 会在依赖被重建时重启前端，并等依赖健康才启动——新后端失败时前端一直没起来，遮罩显示“服务暂时不可达”，直到回退完成。修了三处：去掉 `restart: true`；nginx 对会被重建的后端/Encore 用 `resolver` + 变量按请求解析（重建后新地址自动找到）；Agent 的 `compose up` **不再包含 frontend**，只在健康门通过（升级成功或回退成功）之后才单独更新前端并 `nginx -s reload`（`UpdateProxy`，有单元测试）。第二次坏升级演练：升级全程 `status.json` 一直 200、前端一直在。
2. **升级成功后 `niginx.conf` 不会被应用**：compose 不会因为挂载文件内容变了而重建服务；Agent 现在在 `compose up` 和回退之后 `nginx -s reload`。
3. 升级过程的步骤上报粒度太粗（停服务时一直显示“snapshot”）：补了 `stopping` 步骤。
4. 更新列表里 tp-agent 一行带了 `0001-01-01` 的有效期：改成指针，缺省不输出。
5. 整栈演练中如果新版本一直不健康，**健康门要等满预算（默认 5 分钟）才回退**（compose 不再因前端依赖失败而提前报错）；最坏情况一次失败升级约 7 分钟。可以通过 `upgrade.verify_seconds` 调整，没有改默认。

## 没做 / 没验证（如实）
- **备份下载**（第一期不做）、自动备份设置、上传备份恢复。
- 备份页的**恢复/删除/立即备份**只在 API 层和 R3/R4 的整栈演练里验证过；这一轮在界面里**没有点过**恢复和删除。
- `needs_human` 的界面文字没在真实失败上看到（只有前端单元测试 + 遮罩桩数据）。
- **本地 `r6-frontend:dev` 来自前端会话在 develop 之上的分支，没有 PR/发布**；发布流水线（`encore-image-build.yml` 的服务清单只有 5 个服务，缺 automation/notification/brokerscope/system 等）不属于本卡，已写入 TODO。
- 插件更新还没有（R7）；Agent 自升级没有（第二期）。
- Encore 管理 API 没有写操作日志/审计表（只有 Encore 日志里的“system action”一行，不含用户内容）。
- 演练中发现用户另外两套栈（`tp-base`、`thingspanel-community-v130`）的后端容器在反复重启（重启计数 140+，早于我的演练）；与本卡无关，没动。

## 需要你做的
- 审查三个分支，决定合并顺序（安装器 R6 叠在 R5b 上；R5b、R6 都未合并）。
- 发布侧：新前端镜像、含 `system` 服务的 Encore 镜像、新 Agent 二进制，以及发布清单/信任根怎么接进安装脚本（R5b 遗留）。
