# 2026-10-26 社区版边界修订：静态校验

- 规划交叉校验：通过；错误数 0；告警提示 1（历史基线路径未提供，不代表业务验证失败）。
- 188个稳定任务编号，580条测试规格；108个OpenAPI HTTP操作（103用户/CLI、5内部）；4个P2后置/范围排除，184个当前阻断任务包含于QA010依赖闭包。
- 本轮仅文档静态检查；没有运行产品E2E、实际迁移/部署；用户对1.3模板→模型→设备正常工作的确认不代替目标v2验收。
- 校验执行：python3 validate-plan.py . --expected-tasks 188 --expected-operations 108 --expected-cases 580

## 2026-10-26 增量：完整OpenAPI校验

- 固定独立校验模块 `tools/openapi-validator/`：`libopenapi-validator v0.16.1`、`libopenapi v0.41.3`。
- 执行 `cd tools/openapi-validator && go run . ../../openapi.yaml`：完整OAS文档校验通过。
- 默认CLI会将有效的数组递归树schema误判为无限循环；固定工具只忽略数组递归检测，继续执行其他引用与OAS检查。
- Go/TypeScript客户端生成与编译、媒体类型分支解码仍未验收。OAS校验通过不代表DM-API-001完成。
