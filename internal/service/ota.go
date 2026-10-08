package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type OTA struct{}

func usesDevicePullOTA(otapackage *model.OtaUpgradePackage) bool {
	if otapackage == nil || otapackage.AdditionalInfo == nil || *otapackage.AdditionalInfo == "" {
		return false
	}
	var additional struct {
		DeliveryProtocol   string `json:"deliveryProtocol"`
		OrchestrationRoute string `json:"orchestrationRoute"`
	}
	return json.Unmarshal([]byte(*otapackage.AdditionalInfo), &additional) == nil &&
		(additional.DeliveryProtocol == "ygsoul_http" || additional.OrchestrationRoute == "device_integration")
}

func devicePullOTADescription(otapackage *model.OtaUpgradePackage) string {
	if otapackage != nil && otapackage.AdditionalInfo != nil {
		var additional struct {
			OrchestrationRoute string `json:"orchestrationRoute"`
		}
		if json.Unmarshal([]byte(*otapackage.AdditionalInfo), &additional) == nil && additional.OrchestrationRoute == "device_integration" {
			return "等待设备交互服务编排"
		}
	}
	return "等待设备主动检查升级"
}

func validateOTAUpdate(req *model.UpdateOTAUpgradePackageReq, current *model.OtaUpgradePackage) error {
	if req.PackageUrl != nil && (current.PackageURL == nil || *req.PackageUrl != *current.PackageURL) {
		return fmt.Errorf("OTA 固件地址不可修改，请重新上传 TOS 固件包")
	}
	if req.AdditionalInfo != nil && (current.AdditionalInfo == nil || *req.AdditionalInfo != *current.AdditionalInfo) {
		return fmt.Errorf("OTA TOS 对象信息不可修改")
	}
	return nil
}

func buildOTAInformParams(otapackage *model.OtaUpgradePackage, downloadAddress string, size int64) map[string]interface{} {
	packageURL, signature, signatureType, module := "", "", "", ""
	if otapackage.PackageURL != nil {
		if strings.HasPrefix(*otapackage.PackageURL, "https://") {
			packageURL = *otapackage.PackageURL
		} else {
			packageURL = strings.TrimRight(downloadAddress, "/") + strings.TrimPrefix(*otapackage.PackageURL, ".")
		}
	}
	if otapackage.Signature != nil {
		signature = *otapackage.Signature
	}
	if otapackage.SignatureType != nil {
		signatureType = *otapackage.SignatureType
	}
	if otapackage.Module != nil {
		module = *otapackage.Module
	}
	return map[string]interface{}{
		"version": otapackage.Version, "size": strconv.FormatInt(size, 10), "url": packageURL,
		"signMethod": signatureType, "sign": signature, "module": module,
	}
}

func (*OTA) CreateOTAUpgradePackage(req *model.CreateOTAUpgradePackageReq, tenantID string) error {
	var stored struct {
		ObjectKey string `json:"tosObjectKey"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		Receipt   string `json:"tosReceipt"`
	}
	if req.AdditionalInfo == nil || json.Unmarshal([]byte(*req.AdditionalInfo), &stored) != nil {
		return fmt.Errorf("OTA 固件必须先上传并校验 TOS")
	}
	hashBytes, hashErr := hex.DecodeString(stored.SHA256)
	if hashErr != nil || len(hashBytes) != 32 || stored.Size <= 0 || req.PackageUrl == nil || !strings.HasPrefix(*req.PackageUrl, "https://") {
		return fmt.Errorf("OTA 固件必须先上传并校验 TOS")
	}
	if !validOTAFirmwareReceipt(os.Getenv("YOMI_INTERNAL_EVENT_TOKEN"), stored.ObjectKey, *req.PackageUrl, stored.SHA256, stored.Size, stored.Receipt) {
		return fmt.Errorf("OTA 固件缺少有效的 TOS 上传凭据")
	}
	config, err := dal.GetDeviceConfigByID(req.DeviceConfigID)
	if err != nil {
		return err
	}
	product := strings.ToLower(config.Name)
	productKey := ""
	if strings.Contains(product, "esp32s3") {
		productKey = "esp32s3"
	}
	if strings.Contains(product, "a100") {
		productKey = "a100"
	}
	expectedKey := fmt.Sprintf("firmware/%s/%s/xiaozhi-%s.bin", productKey, req.Version, req.Version)
	if productKey == "" || stored.ObjectKey != expectedKey || !strings.HasSuffix(*req.PackageUrl, "/"+expectedKey) {
		return fmt.Errorf("OTA 固件 TOS 对象键与产品/版本不匹配")
	}
	additionalInfo, err := prepareTOSOTAAdditionalInfo(*req.AdditionalInfo, stored.ObjectKey, stored.SHA256, stored.Receipt, stored.Size, productKey)
	if err != nil {
		return err
	}
	req.AdditionalInfo = additionalInfo
	if req.SignatureType == nil || !strings.EqualFold(*req.SignatureType, "SHA256") {
		return fmt.Errorf("TOS 固件签名算法必须为 SHA256")
	}
	var ota = model.OtaUpgradePackage{}
	ota.ID = uuid.New()
	ota.Name = req.Name
	ota.Version = req.Version
	ota.TargetVersion = req.TargetVersion
	// 临时注释
	ota.DeviceConfigID = req.DeviceConfigID
	ota.Module = req.Module
	ota.PackageType = *req.PackageType
	ota.SignatureType = req.SignatureType

	ota.Signature = &stored.SHA256

	ota.AdditionalInfo = req.AdditionalInfo
	defaultAdditionalInfo := "{}"
	if req.AdditionalInfo == nil || *req.AdditionalInfo == "" {
		ota.AdditionalInfo = &defaultAdditionalInfo
	}
	ota.Description = req.Description
	ota.PackageURL = req.PackageUrl
	ota.TenantID = &tenantID

	t := time.Now().UTC()
	ota.CreatedAt = t
	ota.UpdatedAt = &t
	ota.Remark = req.Remark
	return dal.CreateOtaUpgradePackage(&ota, func() error {
		return verifyOTAObjectAvailable(*req.PackageUrl, stored.Size)
	})
}

func verifyOTAObjectAvailable(publicURL string, expectedSize int64) error {
	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || expectedSize <= 0 {
		return fmt.Errorf("TOS 固件地址或大小无效")
	}
	req, err := http.NewRequest(http.MethodHead, publicURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("TOS 固件公网校验失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength != expectedSize {
		return fmt.Errorf("TOS 固件公网地址不可用或大小不匹配")
	}
	return nil
}

func validOTAFirmwareReceipt(token, objectKey, publicURL, digest string, size int64, receipt string) bool {
	if token == "" || receipt == "" || size <= 0 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%s\n%d", objectKey, publicURL, digest, size)
	expected, err := hex.DecodeString(receipt)
	return err == nil && hmac.Equal(expected, mac.Sum(nil))
}

func prepareTOSOTAAdditionalInfo(raw, objectKey, digest, receipt string, size int64, productKey string) (*string, error) {
	var additional map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &additional); err != nil || additional == nil {
		return nil, fmt.Errorf("OTA 固件附加信息无效")
	}
	additional["tosObjectKey"] = objectKey
	additional["sha256"] = digest
	additional["size"] = size
	additional["tosReceipt"] = receipt
	additional["deliveryProtocol"] = "ygsoul_http"
	if productKey == "a100" {
		additional["orchestrationRoute"] = "device_integration"
	}
	encoded, err := json.Marshal(additional)
	if err != nil {
		return nil, err
	}
	value := string(encoded)
	return &value, nil
}

func isVerifiedTOSOTAPackage(ota *model.OtaUpgradePackage, token string) bool {
	if ota == nil || ota.AdditionalInfo == nil || ota.PackageURL == nil {
		return false
	}
	var metadata struct {
		ObjectKey string `json:"tosObjectKey"`
		SHA256    string `json:"sha256"`
		Receipt   string `json:"tosReceipt"`
		Size      int64  `json:"size"`
	}
	if json.Unmarshal([]byte(*ota.AdditionalInfo), &metadata) != nil {
		return false
	}
	hash, err := hex.DecodeString(metadata.SHA256)
	if err != nil || len(hash) != sha256.Size || metadata.Size <= 0 || !strings.HasPrefix(metadata.ObjectKey, "firmware/") {
		return false
	}
	parsed, err := url.Parse(*ota.PackageURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || !strings.HasSuffix(parsed.Path, "/"+metadata.ObjectKey) {
		return false
	}
	return validOTAFirmwareReceipt(token, metadata.ObjectKey, *ota.PackageURL, metadata.SHA256, metadata.Size, metadata.Receipt)
}

func (*OTA) UpdateOTAUpgradePackage(req *model.UpdateOTAUpgradePackageReq, tenantID string) error {

	oldota, err := dal.GetOtaUpgradePackageByIDAndTenant(req.Id, tenantID)
	if err != nil {
		return err
	}
	if err := validateOTAUpdate(req, oldota); err != nil {
		return err
	}

	var ota = model.OtaUpgradePackage{}
	ota.ID = req.Id

	ota.Name = req.Name
	// ota.Version = req.Version
	// ota.TargetVersion = req.TargetVersion
	// 临时注释
	// ota.DeviceConfigsID = req.DeviceConfigsID
	// ota.Module = req.Module
	// ota.PackageType = *req.PackageType
	// ota.SignatureType = req.SignatureType
	ota.AdditionalInfo = oldota.AdditionalInfo
	ota.Description = req.Description
	ota.PackageURL = oldota.PackageURL
	ota.Signature = oldota.Signature

	t := time.Now().UTC()
	ota.UpdatedAt = &t
	ota.Remark = req.Remark
	info, err := dal.UpdateOtaUpgradePackage(&ota, tenantID)
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return nil
}

func (*OTA) DeleteOTAUpgradePackage(packageId, tenantID string) error {
	ota, err := dal.GetOtaUpgradePackageByIDAndTenant(packageId, tenantID)
	if err != nil {
		return err
	}
	var info struct {
		ObjectKey string `json:"tosObjectKey"`
	}
	if ota.AdditionalInfo != nil {
		_ = json.Unmarshal([]byte(*ota.AdditionalInfo), &info)
	}
	if info.ObjectKey == "" {
		return fmt.Errorf("缺少 TOS 对象键，拒绝删除 OTA 包")
	}
	return dal.DeleteOtaUpgradePackageAndTOS(packageId, tenantID, info.ObjectKey, func() error {
		return deleteOTAFirmwareFromTOS(info.ObjectKey)
	})
}

func deleteOTAFirmwareFromTOS(objectKey string) error {
	endpoint, token := os.Getenv("YOMI_OTA_FIRMWARE_URL"), os.Getenv("YOMI_INTERNAL_EVENT_TOKEN")
	parsed, err := url.ParseRequestURI(endpoint)
	private := parsed != nil && parsed.Scheme == "http" && (parsed.Hostname() == "host.docker.internal" || parsed.Hostname() == "yomi-server")
	if err != nil || parsed == nil || parsed.Host == "" || (!private && parsed.Scheme != "https") || token == "" {
		return fmt.Errorf("TOS 固件删除服务未配置")
	}
	if !strings.HasPrefix(objectKey, "firmware/") || strings.Contains(objectKey, "..") {
		return fmt.Errorf("无效的 TOS 固件对象键")
	}
	body, _ := json.Marshal(map[string]string{"objectKey": objectKey})
	req, err := http.NewRequest(http.MethodDelete, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Yomi-Internal-Token", token)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || result.Code != 0 {
		return fmt.Errorf("TOS 固件删除失败")
	}
	return nil
}

func dispatchOTADevice(firmwareEndpoint, token, deviceNumber, requestID, packageID string) error {
	endpoint, err := url.ParseRequestURI(firmwareEndpoint)
	if err != nil || endpoint.Host == "" || !strings.HasSuffix(endpoint.Path, "/firmware") || token == "" {
		return fmt.Errorf("设备 OTA 派发服务未配置")
	}
	ip := net.ParseIP(endpoint.Hostname())
	private := endpoint.Scheme == "http" && (endpoint.Hostname() == "host.docker.internal" || endpoint.Hostname() == "yomi-server" || (ip != nil && ip.IsLoopback()))
	if endpoint.Scheme != "https" && !private {
		return fmt.Errorf("设备 OTA 派发服务必须使用 HTTPS")
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/firmware") + "/devices/" + url.PathEscape(deviceNumber) + "/dispatch"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	body, _ := json.Marshal(map[string]string{"requestId": requestID, "packageId": packageID})
	req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Yomi-Internal-Token", token)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Code int `json:"code"`
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK || result.Code != 0 || result.Data.Status != "ACCEPTED" {
		return fmt.Errorf("设备 OTA 派发未被接受")
	}
	return nil
}

func (*OTA) GetOTAUpgradePackageListByPage(req *model.GetOTAUpgradePackageLisyByPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetOtaUpgradePackageListByPage(req, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	packageListRspMap := make(map[string]interface{})
	packageListRspMap["total"] = total
	packageListRspMap["list"] = list
	return packageListRspMap, nil

}

func (o *OTA) CreateOTAUpgradeTask(req *model.CreateOTAUpgradeTaskReq) error {
	tasks, err := dal.CreateOTAUpgradeTaskWithDetail(req)
	if err == nil && req.Source != "device_integration" {
		go func() {
			for _, t := range tasks {
				o.PushOTAUpgradePackage(t)
			}
		}()
	}
	return err
}

func (*OTA) DeleteOTAUpgradeTask(id string) error {
	err := dal.DeleteOTAUpgradeTask(id)
	return err
}

func (*OTA) GetOTAUpgradeTaskListByPage(req *model.GetOTAUpgradeTaskListByPageReq) (map[string]interface{}, error) {
	total, list, err := dal.GetOtaUpgradeTaskListByPage(req)
	if err != nil {
		return nil, err
	}
	dataMap := make(map[string]interface{})
	dataMap["total"] = total
	dataMap["list"] = list
	return dataMap, nil
}

func (*OTA) GetOTAUpgradeTaskDetailListByPage(req *model.GetOTAUpgradeTaskDetailReq) (map[string]interface{}, error) {
	total, list, statistics, err := dal.GetOtaUpgradeTaskDetailListByPage(req)
	if err != nil {
		return nil, err
	}
	dataMap := make(map[string]interface{})
	dataMap["total"] = total
	dataMap["statistics"] = statistics
	dataMap["list"] = list
	return dataMap, nil
}

// 设备状态修改(请求参数1-取消升级 2-重新升级)
// 1-待推送 2-已推送 3-升级中 修改为已取消
// 5-升级失败 修改为待推送
// 4-升级成功 6-已取消 不修改
func (o *OTA) UpdateOTAUpgradeTaskStatus(req *model.UpdateOTAUpgradeTaskStatusReq) error {
	taskDetail, err := query.OtaUpgradeTaskDetail.Where(query.OtaUpgradeTaskDetail.ID.Eq(req.Id)).First()
	if err != nil {
		return err
	}
	// 4-升级成功 不可改；6-已取消 允许重新升级
	if taskDetail.Status == 4 {
		return fmt.Errorf("the task status cannot be modified")
	}
	if taskDetail.Status == 6 && req.Action != 1 {
		return fmt.Errorf("the task status cannot be modified")
	}
	// 升级成功的任务不能取消升级
	if req.Action == 6 && taskDetail.Status == 5 {
		return fmt.Errorf("the task status cannot be modified")
	}
	// 1-待推送 2-已推送 3-升级中 不能重新升级
	if req.Action == 1 && taskDetail.Status <= 3 {
		return fmt.Errorf("the task is upgrading")
	}
	t := time.Now().UTC()
	if req.Action == 6 {
		//取消升级
		taskDetail.Status = 6
		taskDetail.UpdatedAt = &t
		desc := "手动取消升级"
		taskDetail.StatusDescription = &desc
		_, err := query.OtaUpgradeTaskDetail.Updates(taskDetail)
		return err
	}
	if req.Action == 1 {
		desc := "手动开始重新升级"
		startStep := int16(0)
		//重新升级
		taskDetail.Status = 1
		taskDetail.UpdatedAt = &t
		taskDetail.StatusDescription = &desc
		taskDetail.Step = &startStep

		_, err := query.OtaUpgradeTaskDetail.Updates(taskDetail)
		if err != nil {
			return err
		}
		// 重新升级后推送升级包
		err = o.PushOTAUpgradePackage(taskDetail)
		return err
	}

	return err
}
func (*OTA) PushOTAUpgradePackage(taskDetail *model.OtaUpgradeTaskDetail) error {
	// 查看设备是否在线
	device := &model.Device{}
	device, err := query.Device.Where(query.Device.ID.Eq(taskDetail.DeviceID)).First()
	if err != nil {
		return err
	}
	if device.IsOnline != 1 {
		//修改设备升级任务信息
		taskDetail.Status = 5
		desc := "设备离线"
		taskDetail.StatusDescription = &desc
		t := time.Now().UTC()
		taskDetail.UpdatedAt = &t
		_, err := query.OtaUpgradeTaskDetail.Updates(taskDetail)
		if err != nil {
			return err
		}
		return fmt.Errorf("the device is offline")
	}
	// 查看设备是否有其他升级中的任务（必须排除当前刚插入的明细，否则永远“上次升级未完成”）
	count, err := query.OtaUpgradeTaskDetail.Where(
		query.OtaUpgradeTaskDetail.DeviceID.Eq(taskDetail.DeviceID),
		query.OtaUpgradeTaskDetail.Status.Lt(4),
		query.OtaUpgradeTaskDetail.ID.Neq(taskDetail.ID),
	).Count()
	if err != nil {
		return err
	}
	if shouldBlockForUnfinishedOTA(int(count)) {
		//修改设备升级任务信息
		taskDetail.Status = 5
		desc := "上次升级未完成"
		taskDetail.StatusDescription = &desc
		t := time.Now().UTC()
		taskDetail.UpdatedAt = &t
		_, err := query.OtaUpgradeTaskDetail.Updates(taskDetail)
		if err != nil {
			return err
		}
		return fmt.Errorf("the device is upgrading")
	}
	// 推送升级包
	taskQuery, err := query.OtaUpgradeTask.Where(query.OtaUpgradeTask.ID.Eq(taskDetail.OtaUpgradeTaskID)).First()
	if err != nil {
		return err
	}
	otapackage, err := query.OtaUpgradePackage.Where(query.OtaUpgradePackage.ID.Eq(resolveOTAPackageID(taskQuery.ID, taskQuery.OtaUpgradePackageID))).First()
	if err != nil {
		return err
	}
	if err := ensureLegacyOTAPackageHasTOSReceipt(otapackage); err != nil {
		taskDetail.Status = 5
		desc := "旧 TOS 固件迁移校验失败"
		taskDetail.StatusDescription = &desc
		t := time.Now().UTC()
		taskDetail.UpdatedAt = &t
		_, _ = query.OtaUpgradeTaskDetail.Updates(taskDetail)
		return err
	}
	if !isVerifiedTOSOTAPackage(otapackage, os.Getenv("YOMI_INTERNAL_EVENT_TOKEN")) {
		return fmt.Errorf("OTA 包缺少有效 TOS 对象或上传凭据，拒绝回退到 ThingsPanel 文件服务")
	}
	if !usesDevicePullOTA(otapackage) {
		return fmt.Errorf("OTA 包未配置设备交互服务下发，拒绝回退到 ThingsPanel 文件服务")
	}
	t := time.Now().UTC()
	desc := devicePullOTADescription(otapackage)
	if err := dispatchOTADevice(os.Getenv("YOMI_OTA_FIRMWARE_URL"), os.Getenv("YOMI_INTERNAL_EVENT_TOKEN"), device.DeviceNumber, taskDetail.ID, otapackage.ID); err != nil {
		taskDetail.Status = 5
		desc = "设备交互服务 OTA 派发失败"
		taskDetail.StatusDescription = &desc
		taskDetail.UpdatedAt = &t
		_, _ = query.OtaUpgradeTaskDetail.Updates(taskDetail)
		return err
	}
	taskDetail.Status = 2
	desc = "已通知设备检查 TOS 固件"
	taskDetail.StatusDescription = &desc
	taskDetail.UpdatedAt = &t
	_, err = query.OtaUpgradeTaskDetail.Updates(taskDetail)
	return err
}

func (*OTA) ApplyOTAProgress(raw []byte) error {
	progress, err := parseOTAProgressPayload(raw)
	if err != nil {
		logrus.WithError(err).WithField("payload", string(raw)).Error("[OTA] bad progress payload")
		return err
	}
	status, ok := nextOTAStatusFromStep(progress.Step)
	if !ok {
		return fmt.Errorf("invalid ota step %d", progress.Step)
	}
	var device *model.Device
	if progress.DeviceKey != "" {
		device, err = dal.GetDeviceByID(progress.DeviceKey)
		if err != nil {
			device, err = dal.GetDeviceByDeviceNumber(progress.DeviceKey)
		}
	}
	if err != nil || device == nil {
		logrus.WithField("device_key", progress.DeviceKey).Error("[OTA] progress device not found")
		return fmt.Errorf("ota progress device not found: %s", progress.DeviceKey)
	}
	detail, err := query.OtaUpgradeTaskDetail.Where(
		query.OtaUpgradeTaskDetail.DeviceID.Eq(device.ID),
		query.OtaUpgradeTaskDetail.Status.In(otaInProgressStatuses()...),
	).First()
	if err != nil {
		detail, err = query.OtaUpgradeTaskDetail.Where(
			query.OtaUpgradeTaskDetail.DeviceID.Eq(device.ID),
		).Order(query.OtaUpgradeTaskDetail.UpdatedAt.Desc()).First()
		if err != nil {
			logrus.WithError(err).WithField("device_id", device.ID).Error("[OTA] no task for progress")
			return err
		}
		logrus.WithFields(logrus.Fields{
			"device_id": device.ID, "task_detail": detail.ID, "prev_status": detail.Status, "step": progress.Step,
		}).Warn("[OTA] no in-progress task, applying to latest detail")
	}
	if detail.Status == 4 && progress.Step != 100 {
		return nil
	}
	detail.Status = status
	step := int16(progress.Step)
	detail.Step = &step
	desc := progress.Desc
	detail.StatusDescription = &desc
	now := time.Now().UTC()
	detail.UpdatedAt = &now
	if _, err = query.OtaUpgradeTaskDetail.Where(query.OtaUpgradeTaskDetail.ID.Eq(detail.ID)).Updates(detail); err != nil {
		return err
	}
	logRow := buildOTAProgressLog(detail.ID, device.ID, progress, status, string(raw))
	if err = dal.InsertOTAProgressLog(logRow.ID, logRow.TaskDetailID, logRow.DeviceID, logRow.Step, logRow.Status, logRow.Description, logRow.Payload); err != nil {
		logrus.WithError(err).Error("[OTA] persist progress log failed")
	}
	logrus.WithFields(logrus.Fields{
		"stage": "ota_progress", "device_id": device.ID, "task_detail": detail.ID, "step": progress.Step, "status": status, "log_id": logRow.ID,
	}).Info("[OTA] progress applied")
	return nil
}

func (*OTA) ListOTAProgressLogs(detailID, tenantID string) ([]dal.OTAProgressLogRow, error) {
	return dal.ListOTAProgressLogs(detailID, tenantID)
}
