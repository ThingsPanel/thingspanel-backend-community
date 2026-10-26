package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	serverName       = "tpctl-mcp"
	serverVersion    = "1.0.0"
	defaultProtocol  = "2024-11-05"
	supportedProtos  = "2024-11-05,2025-03-26,2025-06-18"
	instructionsText = "ThingsPanel 自动化工具。规则、绑定、资源包、告警都走同一个服务，" +
		"所以这里看到的结果和网页、tpctl 命令行一致。改之前先 validate_rule 和 " +
		"preview_rule_change；只想看效果用 simulate_rule，它不会控设备也不会发通知。"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type server struct {
	client *client
	tools  map[string]*tool
	// out is an io.Writer, not a *json.Encoder: tests need to hand in a
	// bytes.Buffer, and a fresh encoder per write keeps each message on its
	// own line without holding state.
	out io.Writer
}

func newServer(c *client, out io.Writer) *server {
	byName := map[string]*tool{}
	for _, t := range tools() {
		byName[t.name] = t
	}
	return &server{client: c, tools: byName, out: out}
}

func main() {
	base := envOr("TPCTL_BASE_URL", "http://localhost:4000")
	token := envOr("TPCTL_TOKEN", os.Getenv("TP_AUTH_TOKEN"))
	tenant := envOr("TPCTL_TENANT", "")
	timeout := 30 * time.Second
	if v := os.Getenv("TPCTL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			timeout = d
		}
	}
	c := newClient(base, token, tenant, timeout)
	s := newServer(c, os.Stdout)
	if err := s.serve(os.Stdin); err != nil && err != io.EOF {
		fmt.Fprintln(os.Stderr, "tpctl-mcp: "+err.Error())
		os.Exit(1)
	}
}

// serve reads newline-delimited JSON-RPC until stdin closes.
func (s *server) serve(in io.Reader) error {
	dec := json.NewDecoder(bufio.NewReader(in))
	for {
		var req rpcRequest
		if err := dec.Decode(&req); err != nil {
			return err
		}
		s.handle(req)
	}
}

func (s *server) handle(req rpcRequest) {
	// A request without an id is a notification: it must never be answered.
	notification := len(req.ID) == 0 || string(req.ID) == "null"

	switch req.Method {
	case "initialize":
		if notification {
			return
		}
		s.reply(req.ID, s.initializeResult(req.Params))
	case "notifications/initialized", "initialized", "notifications/cancelled":
		return
	case "ping":
		if notification {
			return
		}
		s.reply(req.ID, map[string]any{})
	case "tools/list":
		if notification {
			return
		}
		s.reply(req.ID, map[string]any{"tools": s.toolList()})
	case "tools/call":
		if notification {
			return
		}
		s.callTool(req)
	default:
		if notification {
			return
		}
		s.fail(req.ID, -32601, "unknown method: "+req.Method, nil)
	}
}

func (s *server) initializeResult(params json.RawMessage) map[string]any {
	version := defaultProtocol
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 && json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
		// Echo the client's version when we speak it; otherwise offer ours.
		for _, v := range strings.Split(supportedProtos, ",") {
			if v == p.ProtocolVersion {
				version = p.ProtocolVersion
				break
			}
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
		"instructions": instructionsText,
	}
}

func (s *server) toolList() []map[string]any {
	// Sorted so the list is stable across runs: a model that caches the tool
	// list should not see it shuffle.
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sortStrings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, toolDescriptor(s.tools[name]))
	}
	return out
}

func (s *server) callTool(req rpcRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			s.fail(req.ID, -32602, "params 不是合法对象", nil)
			return
		}
	}
	t, ok := s.tools[params.Name]
	if !ok {
		s.fail(req.ID, -32602, "unknown tool: "+params.Name,
			map[string]any{"available": len(s.tools)})
		return
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	// The tenant is not an argument. If a caller passes one, say so instead of
	// silently ignoring it — "I set the tenant and nothing changed" is a much
	// worse experience than being told no.
	for _, forbidden := range []string{"tenant", "tenant_id", "tenantId"} {
		if _, ok := params.Arguments[forbidden]; ok {
			s.toolError(req.ID, &apiError{
				Code:    "invalid_arguments",
				Message: "租户由服务端凭据决定，不能作为工具参数传入",
				Fields:  []fieldError{{Field: forbidden, Code: "forbidden", Message: "不接受租户参数"}},
			})
			return
		}
	}

	data, apiErr := t.call(s.client, params.Arguments)
	if apiErr != nil {
		s.toolError(req.ID, apiErr)
		return
	}
	s.toolResult(req.ID, t, data)
}

// toolResult returns a successful call.
//
// Both halves are provided: structuredContent for a host that understands it,
// and a text rendering for the models that only read text. They are the same
// bytes, so there is no way for them to disagree.
func (s *server) toolResult(id json.RawMessage, t *tool, data any) {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		s.toolError(id, &apiError{Code: "internal", Message: "结果无法序列化"})
		return
	}
	result := map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(raw)}},
		"isError": false,
		"_meta": map[string]any{
			"tool":     t.name,
			"readOnly": t.readOnly,
		},
	}
	if obj, ok := data.(map[string]any); ok {
		result["structuredContent"] = obj
	}
	s.reply(id, result)
}

// toolError reports a failure as a tool result rather than a protocol error:
// the model needs to see the code and the fields so it can fix its own call.
func (s *server) toolError(id json.RawMessage, e *apiError) {
	raw, _ := json.MarshalIndent(map[string]any{"error": e}, "", "  ")
	s.reply(id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(raw)}},
		"isError": true,
	})
}

func (s *server) reply(id json.RawMessage, result any) {
	_ = json.NewEncoder(s.out).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *server) fail(id json.RawMessage, code int, msg string, data any) {
	_ = json.NewEncoder(s.out).Encode(rpcResponse{
		JSONRPC: "2.0", ID: id,
		Error: &rpcError{Code: code, Message: msg, Data: data},
	})
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
