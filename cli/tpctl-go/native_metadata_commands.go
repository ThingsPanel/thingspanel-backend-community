package main

import "fmt"

// Native metadata routes are available when the Community metadata candidate
// is deployed. They are intentionally separate from the current v1 commands.
func nativeMetadataCommands() commandsList {
	file := flagSpec{name: "file", kind: "string", help: "原生 API 请求体 JSON 文件"}
	etag := flagSpec{name: "if-match", kind: "string", help: "详情接口返回的强 ETag（含引号）"}
	fileBody := func(in *invocation) (any, map[string][]string, error) {
		body, err := nativeJSONFile(in.last("file"))
		return body, nil, err
	}
	conditional := func(withBody bool) func(*invocation) (any, map[string][]string, error) {
		return func(in *invocation) (any, map[string][]string, error) {
			if in.last("if-match") == "" {
				return nil, nil, fmt.Errorf("需要 --if-match 指定详情返回的 ETag")
			}
			if withBody {
				return fileBody(in)
			}
			return nil, nil, nil
		}
	}
	pageFlags := []flagSpec{{name: "page", kind: "int"}, {name: "page-size", kind: "int"}, {name: "search", kind: "string"}}
	list := func(in *invocation) (any, map[string][]string, error) {
		q := queryOf(in, "page", "search")
		if v := in.last("page-size"); v != "" {
			q["pageSize"] = []string{v}
		}
		return nil, q, nil
	}
	cs := commandsList{}
	for _, r := range []struct{ name, path, id, label string }{
		{"native-devices", "/api/v2/devices", "deviceID", "设备"},
		{"native-templates", "/api/v2/device-access-templates", "templateID", "接入模板"},
		{"native-groups", "/api/v2/device-groups", "groupID", "设备分组"},
	} {
		item := r.path + "/:" + r.id
		cs = append(cs,
			&command{name: r.name + ".list", method: "GET", path: r.path, flags: pageFlags, build: list, summary: "列出原生" + r.label},
			&command{name: r.name + ".get", method: "GET", path: item, args: []string{r.id}, summary: "读取原生" + r.label + "和 ETag"},
			&command{name: r.name + ".create", method: "POST", path: r.path, flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "创建原生" + r.label},
			&command{name: r.name + ".update", method: "PATCH", path: item, args: []string{r.id}, flags: []flagSpec{file, etag}, build: conditional(true), sideEffect: true, headerOnlyKey: true, requireIfMatch: true, summary: "按 ETag 更新原生" + r.label},
			&command{name: r.name + ".delete", method: "DELETE", path: item, args: []string{r.id}, flags: []flagSpec{etag}, build: conditional(false), sideEffect: true, headerOnlyKey: true, requireIfMatch: true, summary: "按 ETag 删除原生" + r.label},
		)
	}
	cs = append(cs,
		&command{name: "native-groups.tree", method: "GET", path: "/api/v2/device-group-tree", summary: "读取原生分组树"},
		&command{name: "native-groups.members", method: "GET", path: "/api/v2/device-groups/:groupID/members", args: []string{"groupID"}, flags: pageFlags, build: list, summary: "读取原生分组成员"},
		&command{name: "native-groups.add-members", method: "POST", path: "/api/v2/device-groups/:groupID/member-additions", args: []string{"groupID"}, flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "添加原生分组成员"},
		&command{name: "native-groups.remove-members", method: "POST", path: "/api/v2/device-groups/:groupID/member-removals", args: []string{"groupID"}, flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "移除原生分组成员"},
		&command{name: "native-groups.move", method: "POST", path: "/api/v2/device-groups/:groupID/moves", args: []string{"groupID"}, flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "移动原生分组"},
		&command{name: "native-devices.groups", method: "GET", path: "/api/v2/devices/:deviceID/groups", args: []string{"deviceID"}, summary: "读取设备分组"},
		&command{name: "native-devices.runtime", method: "GET", path: "/api/v2/devices/:deviceID/runtime-state", args: []string{"deviceID"}, summary: "读取设备运行状态"},
		&command{name: "native-devices.readings", method: "GET", path: "/api/v2/devices/:deviceID/readings", args: []string{"deviceID"}, summary: "读取设备最新数据"},
		&command{name: "native-devices.template", method: "GET", path: "/api/v2/devices/:deviceID/template-binding", args: []string{"deviceID"}, summary: "读取设备绑定模板"},
	)
	// Only expose paths with concrete registered handlers in the candidate.
	for _, x := range []struct {
		name, path, summary string
		args                []string
	}{
		{"native-devices.statistics", "/api/v2/device-statistics", "设备统计", nil},
		{"native-devices.connection", "/api/v2/devices/:deviceID/connection-info", "设备连接信息", []string{"deviceID"}},
		{"native-devices.diagnostics", "/api/v2/devices/:deviceID/diagnostics", "设备诊断", []string{"deviceID"}},
		{"native-devices.administrative-state", "/api/v2/devices/:deviceID/administrative-state", "设备管理状态", []string{"deviceID"}},
		{"native-devices.gateway", "/api/v2/devices/:deviceID/gateway-membership", "网关关系", []string{"deviceID"}},
		{"native-devices.children", "/api/v2/devices/:deviceID/subdevices", "子设备", []string{"deviceID"}},
		{"native-devices.credentials", "/api/v2/devices/:deviceID/credentials", "凭据摘要", []string{"deviceID"}},
		{"native-devices.debug-logs", "/api/v2/devices/:deviceID/debug-logs", "设备调试日志", []string{"deviceID"}},
		{"native-devices.simulator-profile", "/api/v2/devices/:deviceID/simulator-profile", "模拟器配置及 ETag", []string{"deviceID"}},
		{"native-devices.simulator-config", "/api/v2/devices/:deviceID/simulator-config", "模拟器配置", []string{"deviceID"}},
		{"native-devices.simulation-run", "/api/v2/devices/:deviceID/simulation-runs/:runID", "模拟执行结果", []string{"deviceID", "runID"}},
		{"native-operations.reading-export", "/api/v2/reading-exports/:operationID", "读数导出状态", []string{"operationID"}},
		{"native-devices.commands", "/api/v2/devices/:deviceID/command-executions", "命令执行记录", []string{"deviceID"}},
		{"native-devices.command", "/api/v2/devices/:deviceID/command-executions/:executionID", "命令执行详情", []string{"deviceID", "executionID"}},
		{"native-devices.history", "/api/v2/devices/:deviceID/status-history", "状态历史", []string{"deviceID"}},
		{"native-templates.references", "/api/v2/device-access-templates/:templateID/references", "模板引用", []string{"templateID"}},
		{"native-operations.get", "/api/v2/metadata-operations/:operationID", "元数据操作状态", []string{"operationID"}},
		{"native-providers.list", "/api/v2/connection-providers", "连接器目录", nil},
		{"native-assets.get", "/api/v2/file-assets/:assetID", "读取授权文件资产", []string{"assetID"}},
	} {
		cs = append(cs, &command{name: x.name, method: "GET", path: x.path, args: x.args, summary: x.summary})
	}
	for _, x := range []struct {
		name, path, summary string
		args                []string
		conditional         bool
	}{
		{"native-templates.copy", "/api/v2/device-access-templates/:templateID/copies", "复制模板", []string{"templateID"}, false},
		{"native-devices.renumber", "/api/v2/devices/:deviceID/number-changes", "更改设备编号", []string{"deviceID"}, false},
		{"native-devices.execute", "/api/v2/devices/:deviceID/command-executions", "提交设备命令", []string{"deviceID"}, false},
		{"native-devices.cancel-command", "/api/v2/devices/:deviceID/command-executions/:executionID/cancellations", "取消设备命令", []string{"deviceID", "executionID"}, false},
		{"native-devices.set-state", "/api/v2/devices/:deviceID/administrative-state", "更改管理状态", []string{"deviceID"}, true},
		{"native-devices.bind-template", "/api/v2/devices/:deviceID/template-binding", "更改设备模板", []string{"deviceID"}, true},
		{"native-devices.bind-gateway", "/api/v2/devices/:deviceID/gateway-membership", "更改网关关系", []string{"deviceID"}, true},
		{"native-devices.credential-create", "/api/v2/devices/:deviceID/credentials", "创建设备凭据", []string{"deviceID"}, false},
		{"native-devices.credential-rotate", "/api/v2/devices/:deviceID/credentials/:credentialID/rotations", "轮换设备凭据", []string{"deviceID", "credentialID"}, false},
		{"native-devices.credential-revoke", "/api/v2/devices/:deviceID/credentials/:credentialID/revocations", "撤销设备凭据", []string{"deviceID", "credentialID"}, false},
		{"native-devices.debug-start", "/api/v2/devices/:deviceID/debug-sessions", "开始调试", []string{"deviceID"}, false},
		{"native-devices.export-readings", "/api/v2/devices/:deviceID/reading-exports", "创建读数导出任务", []string{"deviceID"}, false},
		{"native-devices.simulator-update", "/api/v2/devices/:deviceID/simulator-profile", "更新模拟配置", []string{"deviceID"}, true},
		{"native-devices.simulator-start", "/api/v2/devices/:deviceID/simulator-sessions", "开启模拟会话", []string{"deviceID"}, false},
		{"native-devices.simulate", "/api/v2/devices/:deviceID/simulation-runs", "运行模拟", []string{"deviceID"}, false},
	} {
		method, flags, build := "POST", []flagSpec{file}, fileBody
		if x.conditional {
			method, flags, build = "PUT", []flagSpec{file, etag}, conditional(true)
		}
		cs = append(cs, &command{name: x.name, method: method, path: x.path, args: x.args, flags: flags, build: build, sideEffect: true, headerOnlyKey: true, requireIfMatch: x.conditional, summary: x.summary})
	}
	cs = append(cs,
		&command{name: "native-templates.validate", method: "POST", path: "/api/v2/device-access-templates/:templateID/validations", args: []string{"templateID"}, flags: []flagSpec{file}, build: fileBody, summary: "校验模板配置"},
		&command{name: "native-devices.validate-number", method: "POST", path: "/api/v2/device-number-validations", flags: []flagSpec{file}, build: fileBody, summary: "校验设备编号"},
		&command{name: "native-templates.test-script", method: "POST", path: "/api/v2/processing-script-tests", flags: []flagSpec{file}, build: fileBody, summary: "校验处理脚本"},
		&command{name: "native-devices.debug-stop", method: "DELETE", path: "/api/v2/devices/:deviceID/debug-sessions/:sessionID", args: []string{"deviceID", "sessionID"}, sideEffect: true, headerOnlyKey: true, summary: "结束调试"},
		&command{name: "native-devices.simulator-stop", method: "DELETE", path: "/api/v2/devices/:deviceID/simulator-sessions/:sessionID", args: []string{"deviceID", "sessionID"}, sideEffect: true, noIdempotency: true, summary: "结束模拟会话"},
	)
	// These are exactly the query keys accepted by the corresponding handlers.
	queryKeys := map[string][]string{
		"native-devices.list":         {"page", "pageSize", "search", "deviceNumber", "deviceKind", "templateId", "productId", "groupId", "includeDescendants", "gatewayId", "administrativeState", "activationState", "connectivity", "label", "sort", "groupScope", "accessWay", "protocol", "providerId", "serviceInstanceId", "firmwareVersion", "batchNumber", "alarmState", "modelId", "hasTemplate", "gatewayScope"},
		"native-templates.list":       {"page", "pageSize", "search", "deviceKind", "protocol", "brand", "tag", "modelId", "administrativeState", "sort"},
		"native-groups.list":          {"page", "pageSize", "parentId", "rootOnly", "search", "sort"},
		"native-groups.members":       {"page", "pageSize", "search", "includeDescendants"},
		"native-groups.tree":          {"rootId", "maxDepth"},
		"native-templates.references": {"page", "pageSize"},
		"native-devices.commands":     {"page", "pageSize", "state", "deliveryMode", "identifier", "from", "to", "labelContains"},
		"native-devices.readings":     {"page", "pageSize", "kind", "identifier", "from", "to", "latestOnly", "aggregation", "window"},
		"native-devices.debug-logs":   {"page", "pageSize", "from", "to", "direction"},
		"native-devices.history":      {"page", "pageSize", "from", "connectivity"},
		"native-devices.credentials":  {"page", "pageSize"},
		"native-devices.children":     {"page", "pageSize", "search"},
		"native-devices.groups":       {"page", "pageSize"},
		"native-providers.list":       {"page", "pageSize"},
	}
	queryKeys["native-devices.statistics"] = queryKeys["native-devices.list"][2:]
	for _, cmd := range cs {
		cmd.nativeMetadata = true
		if keys, ok := queryKeys[cmd.name]; ok {
			cmd.flags = nil
			for _, key := range keys {
				name := key
				if key == "pageSize" {
					name = "page-size"
				}
				cmd.flags = append(cmd.flags, flagSpec{name: name, kind: "string"})
			}
			cmd.build = func(in *invocation) (any, map[string][]string, error) {
				q := map[string][]string{}
				for _, key := range keys {
					name := key
					if key == "pageSize" {
						name = "page-size"
					}
					if value := in.last(name); value != "" {
						q[key] = []string{value}
					}
				}
				return nil, q, nil
			}
		}
	}
	return cs
}
