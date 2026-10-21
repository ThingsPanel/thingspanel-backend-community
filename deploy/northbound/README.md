# 北向服务（数据转发）单机编排

这个目录只做一件事：把北向服务需要的那三个进程，加到已经跑起来的社区部署上。
它**不创建** PostgreSQL、Redis、MQTT Broker 或社区后端——那些是社区部署的资产。

```
deploy/northbound/
├── compose.yaml                 三个服务的编排（核心 + HTTP 插件 + MQTT 插件）
├── Dockerfile.plugin            两个插件共用的镜像定义（靠 PLUGIN 参数选目录）
├── northbound.env.example       环境变量模板，每项都注明对应哪条配置
├── migrate.sh                   社区库迁移 + 验证（重复执行安全）
└── README.md                    本文件
```

## 为什么需要 migrate.sh

`sql/northbound/001_source_outbox.sql` 和动态菜单种子在子目录里，而社区后端的自动迁移
（`initialize/pg_init.go`）只按顺序扫 `sql/<数字>.sql`，**扫不到子目录**。

漏执行它的后果不是报错，而是**静默失败**：数据照常入库，但一条都不会转发，
页面上「等待投递」永远是 0。这种错只能靠脚本堵住，不能靠人记得。

```bash
COMMUNITY_DSN='postgres://user:pass@host:5432/db?sslmode=disable' ./migrate.sh
```

脚本会应用 broker 上下文表、源端 Outbox 和「数据转发」动态菜单/租户权限，随后验证表和菜单记录；缺少时直接非零退出。

## 起停

```bash
cp northbound.env.example northbound.env
$EDITOR northbound.env            # 至少填三个令牌、密钥、镜像 tag、网络名

docker compose --env-file northbound.env up -d
docker compose --env-file northbound.env ps
docker compose --env-file northbound.env logs -f northbound-sink-http
docker compose --env-file northbound.env down        # 只停容器，不删数据
```

`northbound.env` 已被 `.gitignore` 忽略。不要把真实令牌提交进仓库。

## 两个容易踩的坑

**1. 插件必须传 `--host 0.0.0.0`。** 插件默认只监听 `127.0.0.1`（裸进程安装时
这是对的）。但 Docker 的端口发布转发到容器的 eth0，监听容器自己的回环从宿主机
永远连不上。表现极具欺骗性：`docker ps` 显示 Up，日志也说 `listening on
127.0.0.1:18081`，但 `curl` 就是连不通。compose 里已经写好了这个参数。

**2. 核心镜像不是这个目录构建的。** Encore 应用有自己的构建方式：

```bash
cd ../../encore-api
encore build docker <registry>/northbound-core:<tag> --arch amd64 --config <infra 配置>
```

不要用 `--skip-config` 生成生产镜像——那样镜像里没有数据库连接信息。

## 哪些是实测过的

- ✔ 插件镜像：`docker build` 真实构建成功，容器内启动后无令牌 401、带令牌正确
  返回 `describe`，以非 root（65532）运行。
- ✔ `compose.yaml`：`docker compose config` 校验通过。
- ✘ 整栈 `up` 联调：**没跑过**。验收用的是裸进程（见
  `docs/northbound/DEPLOYMENT.md` §3），端口与变量与此处一致。
- ✘ Encore 核心镜像：`encore build docker` 本机跑了 17 分钟未完成，已中止。

完整的能力边界见 `docs/northbound/DEPLOYMENT.md` §8。
