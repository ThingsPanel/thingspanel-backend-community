# TODO

- [ ] 目录整理
- [ ] 数据转发

## PH1-S1 发现（2026-11-05）
- 后端：Broker 对平台命令发布返回 PUBACK 拒绝码（如 rc135）时，命令状态仍为 `broker_acknowledged`，应直接 `failed` 并带原因。
- 安装器：全新目录 `prepare.py --bootstrap` 因 `$` 校验失败（未插值模式下密码带 `${…}`）；`--verify` 硬编码 19091/503 端口。
- Encore：`encore build docker` 对 `encore-api/` 在本机 CPU 100% 无输出挂起（≥21 分钟），`encore check` 正常；前端设备页依赖 Encore `/api/v2/*`。
- 新建设备后需 `--sync-scope`，否则平台命令被 Broker 拒绝。

## HA 插件一键安装：冒烟之后再做（2026-11-09 负责人决定：初期只证明“能用”，这些先记账）

**进 main 之前要做**
- [ ] HA 连接器发布源码 `0b3240e`（分支 `wp/t6-ha-public-artifact-20261107`）合入连接器 `main`；`main` 现在仍是旧代码，和已发布制品对不上。
- [ ] 安装器 `docker-compose.yml` 里写死的 `v1.3.0`（前端、后端、两个 mosquitto）改成新版本，重新发布站点（冒烟阶段用环境变量/覆盖文件代替）。
- [ ] 确认公开目录的签名公钥（指纹 `6f6e902e…4fe8`）算不算正式信任根；要换就重签重发。
- [ ] 后端、前端、安装器、连接器各自合入 main 的方式和顺序；后端主目录里没做完的 cherry-pick（`91b1e958`）处理掉。
- [ ] 干净机器 amd64 + arm64 各完整验一次（冒烟只在一台 arm64 机器上）。

**产品问题（不影响冒烟）**
- [ ] 插件被 Broker 拒绝一阵后不会自己重连，要重启容器。
- [ ] Broker 范围同步后头 1–2 分钟内 HA 的变化没进平台（原因未查）。
- [ ] HA 地址或令牌填错时，插件“健康”仍显示 ok；缺“测试连接”按钮和明确的错误原因。
- [ ] 已发布的插件表单占位符里有内网地址 `http://192.168.31.97:8123`（`handler.go:58` 及表单），换成通用示例并重新构建、重新签名。
- [ ] HA 令牌仍明文存在 `service_access.voucher`（P2b 只有待办卡，没有设计）。
- [ ] 插件启动时不推送当前状态，刚接入的设备在变化前没数据（P5）。
- [ ] HA 实例自动发现没有实现（“自动”模式是保存后自动把所有实体建成设备，未测）；Docker 网络里 mDNS 一般过不去，更现实是示例/默认值提示。
- [ ] 租户菜单里的“设备接入 → 插件安装”页，租户是否会被正确拒绝，未验证。
- [ ] 资源中心地址页：不写 `http://` 被拒；已记住地址后 `_change` 直接跳走，没法更换。

**没测过**
- [ ] 安装后 30 分钟稳定、后端/Agent 失联后的恢复；amd64 实际运行；MySQL。

**文档**
- [ ] 教程 `plugin-release-installation.md`：3.2/3.3/6 节“只有 14 天产物/尚未发布”已过期，更新后合入文档站 `source`（线上现在没有这篇）。
- [ ] 入口流程图改正：下载、验签、`docker load` 发生在平台安装或升级时，点击“安装”只用本机已有的目录和镜像；补上租户配置一步。
- [ ] 连接器 README 里的旧本地运行说明（`cd examples/...`、`go run ./cmd/deploy-homeassistant-service`）。
- [ ] `CLAUDE.md` 证据路径仍写 80G 盘，实际用 `/Volumes/KIOXIA/archive/evidence/`。

**收尾**
- [ ] 测试账号、演示 HA 令牌用完轮换或删除；资源中心 Horizon 分支 `wp/t6-ha-resource-copy` 已部署但未合入 main。
- [ ] （冒烟中发现，2026-11-09）基础设施抖动会让安装进入 `failed`：验收栈的 Agent 容器 09:21 以退出码 135 退出且没有被重启（重启策略是 unless-stopped），11:21 Postgres 也异常重启（日志“没有正常关闭”）。后端随后续租连续 5 次失败，安装转 `failed`、身份吊销，要人工点“重试”。需要：Agent 自动拉起、续租失败的重试窗口是否太短（约 8 分钟）、验收栈的程序和数据放外置盘是否是诱因。
- [ ] 卸载确认框文字过期：写着“重新安装需再次填写令牌”，但现在安装不填令牌。
- [ ] （2026-11-09 清理时发现）验收栈 Postgres 当天第 3 次因 I/O 错误崩溃（PANIC，数据目录在外置 USB 盘上经 OrbStack 挂载）：负责人决定数据库和 Redis 继续放外置盘，由负责人解决外置盘稳定性（线索：`disksleep` 为 10 分钟，见清理盘点 I 节）；`docs/CLEANUP-INVENTORY-2026-11-09.md` 第 H 节有细节。
- [x] closeout 目录已归档到 `archive/secrets-hold/closeout-20261102/` 并删除（见清理盘点 I 节）。
- [x] KIOXIA 历史栈已合并成配方包并删除（见清理盘点 I 节）。
- [ ] 主仓库 `.git/objects/pack/tmp_pack_*` 有一个中断留下的垃圾文件，可 `git gc` 清理。
- [ ] 保留的 9 个 Docker 卷（`automation-rewrite_tp-auto-pgdata`、`tp-stage3-20261018_stage3_pg_data`、`thingspanel-install_*`、`codex-dm-native-read-*-pgdata`）：确认归属的工作线后再决定删不删。
- [ ] 仍保留的未完成工作线（不碰）：后端 `wp/d1-dbx-returning`、`wp/t1-a2-platform-dal-service`、`wp/t1b-ota-fix`、`wp/t7-ha-release-backport`、`wp/mvp-run-default-stack`；安装器 `wp/t6-ha-*`、`codex/ha-image-preload`、`feat/thingmodel-api`；前端 `wp/pui-frontend`、`wp-t7-plugin-list-urgent`、`wp-t8-ha-public-frontend`、`ha-r-deeplink`、主目录 `master`（38 个改动）；连接器 `t6-public-artifact`、`t6-public-release`。

