# 本轮规划交付的验证记录

检查时间：2026-10-23T13:27:44.135120+00:00。**这是文档与工作区保护检查，不是产品验收报告。**

## 1. 实际执行的文档检查

|检查|结果|范围|
|---|---|---|
|OpenAPI YAML and internal references|通过|{"version": "3.2.1", "paths": 87, "schemas": 238}|
|Operation uniqueness, path, task and security mapping|通过|{"operations": 103}|
|Task IDs and required work-item fields|通过|{"tasks": 187, "allBlocking": true}|
|Dependency graph and full release-gate closure|通过|{"coveredByDMQA010": 187, "tasks": 187}|
|Task/API/schema/database/test-spec trace|通过|{"plannedScenarios": 574, "executedProductCases": 0}|
|CLI/API catalogue coverage|通过|{"cliOperations": 103, "apiOperations": 103, "missingCli": [], "missingApi": []}|
|Complete rendered task table|通过|{"rows": 187, "requiredColumns": 11}|
|Frontend inventory size|通过|{"inventoryRows": 139, "dispositionAudit": "separately reviewed 139/139 with exactly one classification"}|
|Required deliverables|通过|["TASKS.md", "DOMAIN-MODEL.md", "API-CONTRACT.md", "openapi.yaml", "DATABASE.md", "CLI.md", "TEST-PLAN.md", "DECISIONS.md", "EVIDENCE.md"]|
|Relative document links|通过|"all referenced deliverable files exist"|

唯一OpenAPI文件 SHA-256：`898d402f617030636791eb8f6ee678284bc59b7858e3a6857ea56b0ff57225ba`。

另由独立源码审阅核对前端139条处置、CLI 103项映射、数据库字段与数据owner。复核发现的退出码、identifier长度、模板包映射、曲线聚合、状态筛选、命令内容和诊断统计等不一致已经回写对应合同/任务；不是以数字覆盖率代替语义检查。

## 2. 没有执行的验收

- 没有运行 Encore/Community 应用、真实数据库约束/迁移、产品单元测试、浏览器业务操作/HAR、CLI业务命令、真实设备或部署。
- 没有运行完整 OpenAPI 3.2.1 validator、Go/TypeScript 客户端生成编译；本轮使用 YAML 解析及自定义结构/引用/追踪检查。这些不替代 DM-API-001/009 的工具链门禁。
- 574条任务测试场景和47条CLI测试ID均为可执行规格，状态为 planned_not_executed；没有把测试源码或历史报告计为本次通过。

## 3. 远程工作区保护和文档落盘

|仓库|基线既有文件|被修改/丢失|HEAD/分支|剔除本任务新目录后的Git状态|
|---|---:|---:|---|---|
|thingspanel-backend-community|1240|0|保持|与基线一致|
|iot-api-encore-kernel|339|0|保持|与基线一致|
|thingspanel-frontend-community|954|0|保持|与基线一致|

本任务仅新增 `docs/initiatives/device-metadata-native-cutover/` 中 19 个规划/追踪文件；远程内容与交付副本逐文件 SHA-256 核对通过。没有业务代码变化、Git提交、迁移或部署。

完整代码基线与证据限制见 [EVIDENCE.md](EVIDENCE.md)；正式产品通过条件见 [TEST-PLAN.md](TEST-PLAN.md)。
