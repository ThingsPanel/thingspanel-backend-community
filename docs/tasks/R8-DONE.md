# R8 交付说明：CI 构建的 Encore 镜像补全服务（对应交接文档 B1）

- 分支：`wp/R8-ci-services`（从 `base/origin-develop-20261113` 切出）
- worktree：`/Users/junhong/Downloads/code/thingspanel-backend-community/.claude/worktrees/wp-r8-ci-services-36047e`
- 提交见文末 `git log`

## 查清的事实
- Encore 应用共 12 个服务（有 `//encore:api` 的目录）：authn、thingmodel、devicemetadata、fileassets、connectorcatalog、automation、notification、marketinstall、northbound、platformconfig、brokerscope、system。
- 本地脚本 `scripts/build-encore-image.sh` 已是 12 个。
- develop 上的工作流清单已是 11 个（brokerscope 已在，TODO 里“只有 5 个”的描述已过时），**只缺 `system`**。
- 安装器 `origin/develop` 的 `runtime.encore.json` `hosted_services` = authn、thingmodel、automation、brokerscope、system；`runtime.notification.json` = authn、notification。都在 12 个之内，无遗漏。

## 改了什么
1. `.github/workflows/encore-image-build.yml`：`--services` 末尾加 `system`，现与本地脚本一致（12 个）。
2. `deploy/community-test/deploy_encore.py`：`native_services` 加 `brokerscope,system`（`marketinstall` 仍按开关追加，未动）。
3. `docs/TODO.md`：把对应两条标为已修复。

## 验证
静态检查：
- actionlint 1.7.7（docker 镜像 `rhysd/actionlint`）对该工作流：无告警，退出码 0。
- Python 解析 YAML 成功；工作流与脚本的服务集合比较，相等（12 个）。
- `deploy_encore.py` 通过 `ast.parse`。**没有**实际运行该部署脚本。

真实构建：
- `scripts/build-encore-image.sh r8-encore:verify`（本机 arm64，约 22 秒，从干净导出构建，提交后的 HEAD）。
- 用安装器 `runtime.encore.json`（base64 放进 `ENCORE_RUNTIME_CONFIG`）起容器，映射 8080；日志 `registered 97 API endpoints`。
- `GET /api/system/info`、`/api/system/backups` 无令牌 → **401 unauthenticated**；不存在的 `/api/system/nonexistent-xyz`、`/api/nope` → **404**。证明 system 路由在镜像里。
- 容器 `r8-verify` 和镜像 `r8-encore:verify` 已删除，只删了自己建的。

## 没做 / 局限
- 这是本地 arm64 构建，用的是本地脚本；CI 工作流本身没有真的跑（不推送、不发布）。CI 用 amd64+arm64、Linux 上的 Encore 1.58.6 下载，未在本地复现。
- 容器没有接数据库，只验证了路由存在与鉴权拦截，不是整栈验收。
- 未对比“不带 system 的构建”作反例（401 对 404 的差异已足够区分）。

## git log --oneline base/origin-develop-20261113..HEAD
```
8e552681 ci: add system to Encore image --services; deploy_encore.py adds brokerscope, system (R8)
(本说明所在提交)
```
