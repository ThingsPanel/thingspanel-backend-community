# R4S 交付说明：dump/restore 之后后端可启动

## 结论
`pg_dump -Fc` → `pg_restore` 之后后端不再因 schema 校验拒绝启动。根因只有一处：`verifyNativeConstraint` 比较的 `pg_get_constraintdef` 文本，在还原后对 `varchar` IN 列表的渲染不同。修复在 `initialize/native_binding_schema.go`（见下），提交号见 `git log wp/R4S-schema-contract`（最新一条 `fix(initialize): ...R4S`）。未推送、未合并。

## 审计结果（所有读取目录文本的校验）
| 校验 | 读取内容 | 还原后是否变形 | 处理 |
|---|---|---|---|
| `verifyNativeConstraint`（native_binding / device_identity / history_intent / service_access / market_install 共用） | `pg_get_constraintdef` | 仅 `device_id_reservations_state_check`（`state IN (...)` 的 varchar 数组）变形：`ANY ((ARRAY['a'::character varying,..])::text[])` → `ANY (ARRAY[('a'::character varying)::text,..])`。其余被校验的 CHECK/UNIQUE/PK 文本前后一致（真实库实测） | 规范化 |
| `device_identity_schema.go` 触发器 | `pg_get_triggerdef`、`prosrc`、`proconfig` | 一致 | 无需改 |
| `platformconfig_schema.go` | `prosrc`、`proconfig` | 一致（还原库上实测通过，含 `TP_PLATFORMCONFIG_OWNER=encore`） | 无需改 |
| `auth_schema.go` | `pg_get_constraintdef = 'UNIQUE (issuer, subject)'` | 一致 | 无需改 |
| `history_intent`/`service_access`/`market_install` 的 `character varying` 默认值 | `column_default` | 一致 | 无需改 |

任务书里说的“约 16 个 CHECK”是还原库里 `varchar` IN 列表约束的总数（6 个含 `(...::character varying)::text` 渲染）；Go 侧真正按文本比较的只有 `device_id_reservations_state_check` 一个，其余（broker_*、notification_source_* 等）后端不比较，所以不受影响。

## 改动
- `initialize/native_binding_schema.go`：`normalizeSchemaExpression` 先调用 `canonicalizeRestoredArrayAny`。它只改写“每个元素都是 `('字面量'::character varying)::text`”的还原形态，改写成新装形态 `ANY ((ARRAY['a'::character varying, ...])::text[])`；字面量原样保留。其他任何文本原样返回。因此：值多一个/少一个/不同、顺序不同、列不同、`ALL`/`<>` 运算符、混合元素、`NOT VALID`、类型/验证状态不符，仍然被拒绝。固定期望字符串未改。
- `initialize/native_binding_schema_test.go`：单元测试（两种渲染等价、带引号与逗号的字面量、新装形态不被改动、8 个反例）。
- `initialize/schema_restore_postgres_test.go`：可选真库测试 `TestPlatformSchemaStartupOnPostgres`，用 `TP_R4S_TEST_DSN` 跑后端真实的 `initializePlatformSchema`（`CheckVersion` + 全部组件初始化与校验）；仅允许回环地址且库名以 `r4s_` 开头，否则跳过/拒绝。
- 未使用 `pkg/sqldialect`、`dbx`；没有运行时改写 SQL，只是对比用的文本规范化。

## 验证
单元（不连数据库）：
```bash
go test ./initialize -count=1        # ok
go test ./initialize -run 'NormalizeSchema' -v
```
真库往返（PG 14.5 + TimescaleDB，镜像 `registry.cn-hangzhou.aliyuncs.com/thingspanel/timescaledb:14`，容器 `r4s-pg`/网络 `r4s-net`，已删除）：
1. 空库 `r4s_src` 跑 `TestPlatformSchemaStartupOnPostgres`：通过（新装，全部迁移+校验）。
2. `pg_dump -Fc`；新建 `r4s_dst`；`timescaledb_pre_restore()`；`pg_restore --no-owner`；`timescaledb_post_restore()`。还原后该约束文本确为 `ANY (ARRAY[('active'::character varying)::text, ...])`。
3. 未修复代码（临时关掉规范化）跑还原库：**失败**，报错与现场一致（`device_id_reservations_state_check does not match the native binding contract`）。
4. 修复后跑还原库：**通过**；新装库再跑一次也通过。
5. 在还原库上把该约束改成 `('active','tombstoned','extra')`：仍被拒绝；改成正确值但 `NOT VALID`：仍被拒绝。
证据：`/Volumes/KIOXIA/archive/evidence/2026-11-12-R4S/restored-without-fix.txt`、`restored-with-fix.txt`。

## 没覆盖 / 没做
- 没有重放完整的 3M 行数据演练、也没有跑 `r3-postgres-1/r4scratch`（只读取过它的约束与触发器文本；`r4scratch` 里没有 platformconfig 函数，因此该函数在我自建的还原库上验证）。
- 只测了 PG 14.5。更高版本的 deparse（PG 16）可能在其他表达式上也有差异，规范化只覆盖已观察到的 varchar 数组形态。
- 这是真库单次往返，不是端到端验收（后端镜像重建后的整套还原演练由父会话进行）。
- MySQL 校验路径未审计（不涉及 pg_dump）。
