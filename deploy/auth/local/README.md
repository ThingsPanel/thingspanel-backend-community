# 本地第三方认证联调

这是隔离验收环境，不是新的生产认证服务。生产继续使用唯一 Encore 应用。

## 已验证的组件

- Casdoor `v4.23.0`，镜像 digest `sha256:e13c9aada231f7338a03ab0ae7054d0a9480e77283338ab9fc319d88fe2b3ce0`。
- 浏览器 HTTPS 网关 `https://localhost:24444`；Casdoor `https://localhost:24443`。
- 实际 Vue 登录/回调/个人中心组件、实际 `authcore.HTTP` 和 `SQLAccounts`。
- PostgreSQL `notification_test` 中随机独立 schema，结束测试时删除；Redis 为进程内隔离 miniredis。
- Web 使用静态菜单；个人中心展示请求在本地网关映射到真实 SQL 用户详情。设备业务、个人资料修改不在本夹具验收范围，不能据此宣称完整生产部署通过。

## 复现

需要 Docker、Node、Python + cryptography、已有前端依赖。所有配置放仓库外新空目录。

1. `python deploy/auth/local/prepare.py PRIVATE_DIR` 生成本地配置、自签名证书和随机客户端密钥。
2. 启动 Casdoor：

   ```powershell
   docker run -d --name tp-auth-casdoor-local -p 127.0.0.1:24442:8000 -v "${fixtureDir}/app.conf:/conf/app.conf:ro" -v "${fixtureDir}/init_data.json:/init_data.json:ro" -v "${fixtureDir}/data:/data" casbin/casdoor:v4.23.0
   ```

3. 在后端根目录的父目录挂载整个社区仓库（测试会读取社区 SQL）后运行 `encore-api` 包测试：

   ```powershell
   docker run --rm --name tp-auth-oidc-fixture -p 127.0.0.1:24445:24445 -v "${backendDir}:/backend" -v "${fixtureDir}:/run/oidc:ro" -w /backend/encore-api --env-file "$fixtureDir/test.env" golang:1.24-alpine go test -count=1 -v -timeout 95m ./internal/authcore -run '^TestOIDCLiveHTTPFixture$'
   ```

   `test.env` 私密文件：TP_AUTH_OIDC_LIVE=1；TP_AUTH_TEST_DSN 指向专用 `notification_test` PostgreSQL；TP_AUTH_OIDC_CONFIG_FILE=/run/oidc/provider.private.json；TP_AUTH_OIDC_CA_FILE=/run/oidc/cert.pem。可复用 Go 模块/构建缓存。不得使用生产 DSN。Windows Docker 中主机数据库用 host.docker.internal。

4. 前端目录：设置进程环境 VITE_AUTH_ROUTE_MODE=static、VITE_HTTP_PROXY=Y，运行 `pnpm exec vite --host 127.0.0.1 --port 5003 --strictPort`。
5. `node deploy/auth/local/tls-proxy.mjs PRIVATE_DIR`。本地已有社区服务 9999 仅用于公开 logo/功能开关/版本显示。
6. 浏览器打开 `https://localhost:24444/login?redirect=/personal-center`。仅对该本地测试证书继续访问；没有关闭后端证书检查，也没有安装全局信任根。

测试身份（仅此隔离环境）：Casdoor `tester` / `Local-test-2026!`；平台 `local@example.invalid` / `Local-platform-2026!`。初始无绑定。先第三方登录，再验证平台密码绑定，再退出/再次第三方登录。个人中心可验证错误密码拒绝解绑、正确密码解绑、密码重新登录与重新绑定。

结束时调用 `http://127.0.0.1:24445/_qa/complete`，等待测试 PASS，确认随机 schema 清理；停止本次启动的 TLS 网关、Vite 和 Casdoor 容器。测试服务最多等待 90 分钟。保留私密目录用于排错时不要提交到 Git。

## 验收边界

Casdoor 本地真实授权码及签名校验通过，不等于客户企业 IdP 已验收。LDAP/AD、SAML 应由身份服务接入并转换成 OIDC；本次没有真实目录或 SAML 来源，不宣称这两段上游链路已通过。微信网站的适配器已有模拟协议/浏览器事务测试；真实扫码仍需要网站应用资质、AppID/Secret 和回调域名。小程序 AppID 不可替代网站应用。

官方参考：https://casdoor.ai/docs/deployment/docker/ 、https://casdoor.ai/docs/deployment/data-initialization/ 。本地仅启用授权码流程，不将默认应用配置或公开测试密码用于生产。
