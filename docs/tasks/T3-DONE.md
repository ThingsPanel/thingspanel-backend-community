# T3 完成说明（暂停于 A-1）

分支：`wp/t3-tdengine`  基线：`develop@c0ab14d1`  当前提交：见本文件提交

## 改了什么

- `docs/WORKSPACES.md`：登记 TDengine 验收 worktree、隔离测试栈和证据目录。
- 本次没有改 TDengine 后端实现；A-2 必须等 A-1 三项验收完成后才开始。

## 验证

- `docker build -t thingspanel-backend:dbp1-c0ab14d1 .`（干净 develop）：通过。
- 在独立 `dbp1-tdengine` Compose 项目运行 `prepare.py --bootstrap`：迁移完成；随后全栈启动，后端、PG、Redis、TDengine 均 healthy，后端配置为 `history_backend=tdengine`、`experimental=true`。
- 首次发布发生在 backend MQTT subscriber 重连之前；随后按正确顺序重发 3 条 QoS 1 消息，PG 当前值、9 条 history intent 和 TDengine 9 条历史记录均出现。
- 设备详情读到 humidity=47.9、switch=0、temperature=24.6。打开湿度历史页后，三条数据 47.9/48.2/48.5、折线和统计值（平均 48.20、最大 48.5、最小 47.9）均可见；统计 API 返回 HTTP 200。
- “导出数据”已点击且页面未报错，但没有观察到新文件或下载完成提示；受浏览器策略限制，无法检查浏览器内部下载记录。**A-1 尚未通过，不开始 A-2。**
- 设备状态仍显示离线（online status 0），尽管遥测和历史页可见。另有 `/api/v1/plugin/heartbeat` 503，未追查。

## 没做 / 发现的问题

- 等待导出文件结果后再判定 A-1 是否通过；成功后只按 dispatch 修改 `internal/app/history_backend.go`、`internal/app/storage.go`、`internal/app/history_backend_test.go` 和新建 `docs/DATABASE-SWITCH.md`。
- B-2 仍需等 T1-A2 经负责人审查合入 develop 后再开始。
- 隔离测试栈和数据卷仍保留，供负责人检查；没有删除容器或卷。测试配置保存在 `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack/.env`，权限为 600，不属于证据目录。

证据：`/Volumes/KIOXIA/archive/evidence/2026-11-09-T3-tdengine/A1-observation.txt`、`backend-relevant.log`。历史/统计截图未写入证据目录。
