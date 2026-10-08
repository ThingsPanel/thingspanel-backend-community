package service

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
)

type existingOTAFirmwareReceipt struct {
	Storage   string `json:"storage"`
	ObjectKey string `json:"objectKey"`
	PublicURL string `json:"publicUrl"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Receipt   string `json:"receipt"`
}

func expectedLegacyOTAObjectKey(productName, version string) string {
	product := strings.ToLower(productName)
	productKey := ""
	switch {
	case strings.Contains(product, "esp32s3"):
		productKey = "esp32s3"
	case strings.Contains(product, "a100"):
		productKey = "a100"
	default:
		return ""
	}
	return fmt.Sprintf("firmware/%s/%s/xiaozhi-%s.bin", productKey, version, version)
}

func requestExistingFirmwareReceipt(endpoint, token, objectKey string) (*existingOTAFirmwareReceipt, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	privateYomiHost := parsed != nil && parsed.Scheme == "http" && (parsed.Hostname() == "host.docker.internal" || parsed.Hostname() == "yomi-server" || (net.ParseIP(parsed.Hostname()) != nil && net.ParseIP(parsed.Hostname()).IsLoopback()))
	if err != nil || parsed == nil || parsed.Host == "" || (!privateYomiHost && parsed.Scheme != "https") || token == "" || !strings.HasSuffix(strings.TrimRight(parsed.Path, "/"), "/firmware") {
		return nil, fmt.Errorf("TOS 固件校验服务未配置")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/receipt"
	body, err := json.Marshal(map[string]string{"objectKey": objectKey})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Yomi-Internal-Token", token)
	resp, err := (&http.Client{
		Timeout:       45 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Code int                        `json:"code"`
		Data existingOTAFirmwareReceipt `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16*1024)).Decode(&payload); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || payload.Code != 0 || payload.Data.Storage != "tos" || payload.Data.ObjectKey != objectKey || payload.Data.Size <= 0 || payload.Data.Receipt == "" {
		return nil, fmt.Errorf("TOS 固件对象校验失败")
	}
	if digest, err := hex.DecodeString(payload.Data.SHA256); err != nil || len(digest) != 32 {
		return nil, fmt.Errorf("TOS 固件哈希无效")
	}
	return &payload.Data, nil
}

func ensureLegacyOTAPackageHasTOSReceipt(ota *model.OtaUpgradePackage) error {
	if isVerifiedTOSOTAPackage(ota, os.Getenv("YOMI_INTERNAL_EVENT_TOKEN")) {
		return nil
	}
	if ota == nil || ota.AdditionalInfo == nil || ota.PackageURL == nil {
		return fmt.Errorf("OTA 包缺少 TOS 对象信息")
	}
	var metadata struct {
		ObjectKey string `json:"tosObjectKey"`
		Receipt   string `json:"tosReceipt"`
	}
	if json.Unmarshal([]byte(*ota.AdditionalInfo), &metadata) != nil || metadata.ObjectKey == "" || metadata.Receipt != "" {
		return fmt.Errorf("OTA 包 TOS 回执无效，拒绝回退")
	}
	config, err := dal.GetDeviceConfigByID(ota.DeviceConfigID)
	if err != nil {
		return err
	}
	expectedKey := expectedLegacyOTAObjectKey(config.Name, ota.Version)
	if expectedKey == "" || metadata.ObjectKey != expectedKey {
		return fmt.Errorf("OTA 包 TOS 对象键与设备产品/版本不匹配")
	}
	token := os.Getenv("YOMI_INTERNAL_EVENT_TOKEN")
	receipt, err := requestExistingFirmwareReceipt(os.Getenv("YOMI_OTA_FIRMWARE_URL"), token, metadata.ObjectKey)
	if err != nil {
		return err
	}
	if !validOTAFirmwareReceipt(token, receipt.ObjectKey, receipt.PublicURL, receipt.SHA256, receipt.Size, receipt.Receipt) {
		return fmt.Errorf("TOS 固件回执签名无效")
	}
	productKey := strings.Split(metadata.ObjectKey, "/")[1]
	additional, err := prepareTOSOTAAdditionalInfo(*ota.AdditionalInfo, receipt.ObjectKey, receipt.SHA256, receipt.Receipt, receipt.Size, productKey)
	if err != nil {
		return err
	}
	signatureType := "SHA256"
	if err := dal.UpdateOTAPackageTOSMetadata(ota.ID, ota.TenantID, receipt.ObjectKey, receipt.PublicURL, receipt.SHA256, &signatureType, additional); err != nil {
		return err
	}
	ota.PackageURL = &receipt.PublicURL
	ota.Signature = &receipt.SHA256
	ota.SignatureType = &signatureType
	ota.AdditionalInfo = additional
	return nil
}
