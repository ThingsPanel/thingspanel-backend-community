package main

import (
	"encoding/json"
	"fmt"
)

// The current Community device API uses x-token and wraps even errors in HTTP
// 200. It does not promise idempotent writes, so no retry key is invented.
func communityVerdict(raw []byte) *apiError {
	var result struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || result.Code == nil {
		return &apiError{Code: "invalid_response", Message: "社区接口返回了无法识别的响应", ExitCode: exitError}
	}
	if *result.Code == 200 {
		return nil
	}
	return &apiError{Code: "community_error", AppCode: fmt.Sprint(*result.Code), Message: result.Message, ExitCode: exitError}
}

func communityDeviceCommands() commandsList {
	file := flagSpec{name: "file", kind: "string", help: "社区 API 请求体 JSON 文件"}
	fileBody := func(in *invocation) (any, map[string][]string, error) { b, e := in.jsonFile("file"); return b, nil, e }
	listFlags := []flagSpec{{name: "page", kind: "int", def: "1"}, {name: "page-size", kind: "int", def: "20"}, {name: "name", kind: "string"}}
	listQuery := func(in *invocation) (any, map[string][]string, error) {
		q := queryOf(in, "page", "name")
		if v := in.last("page-size"); v != "" {
			q["page_size"] = []string{v}
		}
		return nil, q, nil
	}
	var result commandsList
	for _, r := range []struct{ name, path, detail, label string }{
		{"devices", "/api/v1/device", "/detail/:id", "设备"},
		{"device-templates", "/api/v1/device_config", "/:id", "设备模板"},
		{"device-groups", "/api/v1/device/group", "/detail/:id", "设备分组"},
	} {
		result = append(result,
			&command{name: r.name + ".list", method: "GET", path: r.path, flags: listFlags, build: listQuery, communityAPI: true, verdict: communityVerdict, summary: "列出" + r.label},
			&command{name: r.name + ".get", method: "GET", path: r.path + r.detail, args: []string{"id"}, communityAPI: true, verdict: communityVerdict, summary: "读取" + r.label},
			&command{name: r.name + ".create", method: "POST", path: r.path, flags: []flagSpec{file}, build: fileBody, sideEffect: true, noIdempotency: true, communityAPI: true, verdict: communityVerdict, summary: "创建" + r.label},
			&command{name: r.name + ".update", method: "PUT", path: r.path, flags: []flagSpec{file}, build: fileBody, sideEffect: true, noIdempotency: true, communityAPI: true, verdict: communityVerdict, summary: "更新" + r.label},
			&command{name: r.name + ".delete", method: "DELETE", path: r.path + "/:id", args: []string{"id"}, sideEffect: true, noIdempotency: true, communityAPI: true, verdict: communityVerdict, summary: "删除" + r.label},
		)
	}
	result = append(result,
		&command{name: "device-groups.tree", method: "GET", path: "/api/v1/device/group/tree", communityAPI: true, verdict: communityVerdict, summary: "读取分组树"},
		&command{name: "devices.status", method: "GET", path: "/api/v1/device/online/status/:id", args: []string{"id"}, communityAPI: true, verdict: communityVerdict, summary: "读取设备在线状态"},
		&command{name: "devices.children", method: "GET", path: "/api/v1/device/sub-list/:id", args: []string{"id"}, communityAPI: true, verdict: communityVerdict, summary: "列出子设备"},
	)
	return result
}
