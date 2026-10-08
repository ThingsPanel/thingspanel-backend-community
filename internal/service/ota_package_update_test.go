package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"project/internal/model"
)

func TestExpectedLegacyOTAObjectKeyBindsConfiguredProductAndVersion(t *testing.T) {
	for _, tc := range []struct {
		name, product, version, want string
	}{
		{"A100", "A100 电子吧唧模板 01", "1.0.7", "firmware/a100/1.0.7/xiaozhi-1.0.7.bin"},
		{"ESP32S3", "YGSoul ESP32S3", "1.0.38", "firmware/esp32s3/1.0.38/xiaozhi-1.0.38.bin"},
		{"unsupported", "unknown", "1.0.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expectedLegacyOTAObjectKey(tc.product, tc.version); got != tc.want {
				t.Fatalf("object key = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRequestExistingFirmwareReceiptUsesAuthenticatedExactKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/internal/ota/firmware/receipt" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Yomi-Internal-Token") != "test-token" {
			t.Fatal("internal token header missing")
		}
		body := make([]byte, r.ContentLength)
		if _, err := r.Body.Read(body); err != nil && err.Error() != "EOF" {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `"objectKey":"firmware/a100/1.0.7/xiaozhi-1.0.7.bin"`) {
			t.Fatalf("unexpected request body %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"storage":"tos","objectKey":"firmware/a100/1.0.7/xiaozhi-1.0.7.bin","publicUrl":"https://bucket.tos.example/firmware/a100/1.0.7/xiaozhi-1.0.7.bin","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":42,"receipt":"signed"}}`))
	}))
	defer server.Close()
	receipt, err := requestExistingFirmwareReceipt(server.URL+"/api/v1/internal/ota/firmware", "test-token", "firmware/a100/1.0.7/xiaozhi-1.0.7.bin")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Receipt != "signed" || receipt.Size != 42 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
}

func TestValidateOTAUpdateRejectsFirmwareAddressChanges(t *testing.T) {
	current := &model.OtaUpgradePackage{PackageURL: stringPtr("https://tos.example/firmware/1.bin"), AdditionalInfo: stringPtr(`{"tosObjectKey":"firmware/1.bin"}`)}
	req := &model.UpdateOTAUpgradePackageReq{PackageUrl: stringPtr("https://attacker.example/firmware.bin")}
	if err := validateOTAUpdate(req, current); err == nil {
		t.Fatal("expected firmware URL change to be rejected")
	}
}

func TestValidateOTAUpdateRejectsTOSMetadataChanges(t *testing.T) {
	current := &model.OtaUpgradePackage{PackageURL: stringPtr("https://tos.example/firmware/1.bin"), AdditionalInfo: stringPtr(`{"tosObjectKey":"firmware/1.bin"}`)}
	req := &model.UpdateOTAUpgradePackageReq{AdditionalInfo: stringPtr(`{"tosObjectKey":"firmware/other.bin"}`)}
	if err := validateOTAUpdate(req, current); err == nil {
		t.Fatal("expected TOS metadata change to be rejected")
	}
}

func TestValidOTAFirmwareReceiptBindsAddressAndMetadata(t *testing.T) {
	token, key, address, digest, size := "secret", "firmware/esp32s3/1.2.3/xiaozhi-1.2.3.bin", "https://bucket.tos.example/firmware/esp32s3/1.2.3/xiaozhi-1.2.3.bin", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", int64(42)
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%d", key, address, digest, size)
	receipt := hex.EncodeToString(mac.Sum(nil))
	if !validOTAFirmwareReceipt(token, key, address, digest, size, receipt) {
		t.Fatal("valid receipt rejected")
	}
	if validOTAFirmwareReceipt(token, key, "https://attacker.example/firmware.bin", digest, size, receipt) {
		t.Fatal("receipt accepted a substituted download URL")
	}
}

func TestTOSOTAAdditionalInfoRoutesDeliveryThroughDeviceIntegration(t *testing.T) {
	result, err := prepareTOSOTAAdditionalInfo(`{"partitionTable":"v2/32m"}`, "firmware/a100/1.2.3/xiaozhi-1.2.3.bin", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "receipt", 42, "a100")
	if err != nil {
		t.Fatal(err)
	}
	var info map[string]interface{}
	if err := json.Unmarshal([]byte(*result), &info); err != nil {
		t.Fatal(err)
	}
	if info["deliveryProtocol"] != "ygsoul_http" || info["orchestrationRoute"] != "device_integration" || info["tosObjectKey"] == nil || info["size"] != float64(42) {
		t.Fatalf("OTA route metadata = %#v", info)
	}
}

func TestTOSPackageReceiptRequiredBeforeTaskDispatch(t *testing.T) {
	token, key, address, digest, size := "secret", "firmware/esp32s3/1.2.3/xiaozhi-1.2.3.bin", "https://bucket.tos.example/firmware/esp32s3/1.2.3/xiaozhi-1.2.3.bin", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", int64(42)
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%d", key, address, digest, size)
	info, err := prepareTOSOTAAdditionalInfo("{}", key, digest, hex.EncodeToString(mac.Sum(nil)), size, "esp32s3")
	if err != nil {
		t.Fatal(err)
	}
	ota := &model.OtaUpgradePackage{PackageURL: &address, AdditionalInfo: info}
	if !isVerifiedTOSOTAPackage(ota, token) {
		t.Fatal("valid TOS package was rejected")
	}
	if isVerifiedTOSOTAPackage(ota, "wrong-token") {
		t.Fatal("untrusted TOS package was accepted")
	}
}

func stringPtr(value string) *string { return &value }
