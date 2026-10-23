package authcore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Authentication is only served by the self-hosted, no-tracing profile. Encore
// dev tracing can capture credential bodies before an application logger runs.
func privacyReady() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	raw, e := os.ReadFile("/proc/self/environ")
	if e != nil {
		return false
	}
	values := map[string]string{}
	for _, entry := range strings.Split(string(raw), "\x00") {
		k, v, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if k == "ENCORE_RUNTIME_CONFIG" || k == "ENCORE_RUNTIME_CONFIG_PATH" || strings.HasPrefix(k, "ENCORE_TRACE_") || strings.HasPrefix(k, "OTEL_") {
			return false
		}
		values[k] = v
	}
	path := values["ENCORE_INFRA_CONFIG_PATH"]
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || values["TP_AUTH_INFRA_CONFIG_PATH"] != path {
		return false
	}
	data, e := os.ReadFile(path)
	if e != nil || len(data) > 4<<20 {
		return false
	}
	if values["TP_AUTH_INFRA_SHA256"] != Digest(string(data)) {
		return false
	}
	var config map[string]any
	if json.Unmarshal(data, &config) != nil || config == nil {
		return false
	}
	return noTrace(config)
}
func noTrace(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			key := strings.ToLower(k)
			if strings.Contains(key, "trace") || strings.Contains(key, "observability") {
				if value != nil {
					return false
				}
			}
			if !noTrace(value) {
				return false
			}
		}
	case []any:
		for _, value := range x {
			if !noTrace(value) {
				return false
			}
		}
	}
	return true
}
