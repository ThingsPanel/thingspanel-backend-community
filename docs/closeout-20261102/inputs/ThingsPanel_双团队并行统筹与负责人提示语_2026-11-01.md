# ThingsPanel 双团队并行开发统筹与负责人提示语

> 日期：2026-11-01（北京时间）。本轮已完成方案和关键源码对照，未实施代码、部署或运行验收。
>
> **甲队负责平台核心、Encore与存储；乙队负责插件交付、市场安装、前端和正式安装器。**
>
> 本文调整两份原任务中的交叠负责人、接口前置和联合验收。原任务编号A08不代表“甲队任务”；其安装编排及这部分Encore迁移整体交乙队。原有业务不变量与测试要求继续有效。

## 1. 结论和必须解决的冲突

两项工作可以同时开始。主要冲突在共享源码、业务登记、迁移执行、安装器和运行资源；仅建两个分支无法保证互不影响。

| 冲突 | 具体后果 | 本轮分工 |
| --- | --- | --- |
| 原A08迁移与插件安装扩展同时修改现有编排 | 两队搬迁、扩展同一文件，或形成两个安装中心 | 乙统一负责现有市场发布/安装编排、记录、恢复及其Encore迁移；甲提供模型、设备绑定、文件等业务端口 |
| A10现有插件登记与“安装后自动登记” | 创建两份目录、重复服务或绕过业务权限 | 运行登记/配置/回调仍归甲；乙通过受控API登记并保存引用 |
| 原Q02与Agent安装接线 | 两份正式Compose、迁移执行重复、镜像/配置版本错配 | 甲交核心制品与运行/迁移契约；乙统一维护现有正式installer和Agent接入 |
| 多数据库与乙新增安装表 | 核心已用MySQL，安装状态仍暗留PG | 乙负责安装表族PG/MySQL实现；甲纳入资源清单和最终矩阵 |
| **ThingsVis的实际PG依赖** | 带看板的组合安装无法在无PG环境运行 | 乙负责现有ThingsVis持久层、迁移和安装适配；甲协调共享库数据搬迁，不遗漏或删除其schema |
| 凭证、待激活、升级切换 | 安装器有了容器却没有正确业务授权，旧实例仍可能写 | 甲守业务身份/投递/绑定；乙守主机执行/网络/资源；共同验证切换屏障 |
| 两队改同一前端、SDK或公共文件 | 请求、菜单、协议和制品同时漂移 | 前端乙统一维护，SDK业务协议甲维护，公共文件固定单一维护者 |
| 共用工作区/测试栈 | 覆盖未提交成果，端口/数据库/设备互相干扰 | 独立工作树与运行环境；遵守同仓串行规则，采用短写入窗口 |

其中ThingsVis是本次新增确认的实质依赖：installer的ThingsVis-server连接PG的thingsvis schema并依赖postgres；ThingsVis当前源分支的Prisma provider也写为postgresql。完整无PG组合不能仅验证backend。[E03][E06]

## 2. 证据基线与检查边界

### 2.1 两份原始输入

- Encore任务书：**ThingsPanel_Encore_Migration_Tasks_2026-11-01.md**，35个任务包、100张接口小卡。核心审计基线为社区后端main提交7f694779c979702740b15125164ebec2545604a4。
- 插件原文：**/Volumes/500G/docs/ThingsPanel_插件发布与一键安装设计_2026-11-01.md**。本轮完整读取761行，重点对照第5、7、9、10、12、14节及附录B/C。其v1.3.0研究基线与上述main不是同一制品；原文建议的Agent、安装状态与历史pluginmgr线索不能直接当成现成功能。

### 2.2 本轮核对的源代码版本

| 仓库 | 分支与固定提交 | 实际读取范围 |
| --- | --- | --- |
| thingspanel-backend-community | main，7f694779c979702740b15125164ebec2545604a4 | 沿用前轮完整路由/核心审计，本轮再次核对远端main和安装/插件接缝 |
| thingspanel-frontend-community | master，de13a7f0083fb0c9d95d0170375e44268d51333e | AGENTS、市场/插件API及相关目录 |
| thingspanel-installer | main，98f9715f2106be08f5a925d552af8a0b5810b2c2 | AGENTS、正式Compose、install.bash、发布workflow |
| thingspanel-horizon | main，11f61a4b47c0be3670d771bb9a06fd821fd631c9 | AGENTS、既有task-graph、契约变更流程、canonical Bundle schema |
| thingsvis | master，e22e42d7a0bcfa634d7b62f36dc901f177980913 | Prisma schema和Compose；未核验installer所用v1.0.23镜像内部内容 |
| device-connector-sdk-go | main，74758b2cd92fa5044fb58ed446a8a3d52642c515 | 仅核对分支头，本轮未全量审SDK |
| thingspanel-notification-plugin-sdk | main，3226fad59042a210d4390becc5229b0cb74fad20 | 仅核对分支头，本轮未全量审SDK |

这些是审计输入，**不是已经通过兼容测试的组合**。D0需固定源码、镜像摘要、协议、schema/config、数据库、OS与架构的配套清单，不以main或latest自动承诺兼容。

本地/Users/junhong/Downloads/code下的backend、frontend、installer及若干插件仓库存在未提交修改；installer还在feat/thingmodel-api分支。本轮没有修改它们。开工只读清点待保留成果，不擅自reset、clean、stash、切分支或覆盖。

本地已定位核心/前端/installer和若干连接器；Horizon、SDK及ThingsVis运行checkout未在已检查的code根目录中定位，已从明确远端取得关键源码。thingsvis-internal-docs是文档库，不能当运行实现。D0记录真实checkout，不把旧iot-api-encore-kernel当最新社区主线。

## 3. 仓库、模块与原任务归属

### 3.1 唯一负责人表

| 范围 | 主责 | 对方的协作入口 |
| --- | --- | --- |
| 核心身份/模型/设备/Broker/自动化/通知/北向/遥测/通用文件 | 甲 | 乙使用受控业务API，不直接改业务表 |
| 社区后端中的市场发布/安装编排、安装记录和恢复 | 乙 | 甲提供host模板、身份、业务端口与数据库规范 |
| service_plugins/service_access及运行配置/回调 | 甲 | 乙调用准备、查询、启用、撤销等必要能力，只保存引用 |
| 平台身份/授权/业务秘密保管 | 甲 | 乙保存秘密引用，申请限定权限；Agent身份/执行协议乙实现并接平台规则 |
| 核心连接、R/S存储规则、核心schema与迁移实现 | 甲 | 乙消费配置/迁移契约，不重写核心SQL |
| 安装计划、操作/步骤、实例、代次、资源归属等表族 | 乙 | 甲评审事务/方言与资源装配，纳入PG/MySQL及最终矩阵 |
| 社区前端、请求/路由/菜单接线 | 乙 | 甲交接口与权限变更样例，保持兼容或明确版本变化 |
| Horizon market-service、Portal、包规范与发布目录 | 乙 | 甲评审业务能力及兼容要求；Horizon不重建客户租户体系 |
| 设备/通知SDK业务协议 | 甲 | 乙提交安装所需能力与兼容样例 |
| 独立插件仓库、成品、签名、包与发布校验 | 乙 | 甲提供固定业务协议与SDK |
| 正式thingspanel-installer及Agent接入 | 乙 | 甲交核心镜像、资源/配置/迁移/健康/调度契约 |
| ThingsVis运行层、导入与数据库适配 | 乙 | 甲提供业务绑定/授权与共同数据搬迁边界 |
| 后端镜像workflow、Encore服务/资源选择和公共装配 | 甲 | 乙交安装host及制品需求，由甲接入公共清单 |
| 联合环境的单轮部署/升级 | 乙操作、甲核验业务与数据 | 环境加锁、两负责人签收；各自其他开发测试继续 |

业务owner、文件维护者、迁移执行者分别登记。同一文件混有编排与业务修改时，由其主责团队在一个小PR中拆清，对方评审语义，不能双方整文件重写。

### 3.2 对原35包的调整

| 原任务 | 本轮派工调整 |
| --- | --- |
| T00/T01/T02 | 甲主责，乙参与安装需要的身份、幂等与待启用接缝；模板和最小端口提前交付 |
| T03/T04/T05 | 甲立即修复删除、遥测和自动化已知风险 |
| R01/R02.a/b | 甲负责平台机制；乙登记自身表族及ThingsVis等owner/迁移入口 |
| R02.c | 甲牵头客户平台关系数据搬迁，乙提供安装表和ThingsVis专属步骤及对账 |
| R03～R09 | 核心表族甲；涉及市场安装表的切片乙实现，双库门槛不变 |
| S01～S05 | 甲主责；乙不自改历史库或把安装代次代替业务世代 |
| A01～A07、A09 | 业务后端甲，相关前端乙；甲给稳定输入输出及迁移样例 |
| **A08** | **市场访问、发布/安装编排、安装记录、恢复及其Encore迁移整体乙**；模型/设备/文件业务owner仍甲 |
| A10 | 业务运行登记/配置/回调和脚本甲；安装调用adapter乙 |
| A11 | 平台业务与通用文件甲；市场/可视化页面乙 |
| Q01 | 甲提供生命周期模板，各队关闭自己创建的资源及worker |
| **Q02** | 甲交应用制品/runner/资源契约，乙交唯一正式installer/Agent/接线，联合验收 |
| Q03/Q04 | 各队验证并退出自己负责的旧实现；甲维护全量路由退役台账，乙交A08证据 |

原100张API卡仍是精确范围索引。附录中主任务为A08的接口和相应旧写者/后台恢复一起转乙；A10不会因为名称含plugin就整体转乙。原技术要求继续适用，负责人以本统筹为准。

## 4. 同时开工与最小共同前置

### 4.1 D0只冻结接缝

两负责人共同交一份短清单：

1. 每仓库真实路径、干净基线、待保留本地成果、制品摘要及版本。
2. 当前安装编排唯一实现、最终Encore host、API/Store目录、搬迁顺序；历史pluginmgr/Agent按源码查证。当前社区main树未发现同名实现路径，不代表外部仓库没有。
3. J1～J8首版、已有能力、确实缺少的业务端口、可执行消费样例。
4. E-A/E-B/E-I环境资源、操作人、仓库写入窗口及公共文件维护者。
5. 甲的固定PG参考制品与乙的安装消费fixture；fixture明确标注，不能作为实际组合通过的证据。

共同记录沿已有docs/contracts/tests保存。合同真源仍归所属仓库，协调清单只引用路径、版本和摘要，不另建契约平台或第三团队。

### 4.2 两队立即并行的工作

| 甲立即推进 | 乙立即推进 |
| --- | --- |
| P0缺陷、身份/授权和旧接口契约录制 | 包规范、制品与信任校验、Horizon目录和拒绝样例 |
| T01单一源码迁移样例、T02协议adapter | Agent持久代次、资源预留、受限执行与恢复测试 |
| R01/R02连接与迁移机制、真实驱动夹具 | 前端安装流程及API fixture、插件成品构建 |
| S05.a跨库协议、S01历史语义、核心owner适配 | ThingsVis存储兼容审计、Linux主机能力和既有installer接线 |

乙可先在固定PG混合拓扑完成首链，不等待甲全量去Gin或四库全部完成；甲无需等Agent。未确定的合同只阻塞对应接线小卡。

### 4.3 同仓库采用短写入窗口

Horizon已有任务图的sameRepositoryParallelism:false、契约任务01、后端市场04→05和前端06→07等依赖继续遵守，已完成成果按证据复用。[E05]

本次跨队协作在共享仓库同一时段只安排**一个活动写入团队**；队内仍遵守各仓库与任务图的writeScope、依赖及串行规则。独立worktree保护文件，不自动解除规则。

后端由甲核心窗口和乙A08窗口交替，每次限一个小任务/可审查PR，交接提交、文件、未完成点和测试结果；不能占住仓库等整批完成。无窗口的一队推进其他独占仓库、只读评审、合同设计或独立测试。

公共go.mod/go.sum、核心装配、Encore服务选择、平台配置/migration索引由甲维护；正式Compose/安装脚本由乙维护。对方提交具体需求和版本，维护者语义合并，不整文件覆盖。纯搬迁与功能/数据库语义改变分开PR。

## 5. J1～J8共同合同

用现有OpenAPI、JSON Schema、Go接口与夹具表达这些接缝，不增加通用业务中心、消息总线或万能PluginManager。

| 合同 | 必须固定的语义 | 甲/乙责任 | 必测反例 |
| --- | --- | --- | --- |
| J1 身份权限 | 用户、服务、Agent、插件身份分离；可信tenant、主机/实例/用途、secret引用、吊销与有效期 | 平台业务规则甲；安装计划与Agent验证乙 | 跨租户访问；普通插件调用安装管理 |
| J2 准备/查询/启用/撤销 | 各owner的最小等价操作；稳定operation/step/resource key、请求摘要、归属、创建/复用、binding CAS | 甲实现业务端口，乙消费和恢复 | 提交成功丢回包后查询同一资源 |
| J3 内容与版本 | 平台、业务协议、SDK、包规范、Agent、config/schema版本，镜像及附件digest、能力/profile | 核心及SDK甲；包/锁文件/目录乙 | 标签换内容、未知必需能力、失效目录 |
| J4 持久化幂等 | 同意图同结果、异内容冲突；操作和待处理记录同事务；Commit未知原键查证 | 业务结果甲；安装状态/worker乙 | 双击/并发100次；不同合法实例不得误合并 |
| J5 代次/世代 | 安装执行代次、route_epoch、设备删除世代、binding revision独立 | 业务世代甲；主机最高代次乙 | 过期任务执行前拒绝；Agent丢状态不能重置为0 |
| J6 待激活/切换 | 程序、配置、运行健康、业务启用分别判断；暂停、在途、旧实例隔离、独占资源交接 | 业务投递/绑定甲；进程/网络/资源乙 | inactive仍外发；旧主机失联；健康200账号无效 |
| J7 资源迁移 | 关系/时序独立配置，schema owner、runner、账本、错误、ready、退出、tick/恢复循环 | 核心契约甲；安装/ThingsVis/Agent资源与installer乙 | 双runner、半迁移、隐藏PG、假ready |
| J8 恢复卸载 | 超时不等于停止；只补偿本次新建且无他人引用资源；程序/config/专属数据/绑定共同恢复 | 业务账本甲；交付补偿乙 | 误删用户数据、重复发送、误释放旧实例资源 |

现有ServicePlugin.Create直接生成UUID，单凭CRUD不能满足可查询幂等登记。甲须补稳定操作键、唯一约束、归属查询与待启用能力；乙不能靠盲重试或直写业务表弥补。[E07]

J5/J6由两负责人共同审查失败时序。合同变更附差异、受影响小卡、兼容方案、双方测试和退出条件；未确定时保持旧版本，其他工作继续。

### 5.1 包规范不得复制业务契约

Horizon的contracts/market-solution-bundle/v1/manifest.schema.json是资源Bundle真源。新的运行包说明只负责交付外层及引用；沿用deviceBindings[]、bindingKey与原CCP门禁，不放入客户真实device/tenant/user ID或凭据。[E05]

通知HTTP v1、设备SDK、北向各保留其协议。包digest与业务manifest digest、安装代次与业务幂等分别记录。新增optional字段是否兼容也需实际消费者测试，不凭字段名称作保证。

### 5.2 安装编排的Encore迁移

乙以现有唯一实现为起点，负责扩展并最终按甲T01/T02模板迁到约定host。可以先做纯Go用例、PG参考环境和fixture；不能复制原实现或把暂留Gin当最终完成。

host切换须清点全部API、后台恢复、安装表和写者，停旧受理/worker，处理未完成及unknown状态，迁移唯一实现后启新owner。临时client单向、显式、无自动fallback并有退出卡。安装兼容第三方插件调用已发布扩展接口，不动态把下载源码变成Encore服务。

## 6. 数据库与ThingsVis新增工作

### 6.1 数据与迁移归属

甲负责核心业务R/S数据；乙负责交付计划、操作/步骤、部署实例、代次、资源归属及配置引用；ThingsVis、Agent和插件专属数据各保留明确owner和恢复规则。普通插件不接平台主库。

统一的是发布时的迁移登记/调用协议。异构组件保留自己的迁移工具及账本，不强迫ThingsVis重写成Go ORM；同一schema同一时刻只有一个有效执行者，无依赖schema可独立迁移。installer调用所属owner runner，不复制SQL；Agent不迁平台核心表。

### 6.2 乙新增TV工作包

已确认installer将ThingsVis连接到ThingsPanel库的thingsvis schema并depends_on postgres，且ThingsVis源master的Prisma provider为postgresql。本轮未验证MySQL能力，不能只改DSN便宣称兼容。[E03][E06]

乙须定位运行checkout、核对实际镜像、数据模型与迁移，保留业务层，仅扩展必要的持久层、客户端生成、目标引擎迁移、查询与配置；制品profile/driver能力纳入J3/J7。验证看板创建/编辑/读取、导入导出、设备绑定、租户权限、重启持久性、升级及恢复。

甲在R02.c中牵头整体边界；乙提供ThingsVis专属导出、转换、导入与对账。源PG共享库先清点schema，不能只迁核心表却宣布整库完成，不能删除未知schema或整库回滚覆盖另一owner的新数据。

阶段上可以明确提供“未启用看板”的profile并拒绝依赖看板的包；**不能用它宣称含看板的完整四组合通过**。该工作仍待完成，除非用户另行明确缩小产品范围。

### 6.3 无隐藏PG的范围

客户部署中的平台核心、安装编排、已启用组件与组合包依赖都进入profile清单。MySQL+TDengine完整profile不能暗留PG保存安装或ThingsVis数据。

公网Horizon独立服务的数据库不随客户选项自动更换；已安装业务应能在市场离线时继续运行。一体私有Horizon需另列资源及兼容profile，不能隐藏PG依赖。插件专属数据库需求也须显式申报，安装前按profile允许或拒绝。

## 7. 开发与测试环境隔离

各队使用独立checkout/worktree、构建输出、配置与缓存，不共用dirty目录、数据卷或密钥。按各仓库AGENTS处理开发环境；Windows/PowerShell开发与Linux插件运行目标分别遵守。

| 环境 | 操作者 | 作用 |
| --- | --- | --- |
| E-A | 甲 | 核心API、四存储组合与业务故障 |
| E-B | 乙 | Agent、安装、前端、ThingsVis及双关系库持久性 |
| E-I | 乙负责单轮部署，甲核验业务/数据，双方签收 | 锁定制品的联合故障、升级与恢复 |

这只是三套环境，没有第三团队。Agent涉及主机管理，默认不同测试VM或不同Docker daemon；两个context若指向同一daemon不算隔离。共用宿主前必须证明全局资源已隔离。

必须隔离：全部逻辑库/schema/用户/迁移账本/备份，Redis，MQTT broker/凭据/client ID/topic/route owner，Agent主机身份与状态目录，容器/网络/IPAM/宿主端口，串口/设备/卷/挂载，HTTP域名/cookie/回调，模拟设备/厂商/SMTP。

当前正式Compose含固定thingsvis-server、thingsvis-studio容器名与宿主端口；install.bash默认INSTALL_DIR是/opt/thingspanel。仅docker compose -p不足以隔离，要核对最终渲染配置及独立安装目录。清理只限本环境归属资源，不运行全局prune、不删除陌生实例。[E03]

## 8. 各自测试与联合门禁

### 8.1 甲独立测试

真实验证PG+TimescaleDB、PG+TDengine、MySQL+TimescaleDB、MySQL+TDengine，覆盖选用核心逻辑资源、API、worker和迁移。重点是权限/租户、幂等冲突、Commit未知、锁/租约、乱序、历史读写/聚合/导出/清理、删除重放、自动化唯一owner及退出排空。

提供安装业务端口producer合同测试：已提交丢回包、重复/异内容、待启用、绑定CAS、撤销本次资源和unknown保留。乙新增安装表、ThingsVis在最终相关profile合入后，重跑受影响组合；不能沿用旧矩阵结果。

### 8.2 乙独立测试

安装计划/操作/步骤/实例/归属/代次和worker在真实PG与MySQL跑相同持久性测试。按原插件文档第12节覆盖：

- 同请求连续和并发重放100次得到同一逻辑操作/目标实例；不同合法实例可独立安装。
- 在关系提交、Agent受理、资源创建、业务登记/启用前后中断；提交未知原键查证，不凭日志猜完成。
- 持久最高代次、实例锁与原子资源预留；过期任务执行前拒绝，端口/串口/预算竞争正确处理。
- 篡改包/权限/附件、错误发布者、失效目录、依赖/计划指纹变化运行前拒绝；离线依赖和信任链完整。
- 待激活真实运行层无未授权外部写；账号无效不显示可用；失联旧实例未证实退出/隔离时不启替代写实例。
- 回退包含程序digest、配置、专属数据和绑定；卸载保留原设备/历史/人工修改及共享依赖。
- UI、权限、动态菜单与实际入口；外部管理实例诚实显示能力，ThingsVis按TV任务验收。
- 原平台安装行为保留；Agent只对实测OS/arch开放自动纳管。

模拟器仅证明己方行为，不能替代另一队真实组件。

### 8.3 联合验收

| 门禁 | 必须通过的内容 |
| --- | --- |
| J-G0 | D0基线/owner清单、J1～J8首版及可执行provider/consumer合同 |
| J-G1 | 固定PG首链：成熟设备连接器成品→Agent准备→模型导入→设备绑定→启用→模拟上报可读→停用/依赖预览；双方真实组件 |
| J-G2 | 丢回包、双方进程重启、双worker、旧代次、资源抢占、失联旧实例、版本不符、回退和unknown等组合故障 |
| J-G3 | 同一闭环与关键故障在四profile通过；安装表、已启用ThingsVis/看板依赖一起切换，MySQL+TDengine无必需PG |
| J-G4 | 唯一正式installer新装/升级/迁移中断；平台、Agent、插件分别重启/升级，兼容与不兼容数据恢复，跨关系库/时序搬迁按维护协议 |

首闭环保留物模型/设备优先，通知可并行无发送校验。甲签业务/权限/存储结果，乙签制品/主机/安装恢复和入口。两份独立通过不能代替联合通过。

固定输入未变的有效结果可复用，联合重点验证接缝，不机械重跑全部无关测试。失败按第一个违约边界归属修复，只阻塞对应组合。

## 9. 发布、升级与交接

甲交核心commit/digest、服务选择、配置/秘密引用、schema兼容、runner、health/readiness、退出、tick/续期要求。乙交唯一installer如何调度它们及Agent。各owner实现自己迁移，installer只调用并等待。

普通插件安装、平台升级、Agent升级、数据库搬迁分别是不同操作。插件版本要求更高平台时，转既有平台升级流程，不能自行连带升级核心、Agent或共享数据库。

对同一主机/实例/绑定/数据库的操作登记互斥与顺序。超时不代表停止或释放；旧通知/控制账本不能因恢复旧快照而重发。程序回退先证明schema/config/data兼容，必要时走一致备份和停写窗口。

Agent首期Linux amd64/arm64按实际验证开放，保留installer现有其他平台行为；不支持自动纳管的环境明确外部管理，不能伪装已支持。

每次交接记录：负责人/任务/仓库窗口、源码及制品摘要、合同/schema/config版本、环境与支持profile、消费的对方版本或fixture、实际命令/成功和故障结果、未验证项、旧writer退出证据、资源清理/恢复、对方下一项精确输入。

明确区分实现完成、独立测试、联合验收、发布四种状态。本次方案和提示语不代表已经部署或批准生产发布。

## 10. 证据与引用

- E01：上一轮Encore任务书及其完整逐接口证据。
- E02：用户指定插件原文，完整761行，第5/7/9/10/12/14节与附录B/C。
- E03：installer [AGENTS](https://github.com/ThingsPanel/thingspanel-installer/blob/98f9715f2106be08f5a925d552af8a0b5810b2c2/AGENTS.md)、[Compose](https://github.com/ThingsPanel/thingspanel-installer/blob/98f9715f2106be08f5a925d552af8a0b5810b2c2/docker-compose.yml)、[install.bash](https://github.com/ThingsPanel/thingspanel-installer/blob/98f9715f2106be08f5a925d552af8a0b5810b2c2/install.bash)。
- E04：frontend [AGENTS](https://github.com/ThingsPanel/thingspanel-frontend-community/blob/de13a7f0083fb0c9d95d0170375e44268d51333e/AGENTS.md)、[市场API](https://github.com/ThingsPanel/thingspanel-frontend-community/blob/de13a7f0083fb0c9d95d0170375e44268d51333e/src/service/api/market-bundle.ts)、[插件API](https://github.com/ThingsPanel/thingspanel-frontend-community/blob/de13a7f0083fb0c9d95d0170375e44268d51333e/src/service/api/plugin.ts)。
- E05：Horizon [AGENTS](https://github.com/ThingsPanel/thingspanel-horizon/blob/11f61a4b47c0be3670d771bb9a06fd821fd631c9/AGENTS.md)、[task-graph](https://github.com/ThingsPanel/thingspanel-horizon/blob/11f61a4b47c0be3670d771bb9a06fd821fd631c9/docs/task-bundles/market-solution-bundle-v1/task-graph.json)、[契约流程](https://github.com/ThingsPanel/thingspanel-horizon/blob/11f61a4b47c0be3670d771bb9a06fd821fd631c9/docs/contract-change-workflow.md)、[canonical schema](https://github.com/ThingsPanel/thingspanel-horizon/blob/11f61a4b47c0be3670d771bb9a06fd821fd631c9/contracts/market-solution-bundle/v1/manifest.schema.json)。
- E06：ThingsVis [Prisma schema](https://github.com/ThingsPanel/thingsvis/blob/e22e42d7a0bcfa634d7b62f36dc901f177980913/apps/server/prisma/schema.prisma)、[Compose](https://github.com/ThingsPanel/thingsvis/blob/e22e42d7a0bcfa634d7b62f36dc901f177980913/docker-compose.yml)。
- E07：backend [服务插件创建](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/service_plugin.go)、[原生市场安装编排](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/market_native_install.go)、[Encore镜像选择](https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/.github/workflows/encore-image-build.yml)。

本轮没有遍历外围仓库全部业务源码，也没有运行组件、连接业务库或核验所有发行镜像。方案是基于已读取资料与关键证据的执行设计，不能表述为运行测试已通过。

[E03]: https://github.com/ThingsPanel/thingspanel-installer/blob/98f9715f2106be08f5a925d552af8a0b5810b2c2/docker-compose.yml
[E05]: https://github.com/ThingsPanel/thingspanel-horizon/blob/11f61a4b47c0be3670d771bb9a06fd821fd631c9/docs/task-bundles/market-solution-bundle-v1/task-graph.json
[E06]: https://github.com/ThingsPanel/thingsvis/blob/e22e42d7a0bcfa634d7b62f36dc901f177980913/apps/server/prisma/schema.prisma
[E07]: https://github.com/ThingsPanel/thingspanel-backend-community/blob/7f694779c979702740b15125164ebec2545604a4/internal/service/service_plugin.go

## 11. 两份可直接派发的提示语

将本统筹文件、原Encore任务书和插件原文一并交给负责人。在不同机器上执行时附原文副本，不假定远程绝对路径可访问。

### 11.1 提示语一：甲队技术负责人

你负责 ThingsPanel 核心、Encore 接口迁移、多关系库/多时序库、业务协议与 SDK、后端构建。请组织开发、评审和独立测试，持续交付可验证的小改动，不停留在方案讨论。乙队同步开发插件发布与安装；无需等乙队 Agent 完成再开始核心工作。

#### 一、先读依据并固定基线

先查 SkillHub 共享记忆，优先 `memory_query`，缺失时用 `mq`；再读相关仓库 `AGENTS.md`、`.ai/_项目开发规范.md` 和任务约束。主任务书文件名是 `ThingsPanel_Encore_Migration_Tasks_2026-11-01.md`，通过交付副本定位，不猜它在远程 Encore 目录的位置。插件原文是 `/Volumes/500G/docs/ThingsPanel_插件发布与一键安装设计_2026-11-01.md`，重点读第5、7、9、14节和附录B。

原核心审计提交为 `7f694779c979702740b15125164ebec2545604a4`；插件报告的 v1.3.0 基线不等于该提交。先与乙队确认 backend/Encore、frontend、Horizon、installer、SDK、首个插件的实际提交、协议和制品指纹，并登记相关差异。历史记忆提及的 pluginmgr/Agent 必须按源码核实，不能据名字重复创建。

已知 backend 本地 `0e3ba82` 有大量未提交改动，installer 在 `feat/thingmodel-api`、`583e64e` 且有改动。这些不是可直接共用的开发基线。只读盘点，由原负责人确认待保留成果；不擅自 stash、reset、clean、切分支或覆盖。基线确认后建隔离工作树和构建目录。

#### 二、严格执行唯一负责人分工

甲队保留模型、设备、账户权限、Broker/MQTT、命令、自动化、通知、北向、遥测、通用文件与生命周期；负责业务协议/SDK、T01/T02模板、R/S数据库任务和后端构建。

**原A08的市场访问、发布/安装编排、状态记录、恢复及这部分Encore迁移由乙队统一承接。** 安装表族的Store与迁移由乙队实现，甲队提供连接/方言/事务规则并评审；不要再批量搬动这些文件。甲队提供模型导入、设备绑定、文件等业务端口，不另建安装状态机。

A10现有 `service_plugins/service_access` 运行登记、设备配置、租户接入、表单及回调仍归甲队；乙队通过受控接口登记并保存引用。脚本仍归甲队。通知注册、租户grant、实例和投递由原notification owner维护；北向沿原catalog/worker；Broker权限由owner推导，不让安装器直接改业务表或接管业务重试。

乙队唯一维护 frontend、Horizon、正式installer、Agent、插件制品及ThingsVis/可视化适配。甲队只提供前端接口、菜单权限及兼容要求。Q02拆分：甲队交后端不可变制品、服务/资源清单、迁移runner与运行契约；乙队接入唯一正式部署流程。甲队不另建正式Compose、Agent或安装器；独立测试夹具不得充当另一套正式部署入口。

#### 三、保持同仓库串行，同时让两队推进

Horizon现有task-graph规定 `sameRepositoryParallelism:false`，契约变更先过任务01。保留这一约束，不能以独立worktree为由绕过。两负责人维护短小的仓库写入窗口：同仓库同一时段一个活动写入团队，队内仍遵守仓库及任务图规则，交接时给提交、文件、未完成点和验证结果；不等整批任务做完才交接。没有写入窗口时，继续其他仓库工作、只读评审或独立环境测试，不修改该仓库。源码、测试数据、构建输出和运行资源分别隔离。

#### 四、先共同冻结J1—J8，再按接口并行

将以下内容放进现有docs/contracts/tests，固定版本并附可执行样例，不新增契约平台。只阻塞确实依赖未定条款的小卡，其他工作继续。

| 契约 | 甲队必须提供 |
| --- | --- |
| J1 身份授权 | 可信用户/租户/服务/插件身份、作用域与吊销；插件业务身份不能调用安装管理。 |
| J2 准备/查询/启用/撤销 | 各业务owner的最小操作、稳定操作与子资源键、归属和结果查询；无需强造统一CRUD。 |
| J3 指纹和兼容版本 | 核心API、SDK、配置、schema支持范围和明确拒绝规则，供乙队锁包与选择制品。 |
| J4 持久状态与幂等 | 同意图同结果、异内容冲突，提交结果未知可按原键查询；不能换随机键重做。 |
| J5 代次与业务世代 | 分开定义Broker route epoch、设备删除/重建世代、binding revision；与乙队安装执行代次分别校验。 |
| J6 待激活与切换 | 未启用不投递真实任务，提供暂停、在途查询和绑定切换；与Agent实际隔离配合。 |
| J7 资源与迁移 | 统一连接配置、驱动和迁移调用规则；各schema保留明确账本，同一schema只有一个有效执行者。 |
| J8 超时恢复与卸载 | 已受理/unknown动作继续归原owner，查询状态明确；卸载不能删除用户原资源或重发动作。 |

J5/J6由两负责人亲自确认。共同operation_id用于关联，每个步骤另有稳定step/resource key和请求指纹。现有 `ServicePlugin.Create` 直接生成UUID，不足以保证登记回包丢失后的恢复；甲队须补唯一约束、原键查询和待启用能力，不能让乙队用盲重试或直写数据库补缺。已有通知幂等和模型导入记录直接复用。

#### 五、甲队立即开展的工作

先分派P0：T03修设备删除的归属、零行和Commit处理；T04修遥测fallback错配及乱序最新值；T05统一自动化owner和全部触发入口；T02.a修WS逐设备归属并冻结凭证规则。随后T01在隔离PG做字典样例，T02.b完成协议adapter。物理迁移唯一实现，不非法跨module导入internal，不复制service/DAL，不在Encore包Gin fallback；原生核心保持，流式接口可用raw/net/http。三接口样例不能代表完整表族切流。

同时推进R01/R02.a/b连接与迁移、S05.a跨库协议和S01时序契约，再按owner推进R03—R09、S02/S03/S04。普通CRUD保留GORM，既有SQL Store保留，方言限制在所属repository；不换ORM、不做万能SQL翻译器。生产多引擎须消除强制sqldb PG资源，保留逻辑资源及历史账本映射。

遥测历史走可替换时序入口；当前状态和业务回执保留关系库owner。冻结类型、时间、乱序、冲突、聚合、分页与删除语义。外部时序不假装共享关系事务，复用现有ledger/effects保证受理、投影和重放；删除世代、切换屏障及unknown状态必须可验证。跨关系引擎搬迁走R02.c维护窗口，历史搬迁走S05，不重发已受理副作用。

逐条收口Gin台账和发布调用链。每个PR只变一种维度。给普通开发者精确端点/表族/状态转移、可改文件、复用接口、前置提交和失败样例；架构、权限、事务及副作用由负责人先定。SDK按冻结版本给契约和兼容测试，不另起第二套业务协议。

#### 六、独立测试与交给乙队的制品

甲队自建隔离环境，真实验证 **PG+TimescaleDB、PG+TDengine、MySQL+TimescaleDB、MySQL+TDengine**。覆盖发布中的全部逻辑库、schema和worker；乙队新增安装表及迁移纳入最终矩阵，合入后重验相关门禁。MySQL+TDengine禁止PG可达，不能暗留插件安装库在PG。

**矩阵还必须覆盖看板运行依赖。** installer固定main的 `docker-compose.yml:177` 显示ThingsVis-server使用PG的 `ThingsPanel?schema=thingsvis` 并依赖postgres。R02/R02.c不得遗漏或删除共享数据库的thingsvis schema；甲队给通用迁移/搬迁契约，乙队负责ThingsVis及installer适配。运行源仓库已确认为ThingsPanel/thingsvis，master e22e42d7a0bcfa634d7b62f36dc901f177980913的Prisma schema固定postgresql；本地checkout尚未定位，thingsvis-internal-docs不能当运行实现。源分支不等于v1.0.23发行镜像，乙须核对并适配既有持久层/客户端生成/迁移，不能只改URL。带看板组合包的MySQL+TDengine必须验证该链；未适配时明确此能力不支持，不得宣称完整矩阵或暗留PG。Horizon远端目录自己的数据库不自动纳入客户平台驱动矩阵；一体私有部署另列依赖。

验证权限、幂等冲突、提交未知恢复、并发领取、乱序、历史读写/导出/清理、删除重放和退出排空。以受控插件fixture测试J2/J6，无真实设备动作或通知发送；fixture只证明甲队边界，最终仍需双方真实组件联调。数据库、Redis、MQTT、端口、卷、凭据与测试目录按环境隔离，清理只限本次资源。

尽早交固定PG参考制品和provider契约测试，再逐profile交新制品。每份附commit/digest、服务选择、配置/秘密引用、schema兼容、runner调用、health/readiness、退出和tick/续期要求；installer只接线，不复制核心SQL；乙自有表族和ThingsVis仍由各自owner实现迁移，保留所属工具与账本。提供接口变更、未完成缺口和消费样例，不让乙队追随内部目录变化。

共同首闭环保持设备连接器→模型导入→设备绑定→使用入口，通知只读校验可并行。两队独立通过后，以同一锁定制品验证回包丢失、重启、旧代次、失联、切换与恢复，再收口四组合。

#### 七、报告、授权和记忆收尾

开工交责任表、基线、J1—J8缺口及首批小卡；每次交付记录改动、实际命令/结果、未执行项、兼容影响、恢复办法和乙队所需输入。只把实质阻塞整理成供我转交的问题，不代发外部消息。明确“代码完成、独立测试、组合验收、发布”四种状态。本提示语不代表批准生产部署、发布、发送真实通知或控制设备。

结束前按项目规范做记忆收尾：长期意图走 `learn_evidence` 或当前受治理入口，验证结论走候选治理；只有返回 `stored` 且有 `memory_id` 才称入库。不得把临时SHA、未验证能力或测试秘密写成长期事实。

### 11.2 提示语二：乙队技术负责人

你是乙队技术负责人，带队完成 ThingsPanel 插件发布、下载、安装、自动配置、升级恢复及正式交付。你与甲队同时开工、分别测试。甲负责 Encore / 多数据库及平台业务核心；不要等甲全量去 Gin 或四种数据库组合完成。先在冻结的 PostgreSQL 基线与现有 Community / Encore 混合部署上推进，缺失接口用明确标注的契约 fixture 开发，正式启用前必须换成真实接口验收。

#### 一、先读依据并确认真实起点

先查询 SkillHub 共享记忆，优先 `memory_query`，缺失时使用当前 `mq`；再读各仓库 `AGENTS.md`、适用开发规范和相关源码。阅读随任务提供的 `ThingsPanel_Encore_Migration_Tasks_2026-11-01.md` 与 `/Volumes/500G/docs/ThingsPanel_插件发布与一键安装设计_2026-11-01.md`，后者第12节是故障验收依据；若不在同机，使用随任务提供的原文副本。已审计 backend main 为 `7f694779c979702740b15125164ebec2545604a4`；补充材料固定 installer main `98f9715`、frontend master `de13a7`、Horizon main `11f61a4`。实际开工重新固定提交与差异，不把 v1.3.0 发布版、main 和当前部署视为同一版本。

重点查 backend 的 `internal/service/market_native_install.go`、Horizon 的 `contracts/market-solution-bundle/v1/manifest.schema.json`、`docs/contract-change-workflow.md`、`docs/task-bundles/market-solution-bundle-v1/task-graph.json`，以及 installer 根 `docker-compose.yml`、`install.bash`。Encore 位于该 backend 仓库的 `encore-api/`；本任务没有提供其他远程 Encore 工作区位置，不得猜测。`pluginmgr`、Agent 的历史记忆不是现存实现证明，先查明仓库、职责和运行证据；已有则复用，缺失才补。

#### 二、你拥有的代码与不能接管的业务

乙队拥有原任务 **A08** 的现有市场安装编排及其 Encore 迁移、市场安装表族、社区前端、Horizon 市场与 canonical 包契约、ThingsVis 运行库适配及资源导入接线、唯一正式 installer、Agent、插件制品及其发布流程。A08 是原任务编号，现由乙执行。先扩展唯一实现，甲交付 T01/T02 模板后再由乙移动原实现；不能另外建一套 Encore 安装中心。

甲队拥有身份 / 租户 / 权限、设备 / Broker / MQTT、物模型、通知、自动化等业务核心，SDK 和业务协议，数据库连接 / 驱动 / 迁移 runner，backend / Encore 构建。**A10 的现有业务登记仍归甲**；乙只写安装调用适配，不能直接修改其业务表或复制核心。通知账号密钥留在既有 owner，安装记录只保存引用；设备实际控制仍走原 dispatcher。

你负责新增及既有安装表族的 PG / MySQL 实现和测试，消费甲的连接与迁移机制；ThingsVis保留其适用的Prisma工具和账本，按共同发布契约登记，不能强制重写成甲的Go数据层。不得因新建 `sqldb` 资源引入隐藏 PG，也不得在 installer 复制 SQL runner。先通过 PG 可以交付明确的 PG 阶段成果，不能宣布 MySQL 已支持。

**D0 定位 ThingsVis 的干净 checkout：远端是 `ThingsPanel/thingsvis`，默认 master，已核验提交 `e22e42d7a0bcfa634d7b62f36dc901f177980913`。** 本机路径未定位不代表仓库缺失。该提交 `apps/server/prisma/schema.prisma` 的 provider 固定为 postgresql；installer `98f9715` 的 ThingsVis-server 又固定 PostgreSQL URL、`schema=thingsvis` 和 postgres 依赖。因此乙须核对并适配现有持久层 / 查询 / migration 及正式配置，不能只换 URL。master 源码不是已发布镜像内部证据，仍须核对镜像实际提交与 schema。甲搬迁关系数据须保留 thingsvis schema，并与乙对齐数据、ID 和引用验证。

未完成时明确阻止或标记“含看板组合包暂不支持 MySQL+TDengine 无 PG”，保留待完成任务；禁用后绕过测试不能冒充最终验收。Horizon 公网市场自有数据库不自动跟随客户平台驱动选择；私有一体化部署单列 profile 和实测范围，不能暗带 PG 后宣称完整无 PG。

#### 三、分清三个安装职责

正式 installer 安装、升级平台及依赖，调用甲的迁移程序、接入 Agent、配置声明过的调度；保留原有安装入口和已支持平台行为。镜像由各源码仓库发布，installer 消费固定 digest，不临时编译另一个版本或新造第二套正式 Compose。

现有市场编排保存计划、版本、实例、步骤、资源归属和恢复状态，调用原模型导入 / 绑定流程。Agent 只管理归属明确的插件容器、目录、网络与有限操作，独立保持主机身份、执行代次和资源预留。即使二者都由乙维护，**普通插件安装不能触发平台、数据库、Encore 或 Agent 自升级**；平台升级必须进入独立授权的 installer 流程。

Agent 首期目标是 Linux amd64 / arm64 容器环境，每个档位实测后才承诺支持，不以此删除 installer 原平台兼容。无 Agent 的部署可登记为“外部管理”，不显示无法兑现的自动升级 / 卸载能力。

#### 四、最小共同契约冻结后立即分工

与甲冻结首个案例所需的精确 API、身份、错误、版本和失败样例，不要求先完成整套平台迁移。至少包含可信租户、实例身份、操作号、稳定步骤 / 资源幂等键、请求指纹、期望业务版本、created / reused 归属，以及登记成功但回包丢失后的查询方式。关联操作号不能代替每个子操作的唯一键；冲突载荷须拒绝，重试不能换随机身份。

甲提供原有业务 owner 的登记 / 查询 / 启用 / 撤销能力及 PG 测试制品。乙提供测试签名的受控插件成品、Agent和消费者契约测试，外部设备/厂商用模拟器。缺能力由甲补在原 owner，不能由乙直连数据库兜底。部署执行代次、Broker epoch、设备删除世代和模型 revision 分别处理，不混成一个 generation。

甲同时提供应用镜像、实际服务 / 资源、配置 / driver profile、迁移 runner 调用、schema 兼容、健康 / 退出、tick / 通知续期契约；乙负责正式部署接线。新增薄 host 必须进入实际构建与安装清单，不能只以本地启动成功验收。不要为插件启动 devicemetadata 的旧 MQTT，也不要把未发布 northbound 标成可用。

#### 五、沿用 canonical 资源契约

Horizon 负责市场身份和商品，平台负责本地租户与安装授权，二者不重建对方组织体系。资源包继续使用 Horizon Bundle v1；运行包说明与锁定文件只引用其固定 schema / 内容指纹，通知 manifest 继续沿原 HTTP v1 契约。新包规范只能有一个 canonical 定义位置。

不得私加平行绑定格式、改 `bindingKey` / `deviceBindings[]` 语义，或在可分发资源包塞入真实 tenantId、deviceId、userId、数据库 ID 和凭据。契约变化先走现有 CCP / Task 01、Schema / golden / valid / invalid fixture 和 CI 门禁，再改调用者；不能把包格式升级当成绕过旧资源契约的办法。

#### 六、并行规则与实施顺序

保持既有 `sameRepositoryParallelism:false`，不改规则。同仓库同一时段一个活动写入团队，队内遵守原任务图的串行与依赖；按短任务窗口交接提交、未完成项和契约版本；两个 worktree 不等于获准同仓并写。拿不到 backend 窗口，就推进 Horizon、Agent、installer、制品、前端 fixture 和独立测试，不等待甲整个项目结束。

先完成可信成品、有限 Agent、现有编排的恢复记录和当前 PG 安装接线。首个真实闭环必须是 **设备连接器＋原生模型导入＋设备绑定＋可见使用入口**。通知可并行做 manifest / validate 无发送验证，不能替代物模型优先事项。随后完善公开上架、离线包、更新卸载；WASM、Kubernetes 和任意第三方页面不是首期前置。

程序安装、配置完成、业务启用分别展示。待激活必须同时限制真实任务、业务授权与插件执行能力，不能只有数据库 inactive。缺现场信息只询问有权人员；权限 / 内容 / 主机改变须重新确认计划。安装和健康检查不得发送真实通知或控制设备。

#### 七、独立测试必须证明恢复和隔离

使用固定 PG 核心、假设备 / 假服务商及隔离 Agent 环境，不使用另一队可写库、卷、MQTT 身份或凭据。正式 Compose 有固定容器名和宿主端口，仅 `-p` 不足；优先独立 VM / 主机 / Docker daemon，并隔离绑定路径。若共享宿主，必须完整覆盖并验证端口、容器名、网络和卷，不能清理别的栈解冲突。测试编排只能用于测试。

至少完成插件文档第 12 节对应测试：同请求连续 / 并发重放 100 次仅一个实例与绑定；下载、创建、登记、启用前后进程中断可恢复；业务成功丢回包可查询而不重复创建；旧代次在执行副作用前被拒绝；两个实例争用端口 / 串口仅一个获准；待激活无真实外部写；旧主机离线且可能仍写时不自动启用替代实例。

验证签名 / 发布者 / 内容变化、过期目录、离线依赖缺失、租户越权、限权实际阻断、并发升级和用户外部修改。回滚必须覆盖原镜像指纹、配置版本、插件私有数据及业务绑定；只换镜像不算回滚。卸载只删除本次拥有且未被复用或修改的资源，不删实际设备 / 历史遥测。通知 unknown 和设备未知结果不得由安装器自动重发。

自己的模块测试、当前 PG 联合闭环、四组合最终验收分别记录。fixture 通过不等于真实 provider 通过；甲方言通过也不等于乙 installer 通过。新增组合须完成乙安装表、甲 runner、正式安装器及业务闭环，才扩展支持表。

#### 八、交付与操作约束

先检查分支与 dirty 文件，保留用户修改；不清空数据库 / 卷，不覆盖他人工作。可以自行构建、部署到本队独立测试环境并运行必要测试；未经明确授权，不操作生产或他人共享环境、不推送会触发自动部署的主分支、不发送真实通知、不控制真实设备。前端 master 有自动部署与完整类型检查 / 回归 / 构建门禁，不能拿它试跑。

交回责任表、精确任务卡、契约版本、提交 / 制品指纹、实际测试命令与结果、失败恢复、剩余临时适配和未验证项；不能把未执行写成通过。边界明确就推进，只有真实架构 / 契约缺口才报告具体阻塞。

结束前按现行记忆治理收尾：长期原始意图走 `learn_evidence` / 当前受治理入口，可迁移结论进入候选治理；只有返回 stored 且有 memory_id 才称正式入库。脱敏记录事实与待验证项，不上传凭据，不用旧记忆覆盖本次证据。

