# 暂停与恢复交接 — 2026-10-27

**状态：用户明确要求暂时暂停，以便重启电脑；不自动恢复开发或验收，等用户要求继续。**

## 目标与边界

继续完成原有设备、设备模板、设备分组三菜单功能；6.1 Sol统筹和验收，Luna执行明确小任务。模板只保存一份当前configuration，无草稿、模板版本或发布；物模型版本保留。保护无关脏工作，不reset/clean/stash/merge/push/deploy，不执行生产迁移。本轮仅本地隔离验收。

## 已保存工作区

- Kernel：`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/kernel`，baseline `fafe0f6f8c7d8f58db69e1b4159fedaa7e55eb05`。
- Backend/任务书：`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/backend`，baseline `d638af3e84221b2bb29b1295deef9e72521b8aa3`。
- Frontend：`/Users/junhong/.codex/worktrees/device-metadata-native-cutover-1.3/frontend`，baseline `7d331d15329af0d636265a8a97b9534fff435a21`。
- 都是未提交候选；文件已落盘，未做提交或推送。不要将原始脏checkout和选定隔离候选混合。

## 当前进度与验证

1. **设备/模板的自动化、告警四Tab**：固定Workbuddy A30源码`fbdd653d`集成，实际UI/HTTP及合成遥测触发已验。创建先禁用→确认绑定→CAS启用；覆盖/解绑、最后解绑停止触发、告警分页与确认已验。A31未集成，旧type11迁移、真实通知/物理MQTT/真实IDP仍未整体验收。
2. **设备列表、默认编号和组计数**：原生搜索、分页25=20+5、模板筛选、父子组并集去重和未分组23通过实际HTTP与浏览器。编号省略由服务端使用设备ID，合法自填保留，同key重放稳定；未知运行数据保持null/503。
3. **普通新增和标签**：真实UI无模板、standalone模板、gateway模板创建均201；native binding与GET都确认templateId，gateway配置决定deviceKind。编辑标签PATCH200后刷新读回，中文/逗号完整保留，非空labels KV不被覆盖。初次503为owned fixture缺UPDATE列权限，修最小列授权后通过；不宣称跨刷新同key重试已验。关联事实是`device_access_bindings.template_id`，旧`devices.device_template_id`为空不能判断关联失败。
4. **凭据后端**：0030共用设备/模板凭据owner及显式keyring加密基础；0031扩展原有metadata_idempotency，不新增秘密服务。GET摘要+POST token/用户名密码/公开X509、密文响应、HMAC请求摘要、重放重新授权、10分钟固定领取窗口、墓碑/撤销拒绝已实现。11项联合定向测试通过。没有activation/reveal/rotation交付声明；过期未访问密文的后台定期擦除仍待补。0030/0031已在Encore隔离测试库应用，不能回改checksum。
5. **凭据前端**：typed client和现代连接Tab已实现；同key未知重试、成功后摘要刷新失败不重建、secret只在组件内存、仅join Tab使用show:lazy保留未知状态、路由离开保护。Root最终25项前端unit全过，最终父/子SFC编译通过；局部类型检查由Luna通过，全仓vue-tsc有既有错误，不能宣称全仓通过。领取窗口不是凭据本身到期时间。**真实凭据HTTP/UI尚未完成。**
6. **空分组删除**：新增DELETE、soft-delete、If-Match/幂等审计、子组/成员冲突与并发树锁；12项删除/move/member主测试通过。当前A30没有native group引用，不为不存在的owner新增registry/503。旧public.groups引用迁移仍需单独对账。**真实DELETE HTTP/UI尚未完成。**
7. **文档纠偏**：已清除DOMAIN-MODEL/DATABASE正文中残留的模板版本/草稿/发布设计，并同步任务状态。

仍按27个页面功能块追踪，完整三菜单尚未签收；上述是各切片证据，不能把12/11/25测试数当功能完成比例。详细剩余项见FUNCTION-STATUS-2026-10-27.md。

## 冻结制品与证据

安全制品已另存本仓`evidence/2026-10-27-pause/`，索引为`SHA256-INDEX.json`。不含private env、认证token或部署keyring。

- 当前backend **479文件**：`backend-source-manifest-credentials-groups.json`，SHA256 `9654b44a418d1f76c9b0be296361956f42bd8cb45e3ceeb5de2281f81f31e734`；源码归档`backend-source-credentials-groups-479.tar.gz`，SHA256 `de80594fb88be5335208097494e075076d7b5b6b6b72c26d80e37f60c2fc1916`。
- 当前frontend **80文件**：`frontend-source-manifest-credentials.json`，SHA256 `c5059615a9f88a2b0b141a5eb2f28825647eb9c65151032640767159396bf642`；源码归档`frontend-source-credentials-final.tar.gz`，SHA256 `14378fb6876d58a72a1ff37fef8e76421dd6e0038686dcf56d29a01f19a99f56`。
- 凭据后端11测试日志SHA256 `2f30543e17d667b3d53f5353b5c41dd6840f195409f934c95ff9dc3e4b0ce996`。
- 分组12测试日志SHA256 `3acc79c025c5e9c989b9c771bea1557def4f2c622a84f98a6a856d9d9c626ab8`。该日志最初root看不到子agent的DevScratch位置；已转移共享worktree并核验原始日志/hash，再复制进证据目录，未为路径问题重跑。
- 前端25测试日志SHA256 `68f08e4b46357b3ddac1227ec2e9690abe1f62ef1114d294203a6c0c31c81e5b`。
- 普通登记/标签UI摘要SHA256 `c9abcce2ab06bab812e2e9e82790fc2b9b791a1a8f7f83eb7bb0979f7273b656`；阶段manifest引用前次list freeze，明确排除并发凭据Tab变化。

## 运行环境与暂停点

本轮owned PG名称`codex-device-management-7312c5b5-pg`，app/stub精确名称见PAUSE-QA；旧API `127.0.0.1:55067`、Vite `127.0.0.1:5114`。不要复用用户5002端口。暂停时已完成479归档与overlay逐文件核对（0差异），未启动build/test，未应用owned PG的0030/0031，未创建keyring。自有Vite/API app/stub/PG均已停止，PG AutoRemove=false、数据卷和network保留。确切资源名与恢复命令，以Luna收尾文件`PAUSE-QA-2026-10-27.md`为准；用户暂停时尚未验收479镜像，不能认定新凭据/删除已在旧镜像中。旧镜像为backend466，不支持新增端点。

Scratch：`/Volumes/DevScratch/device-management-native-20261027`。私有recovery文件保留原位置，不拷进Git。synthetic登录已轮换，不能继续使用旧会话。正确Vite启动使用已保存`start-vite-static.sh`，显式static路由和5114 strictPort；pnpm dev脚本曾吞参数导致误端口，错误owned进程已停。

## 用户要求继续后的顺序

1. 先读本文件与PAUSE-QA，检查worktree状态/进程和容器是否停止、私有备份是否可恢复；只操作本轮owned资源。不要重复已通过的列表、登记和标签整轮。
2. 恢复同一owned PG/stub/net，按PAUSE-QA确认0030/0031实际是否已应用；如缺则仅追加，保留原checksum。按归档479逐文件核对overlay普通clone，查看已有build结果再决定是否继续构建。Encore build/test仅一个执行者。
3. 一次新镜像完成凭据GET/POST/no-store/同key重放/越租户及分组DELETE/move/member真实HTTP，再跑前端80的凭据Tab真实UI/未知请求换Tab与离开保护。不得把pending或202当实际接入成功。
4. 确认该切片后更新TASKS/tasks.json/FUNCTION-STATUS/EVIDENCE。继续原有连接/runtime与其余Tab，复用原owner，不另造broker系统；原checkout未跟踪brokercontrol未来候选不能视作当前1.3实现。

## 记忆候选

候选已写`/Users/junhong/.codex/memories/extensions/ad_hoc/notes/20261027T010604Z-native-device-default-number-and-group-counts.md`；当前没有memory MCP正式反馈，**尚无正式记忆ID**，不声称已正式入库。
