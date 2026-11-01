# 重启后继续：凭据与空分组删除实际联调

用户已明确要求继续，由root统筹、Luna执行；不重跑已通过的设备列表、普通新增、标签及自动化四Tab。暂停快照保持历史记录，以本次增量证据更新完成状态。

## 本轮顺序

1. 冻结源与环境核对：backend479归档、manifest及普通clone一致；frontend80逐文件一致。旧API镜像仍为466，不作为新端点验收证据。
2. owned隔离PG恢复并核对缺失迁移，只追加原SQL0030/0031；实际对象完整性、安全摘要和执行限制由backend Luna记录。本轮没有生产升级声明。一次479构建后只验新增凭据及group DELETE/move/member链路。
3. 新API ready后，frontend Luna实际验证凭据领取/摘要与group空删/冲突/移动/成员移除；新缺陷最小修复，最终更新任务书。

## 两位Luna分工

- `luna_resume_backend_acceptance`：唯一Encore build/test执行者；恢复本任务owned PG/app/stub/net并完成真实HTTP，源码变更须先由root判断。
- `luna_resume_ui_acceptance`：唯一浏览器控制者；新API ready前仅核对源码/准备与必要修复。
- root：设计/缺陷判断、冻结制品审阅、验证证据和任务书更新。

## 发现并授权的最小修复

分组列表原来只留key但重试前重新GET ETag；第一次删除实际成功而响应丢失时，第二次GET404会挡住原receipt。分组详情更会每次换新key。统一冻结{id,etag,key}的删除intent，未知后直接用原请求重试；成功删除与后续列表读取分别处理。只改这两处调用及必要state helper/test，不改后端479或公开合同。front80历史归档不覆盖，新制品使用独立delete-retry名字。

## 证据边界

本轮仅本地隔离runtime、synthetic登录，pending凭据不等于已接入；旧groups迁移、凭据激活/轮换/reveal、物理MQTT及真实通知仍不在已完成声明中。安全日志与摘要转存共享evidence/resume-20261027；private env/key/token/秘密响应不进入证据或Git。

## 审查新增的会话边界

凭据退出/切账号同步清理秘密、导入内容和pending请求，旧会话晚到回包不能再次显示；同账号token续期不清原请求。分组删除使用同样身份边界，旧代次GET/DELETE回包不能创建/清理新账号的intent或解锁新请求。Root对凭据flow定向3项通过；Luna对删除state最终6项通过；不重跑暂停前25项整轮。前端新freeze为82文件v2（原80与v1保留），root已逐文件和归档核对0差异。

构建初次发现旧测试infra配置声明不完整，已授权仅本任务owned PG四个独立空测试DB及不可用本地bucket配置以满足资源声明，不映射至用户数据库、不mock存储成功。该未使用bucket与四个无关DB不在本轮通过声明里。业务源479未变。

## 本轮收尾结果

本地凭据创建/摘要/分页/未知响应恢复，以及分组空删/冲突/移动/成员加入移除与CLI删除已完成，证据已核验并同步TASKS/tasks.json/FUNCTION-STATUS/EVIDENCE。后端保持479冻结，前端82-v3最终冻结；业务UI继承v2未改路径，v3身份别名边界局部4测试。环境API55067/Vite5114/PG保留，55068故障注入已停止，无需重搭旧环境或重跑旧25测试。

当前明确限制：真实logout由synthetic fixture404阻挡；设备详情某附属资源仍404，设备保留由列表读回证明；凭据仍pending，无实际接入/轮换/reveal声明。工具诊断暴露的一条合成token已清屏并撤销，产品证据不含原材料。

下一最小候选为模板扩展信息：当前Tab是pending，旧extend-info无调用者且写legacy additional_info。应直接使用native当前customFieldDefinitions与CAS保存，保留其它configuration；后台须先增加已关联设备值的兼容检查与模板/设备写入同步，避免删字段/改类型后旧值无法编辑。不得以隐藏删除功能、新增模板版本/草稿或通用preview作业代替原有功能。尚未授权子agent写该模块代码，先由root决定最小保护方案。

自动接续状态：get_goal仍返回paused；目标更新工具不支持resume，读取Codex自身UI又被自动安全检查拒绝，未绕过限制。已向用户请求恢复长期目标运行状态；这不改变本轮已完成的联调证据，也不表示原27块全部完成。

## 用户明确继续模板扩展信息

用户表示不理解长期目标恢复入口，已明确由root直接安排Luna完成模板扩展信息原功能，无版本/草稿、不重复已过测试。后续以该直接授权推进，不再把内部goal状态控制问题交给用户。

Root已冻结最小PATCH字段增量合同customFieldChanges；backend Luna负责原子合并/关联设备值保护与定向测试，frontend Luna负责原Tab接线与原CRUD/启停、稳定重试；root负责合同/types/技术决策与最终验收。既有479运行环境继续保留，待新后端制品准备后才验新字段接口。


### 模板扩展信息 root 复核边界

本轮仅DM-TPL-019及DM-FE-025中的模板定义切片：当前配置直接保存，不含模板版本/草稿；设备信息表单仍待后续。兼容保护覆盖完整配置与字段增量两种写路，缺必填/未知枚举属性沿原严格schema拒绝。复用55067/5114/owned PG，不重跑已通过凭据/分组/列表或旧整套测试。root复核已要求修正Modal内未知请求可重试、数字与枚举null可编辑；待最终HTTP/UI证据后更新状态。


### 模板扩展信息最终接续点（本轮已收尾）

本轮三步已完成：字段差量保存/兼容保护、原扩展信息Tab接线、本地HTTP与浏览器验收。最新源：backend480（见本轮manifest）+frontend85-v2（manifest e9ce86fdf3e4960dff880a07ba49a1eda577bf23e02803ff0c3424fb093478fa，tar280cdabbb0b17ca3cfe2d9a3ed11b3054079825b9d5a8b9a850d0d7cc17e8169）。运行API55067 image cc422dadc7014c26e4af8c8880b6afb149f19cc8504795baf85e17d4bea3e3ad，Vite5114保留，55068无监听；旧app-before-custom-field保留未删除。

本轮HTTP14请求17检查通过，前端8项局部测试及helper tsc/SFC/target lint通过；实际UI定义CRUD/nullable默认值/已有值409/Modal原请求恢复/刷新读回通过。刷新时canonical URL保留id，模板GET200且DOM正常；logo/sys_version/fav独立404是隔离fixture缺项，不扩查。详情见EVIDENCE与template-extend/template-ui-acceptance-v2.json。不要重跑这些已通过路径，除非后续改动或新失败直接影响。

本轮owned测试session已轮换，私有bootstrap文件原路径已更新；不得用旧tool trace中的会话。old四token未逐个取得401，记录只证明active映射替换及新会话200/未登记合法签名会话401。成功PATCH所需owned role12个SET列权限已补，无table ALL/tenant_id/deleted_at，无schema迁移。

下一项建议：设备“信息”Tab的customFields表单（DM-DEV-025/DM-FE-025剩余切片），复用当前模板字段定义/已存在设备PATCH与并发规则。模板版本、草稿、发布仍明确排除。真实IDP、升级/迁移和runtime整体交付仍按原任务后续，不因本切片通过自动标记完成。无需用户查找内部目标恢复入口。


### 用户授权继续设备“信息”Tab（2026-10-27）

root负责范围/合同与验收；Luna执行设备信息state/UI和新专用client测试，保留原位置/地图及String/Number/Boolean/Enum扩展资料。复用当前模板字段定义和既有设备GET/PATCH；后端480、OAS与通用HTTP不变。仅前端专用资料客户端保留原JSON scalar token，避免customFields整体替换时改坏未动过的高精度/停用历史值。仅提交变化对象，不自动将未赋值字段默认值落库；原payload/ETag/key保留未知结果，不造版本/草稿/新产品功能。

本轮步骤：1核对原功能与剩余标签（已完成，只读无旧套件重跑）；2设备信息页面接线及局部验证（进行中）；3独立fixture实际UI与既有API读回（待做）。用户关注的两个菜单共21详情Tab：起始5个主要本地流程已验、7部分、9待接，16个仍需收尾；两个列表入口还存在各自余项。不将“组件存在”算原生接完，也不把真实身份/运行owner等整体验收与本地操作混成一个数字。


### 设备信息最终接续点（本轮已完成）

本轮三步结束：范围/21Tab盘点、设备信息原功能接线、局部与实际UI验收。最新frontend90-v2（manifest00e15f2e287a25d8cc8809ef8ce26dfb7cb0371df47a6e2daec39e180e797a18/tar8ab37f479c36b5b7c6ae07c1bb468e7588df82c95030007e78b9acccd30247a2），backend480/image不变，无OAS/HTTP/表/迁移变更。位置/按需地图入口、模板字段值、缺省/精度/停用历史、412不覆盖、unknown原请求恢复、(0,0)/清位置/读回已通过本地验证；新位置校验只用于新信息页。

证据统一evidence/resume-20261027/device-info/，最终入口root-final-acceptance.json，UI结果device-info-ui-acceptance-v2.json；读取旧final-readback的Note/mode false须同时看root review与device-info-note-status-safe.json（旧期望过时，最后Note已匹配，version5）。不要重跑已过template/credential/group/列表或本轮信息路径，除非后续修改直接影响。

API55067/Vite5114保留，apiBase恢复55067，55068停、gate false；两台INFO fixture仍inactive留作读回证据，不动业务数据。原scratch使用device-management-native-20261027，metadata同名近似目录不是它，勿误报目录删除。

当前两个菜单共21详情标签：6个主要本地流程已验、7部分、8待接，共15个未收尾；2个列表入口还有各自剩余。不代表整版发布。建议下一项优先模板“协议配置”（当前只读，原Topic编辑等未接），其次“数据处理”；设备runtime类标签按正式owner接口集中处理。范围沿原任务，不新增版本/草稿/预览框架。
