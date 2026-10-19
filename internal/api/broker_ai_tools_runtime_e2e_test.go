package api

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"project/internal/brokeraitools"
)

// runChatBridgeBrokerToolRuntimeE2E is deliberately opt-in because it builds
// the CLI and imports a separately checked-out ChatBridge source tree. It
// exercises a read-only delegated result lookup; no LLM or ChatBridge server
// is started.
func runChatBridgeBrokerToolRuntimeE2E(t *testing.T, router *gin.Engine, issued brokeraitools.Issued, tenantID, subjectID, deviceID, messageID string) {
	t.Helper()
	if os.Getenv("TEST_CHATBRIDGE_BROKER_TOOLS_E2E") != "1" {
		t.Skip("set TEST_CHATBRIDGE_BROKER_TOOLS_E2E=1 to build the CLI and run the real ChatBridge adapter/dispatcher chain")
	}
	chatbridgeSrc := strings.TrimSpace(os.Getenv("CHATBRIDGE_SRC"))
	if chatbridgeSrc == "" {
		t.Fatal("TEST_CHATBRIDGE_BROKER_TOOLS_E2E=1 requires CHATBRIDGE_SRC to name an existing ChatBridge source checkout")
	}
	info, err := os.Stat(chatbridgeSrc)
	if err != nil || !info.IsDir() {
		t.Fatal("CHATBRIDGE_SRC must point to an existing source directory")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("python3 is unavailable for the explicitly enabled ChatBridge integration")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal("go toolchain is unavailable")
	}
	if issued.Token == "" || issued.TenantID != tenantID || issued.SubjectID != subjectID {
		t.Fatal("fixture delegation is not bound to the expected actor")
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal("resolve repository root")
	}
	temp := t.TempDir()
	clientPath := filepath.Join(temp, "tp-broker-tools-client")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("resolve toolchain home")
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		t.Fatal("resolve toolchain cache directory")
	}
	build := exec.Command(goPath, "build", "-p", "1", "-o", clientPath, "./cmd/tp-broker-tools-client")
	build.Dir = repoRoot
	build.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GOPATH=" + filepath.Join(home, "go"),
		"GOMODCACHE=" + filepath.Join(home, "go", "pkg", "mod"),
		"GOCACHE=" + filepath.Join(cacheRoot, "go-build"),
		"GOTOOLCHAIN=local",
		"GOENV=off",
		"GOPROXY=https://proxy.golang.org,direct",
		"GOSUMDB=sum.golang.org",
	}
	if output, err := build.CombinedOutput(); err != nil {
		// Compiler diagnostics cannot contain the delegated credential (it has
		// not been sent to the build process), but keep failures concise.
		t.Fatalf("build broker tools client failed: %v (%s)", err, strings.TrimSpace(string(output)))
	}
	if err := os.Chmod(clientPath, 0o700); err != nil {
		t.Fatal("make built CLI executable")
	}

	server := httptest.NewTLSServer(router)
	defer server.Close()
	caPath := filepath.Join(temp, "api-ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal("write temporary test CA")
	}

	issuedJSON, err := json.Marshal(issued)
	if err != nil {
		t.Fatal("encode fixture delegation")
	}
	input, err := json.Marshal(map[string]any{
		"issued":     json.RawMessage(issuedJSON),
		"actor":      map[string]string{"tenant_id": tenantID, "actor_id": subjectID, "thread_id": "runtime-e2e-thread"},
		"binding":    map[string]string{"tenant_id": tenantID, "system_user_id": subjectID},
		"device_id":  deviceID,
		"message_id": messageID,
		"origin":     server.URL,
		"ca_file":    caPath,
		"client":     clientPath,
	})
	if err != nil {
		t.Fatal("encode synthetic ChatBridge context")
	}

	// Keep the credential exclusively on stdin. Python's environment contains
	// only a fixed executable search path, locale and the two import roots.
	script := `import json, sys
try:
    from types import SimpleNamespace
    from chatbridge.runtime import RuntimeAdapterRegistry, RuntimeDispatcher
    from chatbridge.services.installed_skill_registry import EffectiveSkillInstallation
    from chatbridge.skillspec import InstallationStatus, SkillInstallation, SkillScope
    from chatbridge.store import SkillVersionRecord
    from chatbridge.contracts.types import RuntimeToolCall
    from integrations.chatbridge.broker_tools import CommunityBrokerToolAdapter, GrantBindings

    data = json.load(sys.stdin)
    actor = data["actor"]
    bindings = GrantBindings()
    handle = bindings.bind_issued(data["issued"], actor)
    adapter = CommunityBrokerToolAdapter(bindings, executable=data["client"], origin=data["origin"], ca_file=data["ca_file"])
    registry = RuntimeAdapterRegistry()
    registry.register(adapter)
    dispatcher = RuntimeDispatcher(registry)
    # This is the repository's fixed test installation manifest, supplied by
    # the Go fixture and never accepted from model arguments.
    manifest = data["manifest"]
    installation = EffectiveSkillInstallation(
        installation=SkillInstallation("runtime-e2e", "thingspanel.community.broker-tools", "1.0.0", SkillScope.TENANT, actor["tenant_id"], InstallationStatus.ENABLED),
        version_record=SkillVersionRecord("thingspanel.community.broker-tools", "1.0.0", "http_tool", manifest),
    )
    request_context = {
        "binding": SimpleNamespace(**data["binding"]),
        "tenant_id": actor["tenant_id"], "actor_id": actor["actor_id"], "thread_id": actor["thread_id"],
        "community_broker_grant_id": handle,
    }
    result = dispatcher.dispatch(
        call=RuntimeToolCall("community_commands_get_result", {"device_id": data["device_id"], "message_id": data["message_id"]}),
        installations=[installation], request_context=request_context, actor_context=actor,
    )
    if not result.ok or set(result.payload) != {"ok", "data"} or result.payload.get("ok") is not True:
        raise RuntimeError("dispatcher rejected result lookup")
    result_data = result.payload["data"]
    if set(result_data) != {"message_id", "status", "created_at"} or result_data.get("message_id") != data["message_id"] or result_data.get("status") != "device_reported_success":
        raise RuntimeError("safe result projection mismatch")
    visible = json.dumps({"payload": result.payload, "audit": result.audit_payload, "error": result.error}, separators=(",", ":"))
    if data["issued"]["token"] in visible:
        raise RuntimeError("credential reached dispatcher output")
    print(json.dumps({"ok": True, "operation": "commands.getResult", "message_id": result_data["message_id"], "status": result_data["status"], "has_created_at": bool(result_data["created_at"]), "runtime_kind": result.audit_payload.get("runtime_kind")}, separators=(",", ":")))
except Exception:
    # Fixed failure output prevents traceback or request data from exposing the token.
    print('{"ok":false,"error":"runtime_e2e_failed"}')
    raise SystemExit(1)
`
	manifestPath := filepath.Join(repoRoot, "integrations", "chatbridge", "runtime-manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal("read repository runtime manifest")
	}
	var manifest any
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal("decode repository runtime manifest")
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		t.Fatal("decode synthetic subprocess input")
	}
	payload["manifest"] = manifest
	input, err = json.Marshal(payload)
	if err != nil {
		t.Fatal("encode final subprocess input")
	}
	// Python -c receives the trusted runtime installation manifest from this
	// repository; neither it nor the opaque handle is accepted from model args.
	pythonCmd := exec.Command(python, "-c", script)
	pythonCmd.Dir = repoRoot
	pythonCmd.Env = []string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"LANG=C.UTF-8",
		"PYTHONPATH=" + repoRoot + string(os.PathListSeparator) + chatbridgeSrc,
	}
	pythonCmd.Stdin = bytes.NewReader(input)
	output, err := pythonCmd.Output()
	if bytes.Contains(output, []byte(issued.Token)) {
		t.Fatal("credential appeared in runtime subprocess output")
	}
	if err != nil {
		t.Fatalf("ChatBridge dispatcher/adapter runtime chain failed: %v; safe output=%s", err, strings.TrimSpace(string(output)))
	}
	var result struct {
		OK           bool   `json:"ok"`
		Operation    string `json:"operation"`
		MessageID    string `json:"message_id"`
		Status       string `json:"status"`
		HasCreatedAt bool   `json:"has_created_at"`
		RuntimeKind  string `json:"runtime_kind"`
	}
	if err := json.Unmarshal(output, &result); err != nil || !result.OK || result.Operation != "commands.getResult" || result.MessageID != messageID || result.Status != "device_reported_success" || !result.HasCreatedAt || result.RuntimeKind != "http_tool" {
		t.Fatalf("invalid safe ChatBridge runtime projection: decode=%v result=%+v", err, result)
	}
}
