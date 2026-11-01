package main

// notificationCommands exposes the notification business API already used by
// the UI. Keep this list separate from the automation commands so the main
// command table stays readable.
func notificationCommands() []*command {
	file := flagSpec{name: "file", kind: "string", help: "请求体 JSON 文件，- 表示标准输入"}
	pageFlags := []flagSpec{{name: "page", kind: "int", def: "1"}, {name: "pageSize", kind: "int", def: "20"}}
	queryBuild := func(names ...string) func(*invocation) (any, map[string][]string, error) {
		return func(in *invocation) (any, map[string][]string, error) { return nil, queryOf(in, names...), nil }
	}
	fileBuild := func(*invocation) (any, map[string][]string, error) { return nil, nil, nil }
	jsonBody := func(in *invocation) (any, map[string][]string, error) {
		body, err := in.jsonFile("file")
		return body, nil, err
	}
	return []*command{
		{name: "notifications.list", method: "GET", path: "/api/v2/notifications", summary: "列出租户通知请求（包含受阻请求）", flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "sourceType", kind: "string"}, flagSpec{name: "sourceId", kind: "string"}, flagSpec{name: "intakeStatus", kind: "string"}, flagSpec{name: "from", kind: "string"}, flagSpec{name: "to", kind: "string"}), build: queryBuild("page", "pageSize", "sourceType", "sourceId", "intakeStatus", "from", "to")},
		{name: "notifications.get", method: "GET", path: "/api/v2/notifications/:id", args: []string{"id"}, summary: "读取通知请求及当前状态"},
		{name: "notifications.send", method: "POST", path: "/api/v2/notifications", sideEffect: true, headerOnlyKey: true, summary: "提交通知（202 表示已接收，不代表已送达）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.deliveries", method: "GET", path: "/api/v2/notification-deliveries", summary: "查询通知投递记录", flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "instanceId", kind: "string"}, flagSpec{name: "sourceType", kind: "string"}, flagSpec{name: "sourceId", kind: "string"}, flagSpec{name: "dispatchStatus", kind: "string"}, flagSpec{name: "deliveryStatus", kind: "string"}, flagSpec{name: "from", kind: "string"}, flagSpec{name: "to", kind: "string"}), build: queryBuild("page", "pageSize", "instanceId", "sourceType", "sourceId", "dispatchStatus", "deliveryStatus", "from", "to")},
		{name: "notifications.delivery", method: "GET", path: "/api/v2/notification-deliveries/:id", args: []string{"id"}, summary: "读取单条投递记录"},
		{name: "notifications.plugins", method: "GET", path: "/api/v2/notification-plugins", summary: "列出可用通知插件"},
		{name: "notifications.plugin", method: "GET", path: "/api/v2/notification-plugins/:id", args: []string{"id"}, summary: "读取通知插件详情"},
		{name: "notifications.instances", method: "GET", path: "/api/v2/notification-instances", summary: "列出租户通知实例", flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "pluginId", kind: "string"}, flagSpec{name: "channel", kind: "string"}), build: queryBuild("page", "pageSize", "pluginId", "channel")},
		{name: "notifications.instance", method: "GET", path: "/api/v2/notification-instances/:id", args: []string{"id"}, summary: "读取通知实例"},
		{name: "notifications.tenant-plugins", method: "GET", path: "/api/v2/platform/notification-tenants/:tenantId/notification-plugins", args: []string{"tenantId"}, summary: "读取指定租户可用插件（SYS_ADMIN）"},
		{name: "notifications.tenant-instances", method: "GET", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances", args: []string{"tenantId"}, flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "pluginId", kind: "string"}, flagSpec{name: "channel", kind: "string"}), build: queryBuild("page", "pageSize", "pluginId", "channel"), summary: "列出指定租户通知实例（SYS_ADMIN）"},
		{name: "notifications.tenant-instance", method: "GET", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id", args: []string{"tenantId", "id"}, summary: "读取指定租户通知实例（SYS_ADMIN）"},
		{name: "notifications.test", method: "POST", path: "/api/v2/notification-instances/:id/test", args: []string{"id"}, sideEffect: true, headerOnlyKey: true, summary: "发送通知实例测试消息", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.groups", method: "GET", path: "/api/v2/notification-groups", summary: "列出租户通知组", flags: append([]flagSpec{}, pageFlags...), build: queryBuild("page", "pageSize")},
		{name: "notifications.group", method: "GET", path: "/api/v2/notification-groups/:id", args: []string{"id"}, summary: "读取通知组及修订"},
		{name: "notifications.group-create", method: "POST", path: "/api/v2/notification-groups", sideEffect: true, headerOnlyKey: true, summary: "创建通知组", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.group-update", method: "PUT", path: "/api/v2/notification-groups/:id", args: []string{"id"}, sideEffect: true, headerOnlyKey: true, summary: "更新通知组（需 expectedVersion）", flags: []flagSpec{file}, build: jsonBody},
		// Validation has no side effect and deliberately does not accept an
		// idempotency key. The service contract guarantees it never sends.
		{name: "notifications.instance-validate", method: "POST", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances/validate", args: []string{"tenantId"}, summary: "验证通知实例配置（不会发送消息）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.instance-create", method: "POST", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances", args: []string{"tenantId"}, sideEffect: true, headerOnlyKey: true, summary: "创建通知实例（SYS_ADMIN）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.instance-update", method: "PUT", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id", args: []string{"tenantId", "id"}, sideEffect: true, headerOnlyKey: true, summary: "更新通知实例（需 expectedVersion）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.plugin-register", method: "POST", path: "/api/v2/notification-plugins", sideEffect: true, headerOnlyKey: true, summary: "注册全局通知插件（SYS_ADMIN）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.plugin-update", method: "PUT", path: "/api/v2/notification-plugins/:id", args: []string{"id"}, sideEffect: true, headerOnlyKey: true, summary: "更新全局通知插件（SYS_ADMIN）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.grants", method: "GET", path: "/api/v2/notification-plugin-grants", summary: "列出通知插件租户授权（SYS_ADMIN）", flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "pluginRegistrationId", kind: "string"}), build: queryBuild("page", "pageSize", "pluginRegistrationId")},
		{name: "notifications.grant-set", method: "PUT", path: "/api/v2/notification-plugin-grants", sideEffect: true, headerOnlyKey: true, summary: "创建或更新插件租户授权（SYS_ADMIN）", flags: []flagSpec{file}, build: jsonBody},
		{name: "notifications.receipt-credential", method: "POST", path: "/api/v2/platform/notification-tenants/:tenantId/notification-instances/:id/receipt-credential", args: []string{"tenantId", "id"}, sideEffect: true, headerOnlyKey: true, summary: "轮换回执凭据；仅一次显示（SYS_ADMIN）", build: fileBuild},
	}
}
