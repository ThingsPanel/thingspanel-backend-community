# Community trusted telemetry evidence API

日期：2026-10-20。此实现仅服务于 `ingress_v1` 已处理可信遥测回执。它复用 broker ingress receipt，不新建历史库，也不把 legacy `telemetry_datas` 记录包装成可信数据。

## API 与证据语义

新增 `GET /api/v1/device/telemetry/evidence/:id?start=RFC3339&end=RFC3339&keys=a,b&limit=N`。请求需当前有效的人类用户 `x-token`；身份、JWT、Redis session、用户启用状态及 tenant 由现有 `ValidateBrokerSubject` 路径复核。设备必须属于当前 tenant 且 enabled/active，并需能解析当前已发布 semantic 描述。当前语义/status 只请求一次，状态通过现有只读 `InspectCurrent` 读取。

查询只读取以下回执：tenant 相同、`stream=telemetry_sample`、`state=processed`、`metadata.source=trusted_telemetry_batch`，并且 `metadata.logical_device_id` 精确等于路径设备 ID。`receipt.device_id` 保存 MQTT transport/root 设备，所以仅按 root ID 查询会把多个子设备数据混在一起；本 API 明确按可信写入器保存的逻辑设备 metadata 过滤。回执自身的 `broker_cell` 和 `route_epoch` 留作历史来源信息，不与当前 binding epoch 比较，因此路由迁移前已经提交的历史点仍可读。

每条结果返回 `reported_at: null` 与 `reported_time_available: false`：当前 trusted writer 把 `Message.Timestamp` 直接设为 broker `observedAt`，没有保留独立设备上报时间；`timestamp_basis=broker_observed` 明确这一限制。`observed_at` 是 receipt `created_at`，`processed_at` 是可信批次成功事务中的处理时间。结果同时带 receipt ID、`quality=trusted_processed`、`source=trusted_telemetry_batch`，以及 transport device、cell 和 route epoch，作为可信记录中的历史来源字段；不会按当前父子关系重新解释历史记录。**没有 `received_at` 字段**：现表没有真实后端到达时间。`window_basis=observed_at` 表示 start/end 是以回执 `created_at` 过滤。start/end 必填、end 独占，区间最多 24 小时；最多 8 个唯一 key，limit 默认 100、最大 200。SQL 使用 2 秒 statement timeout，整个 service 调用最长 3 秒。缺失点位返回空 `items`，不补零。

只支持发布模型中 `kind=TELEMETRY`、read 与 observe 均为 true，且 scalar 类型为 INT/FLOAT/NUMBER/BOOL/STRING 的字段。类型不符的已存点会使请求失败关闭。仅当 semantic 明确提供非空 unit 时 `unit_known=true`；缺失/空 unit 返回 `unit_known=false`，不猜测 dimensionless。ATTRIBUTE、EVENT、COMMAND、不可读/不可观测或当前不支持的类型返回 `unsupported_capability`。min/max 等聚合没有实现，不会声称完成聚合。legacy 记录没有 trusted receipt，不会出现在结果中。

迁移 014 建立 partial expression index `(tenant_id, metadata->>'logical_device_id', created_at, id)`，仅覆盖处理完成的 trusted telemetry receipts。service 同时在 SQL 中按 tenant、来源、逻辑设备和观察时间限定，最多扫描 201 个 receipt、最多读取 16 MiB payload、最多返回 200 个点；达到任一上限会标记结果 `truncated`。`check-broker-schema.sh` 的只读清单现在会检查该命名索引；脚本仍不核对索引定义，也不会自动迁移生产。

## Encore schema 核对

只读检查的 Encore checkout commit 为 `37fbb5e`，路径 `/Users/junhong/Downloads/code/thingmodel-workspaces/dev-release-20261018/kernel-worktree`。`encore-api/thingmodel/core/types.go` 定义 `FieldKind` 的 `TELEMETRY`、`ATTRIBUTE`、`EVENT`、`COMMAND`，`Field` 携带 `Identifier`、`Unit`、`Access` 和 `ValueType`；`ScalarKind` 定义 INT/FLOAT/NUMBER/BOOL/STRING/ENUM。`encore-api/thingmodel/transport.go` 的 API 版本化 `ModelField` 使用 JSON `kind`、`identifier`、`unit`、`access`、`valueType` 字段，`ValueType` 使用 `kind`/`baseKind`。解析器只消费已发布 snapshot 的这组字段，不依据名称、样例值或社区侧旧模型猜类型与单位。

## 隔离验证

真实 QA PostgreSQL 测试使用 `/tmp/tp-mqtt-20261019/qa-dsn`（DSN 未输出），在独立随机 schema 中创建隔离表。`go test -race -v ./internal/api -run 'TestBrokerTelemetryEvidenceTrustedReceiptsAndSemanticPins|TestTelemetryEvidenceQueryRejectsRepeatedParameters'` 在显式设置 `TEST_BROKER_DATABASE_DSN` 后两项通过，无 skip。测试覆盖子设备 logical ID 过滤、跨 tenant 回执排除、窗口 start/end 边界、旧 route epoch 历史保留、pending/legacy/其他子设备过滤、missing metric 不补零、Encore semantic pin/kind/unit、未授权与越界查询、只读不改 receipt/activity/legacy `is_online`。HTTP semantic server 是 httptest stub，按 Encore 契约返回 fixtures；没有访问 live Encore 服务。

`go test -race -v ./internal/service -run 'TestTelemetryEvidenceRequestBounds|TestTelemetryMetricDefinitionUsesPublishedEncoreShape|TestTelemetryMetricDefinitionFailsClosed|TestTelemetryValuesMatchPublishedScalarKind'` 四组 service 单测通过。索引测试在 PostgreSQL `EXPLAIN` 中强制关闭 seqscan 后确认使用 migration 014 的表达式索引。

QA migration checker 对随机 `mqtt_qa_*` schema 完成 001–014 fresh apply、同 schema repeat apply、read-only 校验；三次最终 schema 检查通过。随后只在该临时 schema 删除 014 索引，read-only checker 以 exit 2 明确列出缺少索引，再由 trap 删除该 schema。没有修改 `public` 或生产数据库。完整包 `go test -race -v ./internal/api ./internal/service ./router/apps` 也通过；API 包中若干与 notification 相关的测试因未提供 `NOTIFICATION_TEST_DSN` 而按测试约定 skip，与本功能测试无关。

当前证据证明隔离 PG + 契约 stub 的查询路径，不证明 live Encore 连通、真实生产模型字段覆盖、历史数据完整性、设备物理状态或生产 schema 已安装 014。发布前仍须由受控数据库迁移流程部署并检查 014。
