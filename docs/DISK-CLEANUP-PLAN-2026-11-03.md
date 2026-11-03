# 磁盘清理计划（2026-11-03）

**原则：先量、后删、每一步删之前向用户确认；源码、证据、参考安装的数据不动。本计划没有删除任何用户文件。**
（唯一已执行的删除：我自己这次会话临时目录里的 Go 构建缓存和一个二进制，共 3.6 GB，可随时重建。）

## 1. 实测数字（只读命令得到）
| 位置 | 数字 | 说明 |
|---|---|---|
| 系统 APFS 容器（含 Docker 磁盘镜像） | 96.3% 已用，清理前约 9 GB 可用，删掉我的缓存后约 12 GB | **真正紧张的是这里** |
| `/Volumes/80G` | 39 GB 可用（48.8%） | Go 缓存 `GOCACHE=/Volumes/80G/.gocache` 在这里 |
| `/Volumes/500G` | 471 GB 可用（94.3%） | 当前模块缓存在这里；**先做只读校验再依赖**：`diskutil verifyVolume /Volumes/500G`（之前记录过它报告 APFS 错误） |
| Docker | 镜像 10.6 GB（全部在用）；本地卷 204 个、20.3 GB，**其中约 19.3 GB 未被任何容器使用**；构建缓存 0 | 数据在系统容器里 |

## 2. 为什么 Docker 卷这么多（已查到原因）
- 127 个未被使用的**匿名卷**（名字是 64 位十六进制），单个约 230 MB：都是 `mysql:8.4` / `postgres` 之类的临时测试容器用 `docker rm -f`（不带 `-v`）删除后遗留的。我这次会话也制造了一部分；`scripts/dev-databases.sh` 已改成 `docker rm -fv`。
- 其余是命名卷：Encore 测试库 `sqldb-*`、各次接管验证的 `tp-*`（takeover/closeout/cb-v130/stage3/b-vis…）、安装器测试 `thingspanel-install_*`、参考安装 `thingspanel-community-v130_postgres_data`（292 MB）、`tp-closeout-community-go-*`（1.28 GB，Go 缓存卷）。

## 3. 分级与建议顺序（收益大、风险小的在前）
| 步骤 | 对象 | 判断 | 命令（先看，后删） | 预期释放 |
|---|---|---|---|---|
| S1 | 未使用的**匿名**卷（64 位十六进制名） | 没有容器引用，内容是被删容器的残留，**可删** | 先看数量：`docker volume ls -qf dangling=true \| grep -E '^[0-9a-f]{64}$' \| wc -l`；确认后：`docker volume prune -f`（默认只删匿名卷） | 约 15–29 GB（127 个 × 约 230 MB） |
| S2 | 未使用的 Encore 测试卷 `sqldb-*` | `encore test` 会重建，**可删** | `docker volume ls -qf dangling=true \| grep '^sqldb-'` 看清单，确认后逐个或 xargs `docker volume rm` | 数百 MB 到数 GB |
| S3 | `tp-closeout-community-go-*` 等 Go 缓存卷 | 可重建；删之前确认没有收尾流程要复用它 | `docker volume inspect` 看用途 | 1.3 GB |
| S4 | 接管验证的命名卷 `tp-takeover-*`、`tp-b-*`、`tp-cb-v130-isolated`、`tp-stage3` | **验收证据可能引用**。先归档再删：`docker run --rm -v <卷>:/d -v /Volumes/80G/archive:/out alpine tar czf /out/<卷>.tgz -C /d .` | 视大小，约 1–3 GB |
| S5 | `/private/var/folders/t9/…/T/tp-takeover-build-c80hmwza`（37 份 Go 模块快照，系统卷上） | **证据，先保留**。要腾空间就**搬不删**：`rsync -a` 到 `/Volumes/80G/archive/`，校验后在原路径留符号链接（验收 JSON 引用的路径仍然有效） | `du -sk` 单独量，别扫整个 `/private` | 未量（扫描超时过），预计是系统卷上最大的项 |
| S6 | 旧 Go 缓存：`/Users/junhong/Downloads/code/.cache/{go-build,go-mod}`（8 月）、`thingspanel-backend-community/.cache/go-mod`（10 月 9 日） | 当前 `go env` 不指向它们，**候选**。删之前：`grep -rn "\.cache" Makefile scripts` 确认无引用；`git check-ignore` 确认被忽略 | `du -sk <目录>` 逐个量 | 未量 |
| S7 | 项目自管缓存：`thingspanel-adapter-xiaomi/.cache/gomod`（Makefile 引用）；`thingspanel-closeout-work-20261102/gomodcache`（旧收尾目录） | Xiaomi 的保留；收尾目录里的先核对证据是否引用 | 同上 | 未量 |
| 保留 | `thingspanel-community-v130_postgres_data`（参考安装库）、`thingspanel-install_*`（安装器测试数据，先问负责人）、所有源码仓库与 worktree、`/Volumes/80G/tp-develop-integration/`（18080 栈的 fixture） | 不动 | | |

## 4. 注意
- 删 Docker 卷释放的是 Docker 磁盘镜像里的空间；macOS 的可用空间要等镜像压缩后才增加（Docker Desktop 设置里的磁盘镜像回收/Purge，或对 Docker VM 做 `fstrim`）。做完 S1 后用 `df -h /` 和 `docker system df` 复查，不要假设已经释放。
- 不要对整个 `/private`、`~/Downloads/code` 跑 `du`（小文件太多会超时）；对具体目录用 `du -sk`，放后台并把输出写文件。
- 之前的盘点说 500G 卷曾报告 APFS 错误：把证据**搬到 80G**（39 GB 可用）更稳；500G 先 `diskutil verifyVolume` 再决定。
- 清理由包 E2 负责，建议单独一个人做，别和开发包并行改同一目录。
