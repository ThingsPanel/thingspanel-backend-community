package main

// These commands use the ThingModel API shipped in Community main. Mutations
// accept the API request object, so its revision and resource checks remain on
// the server and the CLI does not maintain another model of the domain.
func thingModelCommands() commandsList {
	file := flagSpec{name: "file", kind: "string", help: "API 请求体 JSON 文件，- 表示标准输入"}
	pageFlags := []flagSpec{{name: "page", kind: "int"}, {name: "page-size", kind: "int"}}
	pageQuery := func(in *invocation) (any, map[string][]string, error) {
		q := queryOf(in, "page", "query")
		if v := in.last("page-size"); v != "" {
			q["pageSize"] = []string{v}
		}
		return nil, q, nil
	}
	fileBody := func(in *invocation) (any, map[string][]string, error) {
		b, err := in.jsonFile("file")
		return b, nil, err
	}
	return commandsList{
		{name: "thingmodels.list", method: "GET", path: "/api/v2/thing-models", summary: "列出物模型", flags: append(append([]flagSpec{}, pageFlags...), flagSpec{name: "query", kind: "string"}), build: pageQuery},
		{name: "thingmodels.get", method: "GET", path: "/api/v2/thing-models/:modelID", args: []string{"modelID"}, summary: "读取物模型"},
		{name: "thingmodels.versions", method: "GET", path: "/api/v2/thing-models/:modelID/versions", args: []string{"modelID"}, flags: pageFlags, build: pageQuery, summary: "列出已发布版本"},
		{name: "thingmodels.version", method: "GET", path: "/api/v2/thing-models/:modelID/versions/:revision", args: []string{"modelID", "revision"}, summary: "读取已发布快照"},
		{name: "thingmodels.resources", method: "GET", path: "/api/v2/thing-models/:modelID/resources", args: []string{"modelID"}, summary: "读取物模型资源"},
		{name: "thingmodels.create", method: "POST", path: "/api/v2/thing-model-commands/create", flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "创建物模型"},
		{name: "thingmodels.update", method: "POST", path: "/api/v2/thing-model-commands/update", flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "更新物模型草稿"},
		{name: "thingmodels.publish", method: "POST", path: "/api/v2/thing-model-commands/publish", flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "发布物模型"},
		{name: "thingmodels.update-resources", method: "POST", path: "/api/v2/thing-model-commands/update-resources", flags: []flagSpec{file}, build: fileBody, sideEffect: true, headerOnlyKey: true, summary: "更新物模型资源"},
	}
}
