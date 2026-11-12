# 受管 HTTP 协议插件

20261112 用户确认；复用现有插件安装与身份机制。

HTTP 仍是 service_type=1 的协议插件。平台安装 ID 固定为 http-adapter，协议标识为 HTTP。共享身份只能访问相同 generation、desired=enabled、state=installing/enabled 的该安装记录所委托的有效 HTTP 直连设备；设备配置与设备必须属于同一租户，且不能属于服务接入点。租户身份继续执行原有租户限制，不因空租户自动放宽权限。

设备列表与单设备查询使用相同过滤条件，HTTP 身份不读取服务接入点凭据。受管记录产生 generation 后，HTTP 的无身份兼容接口停止接受请求，停用、失败、卸载不重新开放匿名访问；其他旧协议保持兼容。动态注册暂不开放，设备通过平台模板创建。

安装和撤销沿 internal/plugininstall、internal/pluginidentity。HTTP 权限入口 internal/api/plugin_http_scope.go；回归 TestSharedHTTPInstallationScope 与 TestLegacyPluginCompatibilityAndHAFencing。

主线测试服仍用 device-safety 组合源码。部署必须在当前已验收的组合源码上应用本改动，保留 RELEASE_PROFILE 的数据库及设备所有者信息，不能把本分支完整源码直接部署为该测试服后端。
