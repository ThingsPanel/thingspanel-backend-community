# T01：字典唯一业务 owner 与现有库接管合同 v1

状态：可审查候选，等待固定版本的真实 Encore HTTP、发布制品和共同切流验收。本文不是生产切流授权，也不把 PostgreSQL 模块测试扩展为 J-G0～J-G4 通过。总负责人维护原任务和共同门禁总账。

## 1. 归属与完整入口

`encore-api/platformconfig/app` 是原生 owner 的字典业务用例；`platformconfig/store` 是当前 PostgreSQL 实现；`platformconfig/service.go` 是真实 Encore `auth raw` host。它们均在现有 `encore-api/go.mod` 下，不新建 Go module，不导入旧 Gin、ServiceGroup、global.DB 或 GORM。调用方只提交原输入，subject、tenant 和 role 来自现有 `authn.CurrentData()`。

| 原路由 ID / 小卡 | 保留的 URL | Native handler / use case | 当前权限资源 | 附加业务权限 |
|---|---|---|---|---|
| G255 / A01-E10 | POST `/api/v1/dict/column` | `Service.CreateColumn` / `CreateDictionary` | `api/v1/dict/column` | 当前 SYS_ADMIN |
| G256 / A01-E10 | POST `/api/v1/dict/language` | `Service.CreateLanguage` / `CreateTranslation` | `api/v1/dict/language` | 当前 SYS_ADMIN |
| G257 / A01-E10 | GET `/api/v1/dict/enum` | `Service.Enum` / `Enum` | `api/v1/dict/enum` | 当前登录及该资源权限 |
| G258 / A01-E10 | GET `/api/v1/dict` | `Service.Page` / `Page` | `api/v1/dict` | 当前 SYS_ADMIN |
| G259 / A01-E11 | GET `/api/v1/dict/language/:id` | `Service.Languages` / `Languages` | `api/v1/dict/language/:id` | 当前登录及该资源权限 |
| G260 / A01-E11 | DELETE `/api/v1/dict/column/:id` | `Service.DeleteColumn` / `DeleteDictionary` | `api/v1/dict/column/:id` | 当前 SYS_ADMIN |
| G261 / A01-E11 | DELETE `/api/v1/dict/language/:id` | `Service.DeleteLanguage` / `DeleteTranslation` | `api/v1/dict/language/:id` | 当前 SYS_ADMIN |
| G262 / A01-E11 | GET `/api/v1/dict/protocol/service` | `Service.Protocols` / `Protocols` | `api/v1/dict/protocol/service` | 当前登录及该资源权限 |

这两张表是全局字典，没有 tenant 列；不按租户复制字典。`SYS_ADMIN` 对应现有 native `platform_admin`。现有身份 owner 明确允许 SYS_ADMIN 的 tenant_id 为 NULL/空串，初始化管理员也使用空串；适配保留这一规则，普通主体仍必须有租户。空租户例外仍须核验当前数据库账号为 SYS_ADMIN、状态 N、相同主体/租户与当前 Casbin；不能只依赖 token 中的角色。

最终调用闭包仅发现上述八个旧 handler → 原 service/DAL，没有需要后台兼容 client 的实际业务消费者。因此不另建后台 HTTP client。按用户要求，现阶段保留 `internal/service/sys_dict.go`、`internal/dal/{dict,dict_language}.go`、`ServiceGroup.Dict` 和旧 handler 主体，真正删除留待共同验收后的清理卡。`TP_PLATFORMCONFIG_OWNER` 未设置或 disabled 时保持既有旧入口行为；明确选择 encore 时，八个旧 handler 在解析输入、读取 claims 或触达旧 DAL 前统一返回 HTTP 503 / code 100001，native 失败不会回落旧 SQL。无效 owner 值也拒绝旧入口。这个阶段是可切流候选，不能把保留旧实现的状态登记成最终单源码清理已完成。

## 2. 当前身份与细粒度授权

每个业务用例执行 SQL 前，向既有 Community 身份/权限 owner 的固定内部地址发请求：

```text
POST /internal/platformconfig/authorize
X-TP-Service-Token: <独立受信部署凭证>

{"protocolVersion":1,"tenantId":"既有租户或平台空串","subjectId":"已认证主体","operation":"read-enum"}
```

回应必须同时回显 protocolVersion、authorized、tenantId、subjectId、operation 和固定 resource。consumer 拒绝不符、缺 tenant 字符串、未知字段、额外 JSON、跳转、非 200 和过大响应；没有自动重试。调用传递当前请求 context，超时上限 2 秒。该凭证不是用户 bearer，也不是旧安装 execution grant，不能用它绕过本端的现有会话认证。

服务端 `AuthorizeCurrentDictionaryOperation` 只读取既有 `users` 和当前 Casbin，不读取或写入字典表，不另建身份/权限模块。它验证当前 subject、tenant、N 状态和已有 authority；写/分页须当前 SYS_ADMIN；全部操作须对应资源的 `g2` 已登记，并通过已有 enforcer 的 `allow` 动作。未登记路径不再按旧 middleware 的动态 URL 漏洞放行。既有 Casbin matcher 仍归原 owner 管理。

部署必须核对表中各 canonical 资源的实际策略。候选不自动 seed 授权；源码中没有找到可证明这些资源已全部登记的生产策略。动态 ID 统一映射到表中的模板资源，不能只给一个 `api/v1/dict` 宽泛权限。授权服务不可用不执行业务 SQL。

共享 authn 的空租户兼容只接受原 `SYS_ADMIN` authority；签名、冲突凭证和每次 Community session 检查保留。session 回显必须是相同有效 SYS_ADMIN，并明确提供 tenant_id 空串或 NULL；遗漏字段、空白、外租户、角色撤回、禁用或会话拒绝都失败。native mode 继续使用原 authcore 当前账号/会话/委派规则。

本合同关闭当前实例入口权限检查。它不声称 Casbin 政策已跨进程即时传播，也不把权限核验与后续数据库事务之间的竞态描述为 receiver 事务围栏。

## 3. 现有 PostgreSQL 资源与迁移

当前源码没有可证明映射到 Community 原库的 `platformconfig` Encore SQL 资源；因此 host 明确注入现有库连接，不声明/启动一个新的空数据库。配置例：

```json
{
  "databaseURL": "postgres://runtime-user:provided-secret@existing-db:5432/existing-community-db?sslmode=require",
  "expectedDatabase": "existing-community-db",
  "schema": "public",
  "authorizationBaseURL": "http://community-internal:9999",
  "messagesFile": "/etc/thingspanel/messages.yaml"
}
```

配置以只读私有文件挂载，`TP_PLATFORMCONFIG_CONFIG_FILE` 必须是绝对路径。messagesFile 挂载同一后端版本的既有 `configs/messages.yaml`，不维护一份分叉语言目录。凭证不进公共制品或日志。

| 开关 / 资源 | 行为 |
|---|---|
| `TP_PLATFORMCONFIG_OWNER` 未设置或 `disabled` | native host 不读配置、不创建 SQL pool；native 八业务入口和 readiness 不报告可用；原旧入口仍按既有规则处理 |
| `TP_PLATFORMCONFIG_OWNER=encore` | 需要显式 `TP_AUTH_MODE=native` 或 `legacy-jwt`、完整配置和受信权限凭证；其他值启动失败 |
| `TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN` | 两端同一专用凭证，至少 32 字节；Community callback 也须显式 owner=encore |
| SQL 连接 | 校验解析出的数据库名与实际 `current_database()` 均等于 expectedDatabase；每条连接固定 search_path=public，不接受猜测 schema 或空库替代 |
| readiness | GET `/api/v1/platformconfig/health`；仅返回 owner/ready，不回显配置。检查已接管库、账本和 schema；当前账号/策略的真实可用性另由 HTTP 合同回归验证 |

新增迁移 `sql/platformconfig/001_adopt_dictionary.sql` 只验证并接管已有 `public.sys_dict` 和 `public.sys_dict_language`，不创建字典表、不复制或重播 seed、不改已执行 `1.sql`/`4.sql`。首次安装仍由已有历史 runner 建立原 schema。`initialize.ApplyPlatformConfigSchema` 复用现有 schema 排他与事务执行机制，在 `native_component_migration_ledger` 中使用独立 module=`platformconfig-dictionary`、独立 file/checksum 记录；不合并 `sys_version` 或其他 owner 账本。只有原初始化 runner 调用这一步，native host 不运行另一套迁移。

校验函数使用唯一 SQL 源，检查原列/长度/空值/默认值/精度、真实表类型、RLS 状态、PK、唯一约束和已验证的级联外键。重启不仅核对账本，还核对数据库校验函数的原文、语言、稳定性和 invoker 属性，再核对当前 schema。函数替换为 `SELECT true` 或 schema 漂移均不能复用旧账本报告就绪。失败和零行 receipt 按现有 owner runner 回滚，提交/回滚结果不明仍保留 unknown。

这是 PostgreSQL 第一条单源码迁移样例。没有把它计作 MySQL、TDengine 或完整四 profile 的实现。

## 4. 业务兼容与失败语义

成功保持旧 URL、字段名、HTTP 200 和 `code/message/data` envelope。消息读取原目录；空 Accept-Language 对响应消息仍是 zh_CN，但枚举翻译按原 FormatLangCode 是 en_US。`language_code` 输入仍验证，但 enum 按原 header 决定翻译；没有翻译保留空字符串。协议菜单未传 language_code 仍用 `zh`，保留旧 code 的 `DRIECT_ATTACHED_PROTOCOL` 拼写，device_type 是字符串 `"1"/"2"/"3"`。原空 enum/protocol 的 data:null 和页面/语言列表形态有回归。

父删/语言插入共享父行锁；子删除先父后子；已有行删除必须影响准确一行并核对最终状态；已不存在的删除可重放。触发器 RETURN NULL、SQL raise、写入改写及事务结果未知不返回成功。分页 count/list 同快照；所有请求传递取消信号，不以独立 background context 继续业务写入。

旧创建输入没有 operation key，本次不虚构原键恢复能力。提交丢回包返回既有 code 101001，并附 `outcome=unknown, automatic_retry=false`；需要人工/调用方核对现有字典，不自动重复创建。拒绝权限用已有 201001，冲突用已有 201002；普通参数错误仍 100002。新安全分类不会把原 SQL 错误文本或依赖响应泄露给客户端。Encore 在进入 raw handler 前的认证失败由既有 AuthHandler/框架处理，其 HTTP/前端兼容必须在实际部署回归中证明，adapter 单测不替代这一项。

## 5. 验收与接管顺序

下面命令使用总负责人创建的独立甲测试库。`TP_PLATFORMCONFIG_TEST_DSN` 必须是 literal loopback、数据库 `tp_takeover_platformconfig`、用户 `platformconfig_fixture`；测试再次核对数据库实际身份。权限测试使用独立随机 schema；public 接管/ready 测试持有专用 fixture lock、拒绝任何预存对象，只清理由自身建立的对象。

```sh
# backend 根 module：原 runner 与当前权限，以及选择 native 后旧入口关闭
go test -count=1 -v ./initialize -run '^TestPlatformConfigSchemaPostgres'
go test -count=1 -v ./internal/service ./internal/api -run '^TestDictionary'

# 现有 Encore module
go test -count=1 -v ./platformconfig/...
go test -count=1 -v ./l1-access/authn -run 'TestLegacy.*Tenant|TestCheckCommunitySession'
```

实际发布验收还须完成：

1. 固定 backend/Encore 同一源码、配置、migration 和镜像摘要；`--services` 必须实际包含 platformconfig 和既有认证依赖，公共 release/dispatcher 由总负责人唯一合入。
2. 在第三套验收环境接管真实已有 PG 字典/翻译，保留原 seed 与业务数据；执行 owner runner 后再启动 native host；原库不可确认或账本不符时保持不可用，不创建新库弥补。
3. 停止全部仍含旧业务 SQL 的写者/旧二进制，或使用已经证明有效的数据库权限围栏。只设置新进程环境开关或添加 advisory lock 不能证明老版本已退出。
4. 公共 ingress 一次性把 G255～G262 全部送往实际发布 platformconfig；旧进程直连八条均拒绝，失败不得回落旧 SQL。
5. 通过真实登录、当前策略、发布制品运行全部八 URL：创建/语言/枚举/分页/协议/删除，平台空 tenant、普通读者、撤权/禁用/注销、取消、旧数据接管、重启及丢回包。复测客户端错误处理。
6. 共同门禁通过后，才能删除旧 service/DAL、ServiceGroup 字段和旧路由；回退先停止新写者、核对当前数据与兼容性，再按维护授权恢复旧路径/制品，不能同时重新启两套写者。仅本次守卫默认放行旧逻辑并不证明旧 DAL 已获得 native 的事务修复。

责任：甲负责业务、授权合同与 schema；总负责人负责共享构建/dispatcher/迁移调用/门禁和固定版本验收；乙接入正式 installer 的配置与启动生命周期。影响 T01、A01-E10/E11、T02、Q02/Q04、R01/R03 及 J-G0/J-G1/J-G4。模块 PASS、实际 Encore HTTP、制品发布链和生产切流分别记账。
