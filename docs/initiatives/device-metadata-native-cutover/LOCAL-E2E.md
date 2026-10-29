# 本机真实 HTTP 端到端（2026-10-29）

范围声明（先读）：这是**完整 Encore 应用在本机真实运行 + 真实 Postgres + 真实 HTTP 请求**的验证，覆盖设备元数据后端接口。它**不是**“真实浏览器 + 真实 Community 运行时”的整块签收：没有浏览器，没有 Community，notification 服务被替换成空实现。`E2E-CHECKLIST` 里的“整块签收”仍是 0/27。

## 结果

`kernel/scripts/e2e-native-http.sh` 对运行中的应用发 40 项真实请求，**40/40 通过**。覆盖：

| 类别 | 验证了什么 |
|---|---|
| 认证与权限 | 无令牌 401；只读角色不能创建（403）、不能读凭据/模拟器/导出任务；另一租户读不到设备、分组（404） |
| 设备 | 创建（默认设备编号=设备 ID）、幂等重放返回同一设备、读取、列表搜索、`If-Match` 更新成功、旧 ETag 返回 412、改名读回 |
| 设备编号变更 | 提交后返回 202 和操作；没有运行时回执时操作保持 `pending` |
| 分组 | 创建、加成员（需 `expectedGroupVersion`）、成员列表、设备所属分组、非空分组删除返回 409 |
| 凭据 | 创建（密文落库）、列表直接显示明文、另一租户 404、只读角色 403、吊销返回 202 且操作保持 `pending` |
| 模拟器 | 首次保存配置（`If-Match: "sim-profile-0"`）、读回、只读角色 403；无接入配置时 `simulator-config` 返回 404 |
| 导出 | 24 小时内导出在没有 Community 时返回运行时不可用而非崩溃；超过 24 小时的长导出返回 202 `pending`，任务进度可查，只有发起人能查 |
| 后台任务 | 打开 `TP_BACKGROUND_WORKERS=on` 后，长导出任务在 15 秒内被拉起并变为 `running` |

## 没有覆盖

- 真实浏览器页面；真实 Community（读数、模拟发送、改编号下发、长导出的实际读取）；MQTT；notification 服务（被空实现替换）。
- 需要 Community 才能成功的路径：读数/历史/导出正文、投递器下发。

## 这次跑出来的真实问题（已修）

1. **`TP_AUTH_MODE=dev` 在 `encore run` 下被误拒绝。** 之前的环境闸门只认类型 `local`/`test`，而 `encore run` 实际报告类型 `development`、名称 `local`、云 `local`（Encore Cloud 的开发环境云是 aws/gcp）。已改为：云为 `local` 且类型为 `development` 也算本机；生产环境永远不算。见 `l1-access/authn/auth.go` 与其测试。
2. **定时任务在本机和自托管里不会自动运行。** Encore 的 `cron` 只在 Encore Cloud 触发。长导出 worker、过期文件清理、outbox 投递器因此在自托管里默认闲置。已加进程内调度：设置 `TP_BACKGROUND_WORKERS=on` 就会在服务进程里按间隔运行同样三个任务（长导出 15 秒、投递器 1 分钟、清理 1 小时），服务关闭时停止。**自托管部署必须设置这个变量**（Encore Cloud 部署可不设，cron 照常）。

## 如何复现

在 Linux 上不需要下面的“空实现”这一步；在 Mac 上因为 notification 服务只能在特定的 Linux 自托管配置下启动，需要一个**临时副本**：

```bash
# 1) 临时副本（不要提交）
git -C kernel worktree add --detach /tmp/rig HEAD
# 2) 仅在副本里，让 notification 初始化可被跳过：
#    编辑 /tmp/rig/encore-api/l3-core/notification/service_runtime.go，在 initService() 开头加
#      if os.Getenv("E2E_RIG_STUB_NOTIFICATION") == "1" { return &Service{}, nil }
# 3) 生成本机测试密钥（仅测试用）
KEY=$(python3 -c "import os,base64,json;print(base64.b64encode(json.dumps({'e2e-key-1':base64.b64encode(os.urandom(32)).decode()}).encode()).decode())")
# 4) 启动（需要 Docker 提供 Postgres）
cd /tmp/rig/encore-api && TP_AUTH_MODE=dev E2E_RIG_STUB_NOTIFICATION=1 TP_BACKGROUND_WORKERS=on \
  METADATA_CREDENTIAL_KEYRING_B64="$KEY" METADATA_CREDENTIAL_ACTIVE_KEY_REF=e2e-key-1 \
  encore run --browser=never --port 4100
# 5) 另一个终端
kernel/scripts/e2e-native-http.sh http://127.0.0.1:4100
```

脚本里的开发令牌格式是 `用户:租户UUID:角色`（如 `u1:<uuid>:TENANT_ADMIN`）。

## 对 Linux/CI 验收的建议

在真实 Linux 环境按同样脚本先跑一遍（不需要替换 notification），再接入真实 Community，执行 `E2E-CHECKLIST-2026-10-29.md` 里需要 Community 的项目：读数、历史、导出正文、改编号/删除/绑定子设备由投递器真实下发。
