# T3 完成说明（A-2 完成，A-1 导出待确认）

分支：`wp/t3-tdengine`  基线：`develop@c0ab14d1`  A-2 实现提交：`b76e3a38`

## 改了什么

- `internal/app/history_backend.go`：移除 TDengine 实验开关字段和拒绝逻辑，保留配置校验、启动前建表及 fail-closed 行为。
- `internal/app/storage.go`：不再读取 `storage.tdengine.experimental`。
- `internal/app/history_backend_test.go`：增加不设实验开关也可选择 TDengine 的测试；原有坏配置拒绝测试保留。
- `docs/DATABASE-SWITCH.md`：说明时序库切换配置。
- `docs/TODO.md`：记录安装器遗留环境变量可后续清理。
- `docs/WORKSPACES.md`：登记 TDengine 验收 worktree、隔离测试栈和证据目录。

## 验证

- `docker build -t thingspanel-backend:dbp1-c0ab14d1 .`（干净 develop）：通过。
- 在独立 `dbp1-tdengine` Compose 项目运行 `prepare.py --bootstrap`：迁移完成；随后全栈启动，后端、PG、Redis、TDengine 均 healthy，后端配置为 `history_backend=tdengine`、`experimental=true`。
- 首次发布发生在 backend MQTT subscriber 重连之前；随后按正确顺序重发 3 条 QoS 1 消息，PG 当前值、9 条 history intent 和 TDengine 9 条历史记录均出现。
- 设备详情读到 humidity=47.9、switch=0、temperature=24.6。打开湿度历史页后，三条数据 47.9/48.2/48.5、折线和统计值（平均 48.20、最大 48.5、最小 47.9）均可见；统计 API 返回 HTTP 200。
- 用户明确要求记录导出问题并继续后续任务；“导出数据”已点击且页面未报错，但没有观察到新文件或下载完成提示。用户回到电脑后再确认导出文件；本次按用户指示不把它作为 A-2 的阻塞项。
- 设备状态仍显示离线（online status 0），尽管遥测和历史页可见。另有 `/api/v1/plugin/heartbeat` 503，未追查。
- 使用测试专用 `dbp1-tdengine` 容器（TDengine 3.3.6.0，REST 56041）：`TP_TDENGINE_TEST_URL=http://127.0.0.1:56041 go test -count=1 -v ./internal/app ./internal/storage -run 'TDengine|Tdengine'`：5 PASS、3 SKIP、0 FAIL。3 个 SKIP 均需要 `TP_TELEMETRY_BROKER_TEST_DSN`（其中一个同时要求 `TP_TDENGINE_TEST_URL`）。
- `go test -count=1 ./internal/app/...`：通过。
- TDengine 的无开关选择测试通过；真实 TDengine 集成测试在未配置实验字段的 `historyBackendSettings` 上选择并执行了 schema prepare。
- 用 A2 候选镜像启动独立后端实例，确认容器环境中完全没有 `GOTP_STORAGE_TDENGINE_EXPERIMENTAL`，连接 PG、Redis、TDengine 并返回 `/health` HTTP 200。第一次使用独立 client ID 接原 Mosquitto 时被 ACL 拒绝；随后改连独立无认证测试 broker 后启动成功，原 A1 栈未中断。
- `git diff --check`：通过。

## 没做 / 发现的问题

- A-1 的导出文件尚未目视确认；待用户回到电脑后核实，并把非敏感截图补入证据目录。
- B-2 仍需等 T1-A2 经负责人审查合入 develop 后再开始。
- A-2 不依赖安装器环境变量；`GOTP_STORAGE_TDENGINE_EXPERIMENTAL` 仍由安装器 compose 传入但后端已忽略。
- 隔离验收栈、TDengine 测试容器和数据卷均保留，供负责人检查。独立启动探针容器已停止并移除；为测试 broker 自动创建的匿名卷 `b61b4bdc2d16`、`c2e1008577a1` 仍保留，未删除。测试配置保存在 `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack/.env`，权限为 600，不属于证据目录。

证据：`/Volumes/KIOXIA/archive/evidence/2026-11-09-T3-tdengine/A1-observation.txt`、`backend-relevant.log`、`tdengine-targeted-tests.log`、`tdengine-noexp-startup.txt`。历史/统计截图未写入证据目录。
