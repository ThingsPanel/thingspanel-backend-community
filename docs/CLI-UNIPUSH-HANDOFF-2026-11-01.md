# CLI 与 UniPush 合并交接

## 基线和范围

- Community `main` 基线：`7f694779`；CLI 和 UniPush 的提交位于 `codex/community-cli-completion-20261101`。
- 原生元数据命令以 Community 候选 `codex/metadata-menus-on-v1.3.0` 的 `d4e0540b` 已注册接口为准。该候选不在上述 `main` 基线里；单独合并 CLI 不会部署这些接口。
- CLI 原生命令使用服务端身份、显式幂等键、强 ETag、原生 JSON 响应及正式 HTTP 接口；不会回退到旧接口。已有复数设备命令仍使用当前主线的社区接口，单数 `device`、`template`、`group` 使用原生接口。
- CLI 覆盖设备、接入模板、分组、关联、凭据、调试、模拟器、读数导出、资产和连接器目录中已注册的主要操作；规划但未注册的操作没有冒充完成。更详细的命令见 `cli/tpctl-go/README.md` 和 `tpctl help <命令组>`。
- UniPush 是独立通知插件，使用 Encore 通知插件协议；源码和构建文件已迁入 `notification-adapters`，不另建通知核心。现有 App 云函数和部署配置需独立确认，详见 `notification-adapters/UNIPUSH-MIGRATION.md`。

## 验证与剩余工作

| 项目 | 本次结果 |
|---|---|
| Luna 独立测试 | `tpctl-go` 50 项、`tpctl-mcp` 18 项通过；两个模块 `go vet` 和构建通过。测试未执行真实设备命令。 |
| 原生 CLI 对真实候选 API | **未完成**。隔离候选两次 Encore 镜像构建停在依赖下载；离线重试仍缺少其他 Go 依赖，并遇到 PostgreSQL 镜像拉取阻断。未生成镜像或发出 CRUD 请求。日志在本机 `/tmp/tpctl-native-e2e-20261101/build-attempt.log`，不可作为通过证据。 |
| UniPush 联调 | 真实 notification PostgreSQL store、worker 与真实插件进程通过一次联调，目录与 UniCloud 使用本机替身；完整 Encore API 运行器、真实 App、厂商离线通道和 iOS APNs 未验证。 |
| 镜像与生产 | 新增独立模块的 CI 检查；没有发布镜像或部署。历史记录不能替代当前环境验证。 |

合并前应在可用的隔离构建环境固定候选版本，运行真实原生 CLI 的设备、模板、分组 CRUD、ETag、幂等、跨租户和异常用例；核对上传/下载；再与实际 Encore 通知、UniCloud 配置及测试手机验证推送。只有候选后端和 CLI 一起部署后，原生命令才能使用。仓库中旧 `tpctl` 可执行文件不代表本次源码，请重新构建发布制品。

## 本机环境变化

为尝试联调启动了 OrbStack，其自动恢复了此前已有的容器；本次没有写入或停止这些容器。测试创建的独立 PostgreSQL 已停止并删除数据。失败的 `encore run` 结束后，`127.0.0.1:15002` 仍由原有全局 Encore daemon 持有且无 HTTP 响应；为保护其他工作，没有重启该 daemon。
