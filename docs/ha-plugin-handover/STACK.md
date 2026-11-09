# 验收栈运行说明（2026-11-09 整理）

栈目录：`/Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106/`（不在仓库里，**不要删**）。Compose 项目名 `tp-ha-ui-acceptance-20261106`，网络 `tp-codex-ha-ui-net-20261106`。

## 现在跑的是什么

| 部分 | 镜像 / 来源 |
|---|---|
| 后端 | `thingspanel-backend-ha-ui:develop-c0ab14d1`（本地 develop `c0ab14d1` 构建，ID `sha256:018b6b5d94f7…`） |
| Broker（broker-owner + mosquitto） | `thingspanel-mosquitto:ph1-520a3222`（同 ID `b459e470fd6c` 的 `t7b-local`，develop 线构建，T7b 用同一个） |
| 前端 | `thingspanel-vue:v1.3.0` 的 nginx + 挂载 `web/dist`（由 `wp/pui-frontend` @ `c40ad217` 构建，含插件安装页和租户入口） |
| Agent | `tp-agent-tp-ha-ui-acceptance-20261106`，状态目录 `agent2/`，用公开 1.0.0 目录；后端经 `agent-proxy` 访问它 |
| HA 插件 | 由 Agent 起，公开 1.0.0 arm64 镜像，网络别名 `tp-plugin-homeassistant` |
| 演示 HA | 容器 `ha-demo`，在同一网络，地址 `http://homeassistant-demo:8123`（从浏览器访问 `http://127.0.0.1:58123`） |
| Broker 范围监视 | 主机后台进程 `scope_sync.py watch`（`ps aux \| grep scope_sync`） |

## 访问

- 页面 `http://127.0.0.1:58800`（局域网：`http://<这台 Mac 的 IP>:58800`）；后端 `http://127.0.0.1:58999`。
- 账号：平台超管（负责人提供的本机测试账号）；租户账号 `ha-tenant-a@example.invalid`、`ha-tenant-b@example.invalid`，密码在 `/Volumes/KIOXIA/archive/evidence/2026-11-09-HA-HANDOVER/platform-test-accounts.json`（0600）。演示 HA 的账号和长期令牌在同目录 `ha-demo.cred.json`（0600，令牌 7 天后过期）。**这些只用于这台机器上的测试栈，别抄进文档或聊天。**
- 租户 A 下已有接入点 `ha-demo-tenant-a`（指向演示 HA），3 台设备：Bed Light、Outside Temperature、Outside Humidity。租户 B 是空的，用于隔离测试。

## 常用命令

```bash
cd /Volumes/KIOXIA/CodexTests/thingspanel-ha-ui-acceptance-20261106
F=(-p tp-ha-ui-acceptance-20261106 -f docker-compose.yml -f docker-compose.override.yml -f docker-compose.proxy.yml)
docker compose "${F[@]}" ps                                    # 看状态
docker compose "${F[@]}" up -d --no-deps --no-build backend    # 只重建后端（改镜像 tag 先改 override）
python3 config/native/prepare.py --sync-scope                  # 手工同步一次 Broker 设备范围
```

zsh 里必须用数组 `F=( … )` 再 `"${F[@]}"`，直接用字符串变量会报 “invalid project name”。

## 改动记录（可回滚）

- `docker-compose.override.yml.bak-20261109`：改后端镜像前的版本；`.bak2-20261109`：改 Broker 镜像前的版本。旧镜像 `thingspanel-backend-ha-ui:20261106` 和 `registry…/thingspanel-mosquitto:v1.3.0` 都还在。
