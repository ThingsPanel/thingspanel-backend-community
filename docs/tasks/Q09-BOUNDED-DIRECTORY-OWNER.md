# 自动化设备精确查询 owner 增量

基线 9766c010 为现场 Gin1702 精确组合。新增 POST /internal/automation/devices/lookup，复用现有 automation 服务鉴权、部署标识及可信租户上下文。请求仅 {"ids":[...]}；原始数组 1 至 200 项、单 ID 1 至 36 字节、拒绝空白/控制字符、未知字段、尾随 JSON 和超大请求。重复 ID 去重。

SQL 同时限定 tenant、enabled、所请求 ID；模板 join 同样限定 tenant。一次精确查询，不做总租户 count，不分页或扫完整设备列表。返回数不能超过去重后 ID 数；不存在、停用和外租户 ID 都不会返回。旧 GET /devices 契约保留，旧 Encore 可继续在线；依赖该新接口的 Encore 必须后上线，404 无全量 fallback。

本地验证三项全部通过，0 跳过：请求边界与服务鉴权；真实 PostgreSQL 独立 schema 内含 1500 个无关设备，验证 A/B 租户、停用、缺失、重复 ID、跨租户模板关联、合法 200 项批量；原 owner 鉴权回归。测试结束只清理自建 schema。证据 F:/coding/artifacts/20261111-coordinated-rollout/Q09-directory-gin-tests.jsonl。

未部署、未改远程配置或任何共享业务记录。该 owner endpoint 不依赖新 Encore，也未变更数据上报/命令行为。
