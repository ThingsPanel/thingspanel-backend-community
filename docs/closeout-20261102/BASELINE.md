# 统一基线与接管清单

采集时间（UTC）：2026-11-02T06:00:17.122503+00:00

总负责人维护本文件及共同门禁；保全提交仅表示完整接收，不表示通过验收。原目录未 reset/clean/stash/覆盖。新的开发根目录：`/Volumes/500G/code/thingspanel-takeover-20261102`。

## 实际仓库与处置

| 来源/准确目录 | 来源 HEAD | 修改/未跟踪文件 | 保全/集成提交 | 处理结果 |
| --- | --- | ---: | --- | --- |
| `/Volumes/500G/code/thingspanel-closeout-20261102/backend` | `f22c2bdf556a49c5f322773df9061278f09e5919` | 18/29 | `f89fce5810478b5f046b69bc643b1cab9553db07` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Volumes/500G/code/thingspanel-closeout-20261102/device-connector-sdk` | `f183a19cbc51b6c4cb1bba4119a9105098c0fed7` | 0/0 | `f183a19cbc51b6c4cb1bba4119a9105098c0fed7` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Volumes/500G/code/thingspanel-closeout-20261102/frontend` | `57e80b7246166ccaec9860de1a703860cc452983` | 0/0 | `57e80b7246166ccaec9860de1a703860cc452983` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Volumes/500G/code/thingspanel-closeout-20261102/horizon` | `de388bdfcc219ce27d593636b7ae77a09b3b6eb4` | 6/10 | `ae611069eaf4c6e8e61c121d54e693a521c124ea` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Volumes/500G/code/thingspanel-closeout-20261102/installer` | `22d970e4b46d286a9f03a6f0a50a370725732160` | 0/0 | `22d970e4b46d286a9f03a6f0a50a370725732160` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Volumes/500G/code/thingspanel-closeout-20261102/thingsvis` | `aac4fee52a8684d4aad50712be29ab5087901237` | 7/5 | `339279034ca3878227d7aef9a5207fb97c07404c` | 已接收至独立集成分支；待独立测试及共同验收 |
| `/Users/junhong/.codex/worktrees/team-a-access-auth/thingspanel-backend-community` | `missing` | -/- | `—` | 原目录不存在；使用前轮 source-backups 与已保全提交核对，不猜测恢复 |
| `/Users/junhong/.codex/worktrees/team-a-t03-device-delete/thingspanel-backend-community` | `0b19b8d2935ceb5deab79cc6883141df1338b86a` | 6/5 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-t04/thingspanel-backend-community` | `313260442c844502e080db3867d8d23f745a3e76` | 0/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-automation-mode-20261102/thingspanel-backend-community` | `8c8796065b08e1536bc0e53bd220d89d0e6cd200` | 3/4 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-parent-create-fence-20261102/thingspanel-backend-community` | `0b19b8d2935ceb5deab79cc6883141df1338b86a` | 2/1 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-telemetry-read-guard-20261102/thingspanel-backend-community` | `10e3f13f29757e14a415672d2ae8819f2a139109` | 0/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-template-owner-20261102/thingspanel-backend-community` | `79683d0d004f1338ebfbd46250fdae2720b814c0` | 1/2 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-a-ws-device-auth-20261102/thingspanel-backend-community` | `e2b63e7358aa29534b1b27f8387d71f12057405e` | 3/4 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/team-b-compensation-guard/thingspanel-backend-community` | `dc2bf9d899717b8b29b505a6cd16c500b6e4bf0c` | 2/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Volumes/500G/code/team-b-installer-20261102` | `282eb7c38cb5200d21171094a262d5db6f371851` | 8/5 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Volumes/500G/code/team-b-thingsvis-mysql-20261102` | `e22e42d7a0bcfa634d7b62f36dc901f177980913` | 1/45 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Volumes/500G/code/team-b-frontend-publish-20261102` | `012f52cce92ca66c34d29fd4dbb8089e243300e3` | 13/8 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Volumes/500G/code/thingspanel-horizon` | `7ce55781ef8a8dd3a074907c213c0c131b386ce6` | 7/10 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Volumes/500G/code/device-connector-sdk-team-a` | `f183a19cbc51b6c4cb1bba4119a9105098c0fed7` | 0/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/Downloads/code/thingspanel-backend-community` | `0e3ba8281f09c815dabcd03c634d17af845e950b` | 43/563 | `—` | 历史成果完整保全；不自动整体合入 |
| `/Users/junhong/.codex/worktrees/kernel-results-integration-20261101/backend` | `b86bd68a4bf27a2b1525f7827201a6b4b0cdb3cd` | 0/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |
| `/Users/junhong/.codex/worktrees/community-cli-completion-20261101/backend` | `684e91985dfc7d1959613ff849734454b7a2fd74` | 0/0 | `—` | 保留原目录与补丁；待逐项核对测试/等价合入 |

所有文件级 SHA256、mode、删除状态、patch/archive 摘要、精确文件名见 `baseline.json`；源保全目录：`/Volumes/500G/thingspanel-takeover-20261102-140017`。采集前后 status 与内容均无漂移。

## 集成写入窗口

| 仓库 | 唯一活动写入者 | 范围 |
| --- | --- | --- |
| backend | 总负责人建立台账后交甲负责人 | 核心安全修复及已有集成测试；A08 后续短窗口交乙 |
| installer | 乙 installer 负责人 | C01 与 Agent；含构建配置单一维护 |
| horizon | 乙 Horizon 负责人 | C09 与 canonical 合同 |
| thingsvis | 总负责人代乙 | 恢复缺失源码、C07 双 provider 与真实构建 |
| frontend | 暂无写入者 | 完成服务端后按窗口接线 |
| SDK | 暂无写入者 | J6 接线单独窗口 |

## 已有测试的版本和范围

| 证据 | 对应版本/限制 | 处置 |
| --- | --- | --- |
| T04 final-run.txt | 313260442c844502e080db3867d8d23f745a3e76；22 顶层 PASS；缺完整命令/DB版本/当时源码摘要 | 保留；集成版补测 |
| SDK 原6项 race | f183a19cbc51b6c4cb1bba4119a9105098c0fed7；单进程 gate | 保留；不抵跨进程真实接线 |
| prior backend-stage1/stage2 | 来源 closeout；精确文件hash见前轮 backend-sync-state；非本轮重跑 | 保留；当前集成版补测 |
| prior backend-stage3-device-and-provider | 日志末尾 FAIL | 退回失败项修复；不标整体通过 |
| prior encore-coordinator-confirm-recovery | 查询原键/并发/提交未知定向 PASS | 保留；真实产品链待验 |
| prior encore-stage3-native-provider-and-tick | 有 PASS 与 SKIP | 按用例记账；补环境后重跑 skip |
| prior installer-macos-stage1 + correction | 28项通过；后续21项修正通过；非真实Linux/Windows安装 | 保留；独立重跑14下载边界和恢复 |
| prior horizon-publication-stage1 | 发布定向通过，但repo含失败 | 退回失败根因并在新版本复验 |
| ThingsVis PG14/MySQL8.4 | 旧改装候选镜像局部真库证据 | 保留；不抵当前完整源码构建 |

前轮日志根目录：`/Volumes/500G/thingspanel-closeout-work-20261102/test-results`。同目录 sync-state 只是源码指纹线索；缺少每次完整命令/退出码/制品关联时不得扩张结论。

## 共同门禁（本轮起点）

| 门禁 | 状态 | 缺口 |
| --- | --- | --- |
| J-G0 | 未通过 | J1-J8 尚未用同版本provider/consumer实测锁定 |
| J-G1 | 未通过 | 固定真实PG产品链未执行 |
| J-G2 | 未通过 | 真实组合故障未执行 |
| J-G3 | 未通过 | 四profile全链、无PG证明未执行 |
| J-G4 | 未通过 | 正式installer全栈新装/升级/搬迁恢复未执行 |

## 本轮发现

- 第4份之后已有六仓 closeout 成果，必须以本次准确版本接管。
- access-auth 原路径已缺失；前轮保全记录仍在，不擅自认定成果丢失。
- ThingsVis 新 Dockerfile/package.json 引用 database.mjs、email-identity.mjs、auth-secret.mjs，但当前源目录缺文件；运行中的候选镜像不能代替源码。
- 现场有既有业务栈和上轮测试容器；本轮另用 tp-takeover-a/b/i 前缀、独立网络/卷/数据库及随机loopback端口。
