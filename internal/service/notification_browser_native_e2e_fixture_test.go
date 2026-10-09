package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadNativeBrowserE2EFixtureStrictAndBounded(t *testing.T) {
	valid := `{"schemaVersion":1,"sourceDeploymentId":"deployment-native","tenantId":"tenant-native","nativeInstanceId":"b682f98b-996c-4d06-a4bb-b46edbd92d4b","nativeGroupId":"0f8fad5b-d9cb-469f-a165-70867728950e","nativeGroupRevision":3}`
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal("write fixture")
	}
	fixture, err := readNativeBrowserE2EFixture(path)
	if err != nil || fixture.NativeGroupRevision != 3 || fixture.TenantID != "tenant-native" {
		t.Fatalf("valid fixture not decoded: revision=%d tenant=%q err=%v", fixture.NativeGroupRevision, fixture.TenantID, err)
	}
	for name, raw := range map[string]string{
		"unknown field":       `{"schemaVersion":1,"sourceDeploymentId":"deployment-native","tenantId":"tenant-native","nativeInstanceId":"b682f98b-996c-4d06-a4bb-b46edbd92d4b","nativeGroupId":"0f8fad5b-d9cb-469f-a165-70867728950e","nativeGroupRevision":3,"baseUrl":"https://example.invalid"}`,
		"duplicate field":     `{"schemaVersion":1,"schemaVersion":1,"sourceDeploymentId":"deployment-native","tenantId":"tenant-native","nativeInstanceId":"b682f98b-996c-4d06-a4bb-b46edbd92d4b","nativeGroupId":"0f8fad5b-d9cb-469f-a165-70867728950e","nativeGroupRevision":3}`,
		"trailing JSON":       valid + `{}`,
		"malformed JSON":      valid[:len(valid)-1],
		"oversize input":      valid + string(make([]byte, 4096)),
		"null instead object": `null`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal("write fixture")
			}
			if _, err := readNativeBrowserE2EFixture(path); err == nil {
				t.Fatal("unsafe fixture was accepted")
			}
		})
	}
	if _, err := readNativeBrowserE2EFixture(filepath.Join(t.TempDir(), "relative.json")); err == nil {
		t.Fatal("missing fixture was accepted")
	}
}
