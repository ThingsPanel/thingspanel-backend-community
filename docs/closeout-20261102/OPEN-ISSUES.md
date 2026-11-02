# 真实剩余问题与解除条件

更新：2026-11-02 09:20 UTC。以下缺口仍属于原35包/100卡和插件本期范围；不因局部测试通过转为“不支持”。严重缺口先保留安全守卫，避免假完成。

## instance-fencing

负责人甲（业务权限/持久围栏）与乙（Agent执行）。G318～G322旧插件入口处于无权限分组；现SDK执行控制尚未接到全部真实回调。现Broker route epoch不是connector实例generation，不能借切cell机制冒充同cell暂停。当前Agent只创建未启动容器，active=false；没有可信激活服务或实际A08动作链。解除条件：可信当前身份+绑定+实例/绑定/授权代次接收端围栏，旧写者退出/拒绝；真实上报与下行在暂停、升级、失联、重启、未激活时反例均通过。影响J-G0～J-G4。

## installation

负责人乙A08，甲提供业务provider。PG原键/摘要/写入回执/权限已局部实测；补偿、重试、绑定恢复仍以安全拒绝为主。必须将操作/步骤持久状态机连接真实worker、Agent和平台接口，核对成功/失败/未知/人工处理；资源只能按本操作所有权回收，处理复用/后来修改/并发删除。新增MySQL同一store候选尚未真库验证；PG JSONB指数数字摘要漂移是待真库复现的审查风险，不先改旧协议。影响J-G0～J-G4。

## multi-db

负责人甲，乙维护A08/Horizon/ThingsVis各自owner。主平台各实际连接和SQL仍有PG绑定；ThingsVis单模块双库通过不代表平台MySQL。统一schema锁但每owner自有账本；不改已执行PG历史迁移，不合并所有账本。A08MySQL runner候选复用 schema-level GET_LOCK，待真实并发/DDL中断/漂移验证。解除条件：所有启用owner在四profile完整产品链中运行，MySQL+TDengine在PG不可用条件通过。影响J-G3/J-G4。

## time-series

负责人甲。普通PG当前/历史/业务回执同事务已验证；历史统一入口、TDengine实际驱动、Timescale实际profile及跨库意图/删除代次/切换/维护搬迁仍未闭环。当前快照、属性、回执继续由既定关系owner管理。解除条件：历史写读统计导出清理全部实际入口切换，故障/未知可查询，跨库删除/切换与当前状态正确衔接。影响J-G1～J-G4。

## release

负责人总负责人单一安排修改，甲提供镜像/就绪/生命周期，乙接正式installer。现Encore image workflow选服务未包含marketinstall/northbound；T01 native host尚未发布。正式installer配置原子恢复已测，但全部来源/签名/可信目录有效期撤回/依赖锁的产品链未验证。不能直接执行刚下载的prepare.py充当可信预检。承诺Windows路径、迁移中断恢复、真实平台/Agent/插件分别升级、可兼容/不可兼容数据恢复与搬迁未验收。影响全部共同门禁。

## market-ui-and-object-policy

负责人乙Horizon/前端。Horizon版本审批原漏洞已真实红绿验证，但正式MinIO初始化仍设置匿名整桶下载，可能绕过应用授权；需私有桶默认、失败阻断/readiness核对和真实匿名正反例。Frontend完整构建失败1241类型诊断；原基线1230，差异为1217同/13移除/24新增，正在修公共request循环推断和本轮组件错误。不得用仅SFC编译/文本测试宣称完整构建成功。影响J-G1/J-G2/J-G4。

## route-completion

负责人甲（A08乙），总负责人保留357行台账。多数旧Gin路由尚无已核实的原生/消费者/批准退役/静态/关闭条件去向和实际release回归。T01先完成单一源码字典样例，再按相同owner模式分发剩余小卡。解除条件：100卡逐路由指向同一业务用例和已发布服务，客户端兼容及正常/失败用例通过；不通过恢复已批准退役入口制造完成率。影响J-G0/J-G1/J-G4。
