package legacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testClient(ts *httptest.Server) *Client {
	return &Client{baseURL: ts.URL, email: "test@test.cn", password: "123456", http: ts.Client()}
}

func TestGetTelemetryCurrent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "t"}})
		case "/api/v1/telemetry/datas/current/dev-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": []map[string]any{{"key": "switch", "value": 1, "ts": "now"}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cli := testClient(ts)
	points, err := cli.GetTelemetryCurrent(context.Background(), "dev-1")
	if err != nil {
		t.Fatalf("GetTelemetryCurrent err=%v", err)
	}
	if len(points) != 1 || points[0].Key != "switch" || points[0].Value != "1" {
		t.Fatalf("unexpected points: %+v", points)
	}
}

func TestPublishCommand(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "***"}})
		case "/api/v1/command/datas/dev-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": []any{map[string]any{"identifier": "switch"}}})
		case "/api/v1/command/datas/pub":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cli := testClient(ts)
	code, msg, err := cli.PublishCommand(context.Background(), "dev-1", "switch", "0")
	if err != nil {
		t.Fatalf("PublishCommand err=%v", err)
	}
	if code != 200 || msg != "ok" {
		t.Fatalf("unexpected result code=%d msg=%s", code, msg)
	}
}

func TestPublishCommand_DoesNotFallbackToTelemetry(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var paths []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/login":
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "fixture"}})
				case "/api/v1/command/datas/pub":
					var body controlReq
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.DeviceID != "dev-1" || body.Identify != "switch" || body.Value != "false" {
						t.Errorf("unexpected command: %+v", body)
					}
					w.WriteHeader(status)
					_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": "result"})
				default:
					t.Errorf("unexpected fallback or model lookup: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer ts.Close()
			code, _, err := testClient(ts).PublishCommand(context.Background(), "dev-1", "switch", "false")
			if status == http.StatusOK && (err != nil || code != status) {
				t.Fatalf("code=%d err=%v", code, err)
			}
			if status != http.StatusOK && err == nil {
				t.Fatal("command failure must be returned without fallback")
			}
			if len(paths) != 2 {
				t.Fatalf("unexpected requests: %v", paths)
			}
		})
	}
}

func TestCreateProtocolPlugin(t *testing.T) {
	var captured map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "***"}})
		case "/api/v1/protocol_plugin":
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "操作成功", "data": map[string]any{"id": "p-1"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cli := testClient(ts)
	raw, err := cli.CreateProtocolPlugin(context.Background(), ProtocolPluginCreateReq{
		Name:                 "TCP插件（直连）",
		DeviceType:           2,
		ProtocolType:         "tcp",
		ServiceAccessAddress: "http://10.0.0.8:15002",
		Description:          "tcp plugin",
		AdditionalInfo:       "{}",
		LanguageCode:         "zh-CN",
	})
	if err != nil {
		t.Fatalf("CreateProtocolPlugin err=%v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("expected non-empty response")
	}
	if got, _ := captured["name"].(string); got != "TCP插件（直连）" {
		t.Fatalf("unexpected name: %v", captured["name"])
	}
	if got, _ := captured["language_code"].(string); got != "zh-CN" {
		t.Fatalf("unexpected language_code: %v", captured["language_code"])
	}
}

func TestUpsertServicePluginByIdentifier_CreateWhenMissing(t *testing.T) {
	var createCaptured map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "***"}})
		case r.URL.Path == "/api/v1/service/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"list": []any{}}})
		case r.URL.Path == "/api/v1/service" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&createCaptured)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "操作成功", "data": map[string]any{"id": "svc-1"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cli := testClient(ts)
	raw, err := cli.UpsertServicePluginByIdentifier(context.Background(), ServicePluginUpsertReq{
		Name:              "TCP插件（直连）-adapter-tcp",
		ServiceIdentifier: "tcpgeneric",
		ServiceType:       1,
		Description:       "desc",
		Version:           "v1.0.0",
		ServiceHeartbeat:  1,
		ServiceConfig:     "{}",
	})
	if err != nil {
		t.Fatalf("UpsertServicePluginByIdentifier err=%v", err)
	}
	if len(raw) == 0 {
		t.Fatalf("expected non-empty response")
	}
	if _, ok := createCaptured["id"]; ok {
		t.Fatalf("create payload should not include id")
	}
	if got, _ := createCaptured["service_identifier"].(string); got != "tcpgeneric" {
		t.Fatalf("unexpected service_identifier=%v", createCaptured["service_identifier"])
	}
}

func TestUpsertServicePluginByIdentifier_UpdateWhenExists(t *testing.T) {
	var updateCaptured map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "***"}})
		case r.URL.Path == "/api/v1/service/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"list": []map[string]any{{"id": "svc-exist", "name": "x", "service_identifier": "tcpgeneric", "service_type": 1}}}})
		case r.URL.Path == "/api/v1/service" && r.Method == http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&updateCaptured)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "操作成功", "data": map[string]any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	cli := testClient(ts)
	_, err := cli.UpsertServicePluginByIdentifier(context.Background(), ServicePluginUpsertReq{
		Name:              "TCP插件（直连）-adapter-tcp",
		ServiceIdentifier: "tcpgeneric",
		ServiceType:       1,
		ServiceConfig:     "{}",
	})
	if err != nil {
		t.Fatalf("UpsertServicePluginByIdentifier err=%v", err)
	}
	if got, _ := updateCaptured["id"].(string); got != "svc-exist" {
		t.Fatalf("expected update id svc-exist, got %v", updateCaptured["id"])
	}
}

func TestUnbindSubdeviceCallsSubRemoveAndSurfacesRefusal(t *testing.T) {
	var method, path string
	var captured map[string]any
	refuse := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"token": "***"}})
			return
		}
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&captured)
		if refuse {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "message": "nope"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "ok", "data": nil})
	}))
	defer ts.Close()

	cli := testClient(ts)
	if err := cli.UnbindSubdevice(context.Background(), "dev-1"); err != nil {
		t.Fatalf("UnbindSubdevice err=%v", err)
	}
	if method != http.MethodPut || path != "/api/v1/device/sub-remove" || captured["sub_device_id"] != "dev-1" {
		t.Fatalf("unexpected call: %s %s %v", method, path, captured)
	}
	refuse = true
	if err := cli.UnbindSubdevice(context.Background(), "dev-1"); !errors.Is(err, ErrRejected) {
		t.Fatalf("a refusal must be ErrRejected, got %v", err)
	}
}
