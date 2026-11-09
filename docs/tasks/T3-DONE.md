# T3 完成说明（暂停于 A-1）

分支：`wp/t3-tdengine`  基线：`develop@c0ab14d1`  当前提交：见本文件提交

## 改了什么

- `docs/WORKSPACES.md`：登记 TDengine 验收 worktree、隔离测试栈和证据目录。
- 后端代码未改；A-2 依赖 A-1 验收通过，本次未开始。

## 验证

- `docker build -t thingspanel-backend:dbp1-c0ab14d1 .`（干净 develop）：通过。
- 在独立 `dbp1-tdengine` Compose 项目运行 `prepare.py --bootstrap`：迁移完成；随后全栈启动，后端、PG、Redis、TDengine 均 healthy，后端配置为 `history_backend=tdengine`、`experimental=true`。
- 前端初始化后创建验收租户及设备；`prepare.py --refresh-devices` 刷新 1 台设备范围，并按顺序重启 broker-owner、mosquitto。
- 用设备 MQTT 凭证向 `devices/telemetry` 发布 3 条 QoS 1 消息，`mosquitto_pub` 均退出 0；但设备状态仍为离线，PG 的 `telemetry_current_datas` 与 `telemetry_datas` 对应设备行数均为 0，设备详情无数据。因此 A-1 未通过，历史页、导出、统计未验收。**不能据此判断 TDengine 历史功能通过。**
- 观察期间 backend health 正常；日志中另有 `/api/v1/plugin/heartbeat` 返回 503。此项未追查。

证据：`/Volumes/KIOXIA/archive/evidence/2026-11-09-T3-tdengine/A1-observation.txt`、`backend-relevant.log`。本次未保存含凭证的页面截图。

## 没做 / 发现的问题

- 按派工要求，发现数据链路异常后停止，不继续追查、不改范围外代码，也未去掉 TDengine 实验开关。
- A-1 完成前不做 A-2；B-2 仍需等 T1-A2 经负责人审查合入 develop 后再开始。
- 隔离测试栈和数据卷仍保留，供负责人检查；没有删除容器或卷。测试配置保存在 `/Volumes/KIOXIA/tmp/dbp1-tdengine-stack/.env`，权限为 600，不属于证据目录。
