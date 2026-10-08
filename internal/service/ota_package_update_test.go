package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"project/internal/model"
)

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
