package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	query "project/internal/query"
	mqtt "project/mqtt"
	"project/pkg/common"
	global "project/pkg/global"
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
		(additional.DeliveryProtocol == "xiaozhi_http" || additional.OrchestrationRoute == "device_integration")
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

func buildOTAInformParams(otapackage *model.OtaUpgradePackage, downloadAddress string, size int64) map[string]interface{} {
	packageURL, signature, signatureType, module := "", "", "", ""
	if otapackage.PackageURL != nil {
		packageURL = strings.TrimRight(downloadAddress, "/") + strings.TrimPrefix(*otapackage.PackageURL, ".")
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

	// 生成文件签名
	fileurl := *req.PackageUrl
	filepath := strings.Replace(fileurl, "/api/v1/ota/download", "", 1)
	signature, err := utils.FileSign(filepath, *req.SignatureType)
	if err != nil {
		return err
	}
	ota.Signature = &signature

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
	err = dal.CreateOtaUpgradePackage(&ota)
	return err
}

func (*OTA) UpdateOTAUpgradePackage(req *model.UpdateOTAUpgradePackageReq) error {

	oldota, err := dal.GetOtaUpgradePackageByID(req.Id)
	if err != nil {
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
	ota.AdditionalInfo = req.AdditionalInfo
	ota.Description = req.Description
	ota.PackageURL = req.PackageUrl
	if req.PackageUrl != oldota.PackageURL {
		// 生成文件签名
		fileurl := *req.PackageUrl
		filepath := strings.Replace(fileurl, "/api/v1/ota/download", "", 1)
		signature, err := utils.FileSign(filepath, *req.SignatureType)
		if err != nil {
			return err
		}
		ota.Signature = &signature
	}

	t := time.Now().UTC()
	ota.UpdatedAt = &t
	ota.Remark = req.Remark
	info, err := dal.UpdateOtaUpgradePackage(&ota)
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return nil
}

func (*OTA) DeleteOTAUpgradePackage(packageId string) error {
	err := dal.DeleteOtaUpgradePackage(packageId)
	return err
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
	if err == nil {
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
	if usesDevicePullOTA(otapackage) {
		t := time.Now().UTC()
		desc := devicePullOTADescription(otapackage)
		taskDetail.Status = 1
		taskDetail.StatusDescription = &desc
		taskDetail.UpdatedAt = &t
		_, err := query.OtaUpgradeTaskDetail.Updates(taskDetail)
		return err
	}
	var otamsg = make(map[string]interface{})
	// 获取随机九位数字并转换为字符串
	randNum, err := common.GetRandomNineDigits()
	if err != nil {
		return err
	}
	otamsg["id"] = randNum
	otamsg["code"] = "200"
	packageSize := int64(0)
	if otapackage.PackageURL != nil {
		filePath := "." + strings.Replace(*otapackage.PackageURL, "/api/v1/ota/download", "", 1)
		info, statErr := os.Stat(filePath)
		if statErr != nil {
			return fmt.Errorf("ota package is unavailable: %w", statErr)
		}
		packageSize = info.Size()
	}
	otamsgparams := buildOTAInformParams(otapackage, global.OtaAddress, packageSize)
	var m map[string]interface{}
	if otapackage.AdditionalInfo != nil && *otapackage.AdditionalInfo != "" {
		if err = json.Unmarshal([]byte(*otapackage.AdditionalInfo), &m); err != nil {
			logrus.Error(err)
		}
	}
	otamsgparams["extData"] = m
	otamsg["params"] = otamsgparams
	palyload, json_err := json.Marshal(otamsg)
	if json_err != nil {
		logrus.Error(json_err)
	} else {
		t := time.Now().UTC()
		taskDetail.UpdatedAt = &t
		if pubErr := mqtt.PublishOTAInform(device.DeviceNumber, palyload); pubErr != nil {
			taskDetail.Status = 1
			desc := "通知发送失败: " + pubErr.Error()
			taskDetail.StatusDescription = &desc
			_, _ = query.OtaUpgradeTaskDetail.Updates(taskDetail)
			logrus.WithError(pubErr).WithFields(logrus.Fields{
				"stage": "ota_inform_publish", "device_id": device.ID, "device_number": device.DeviceNumber, "task_detail": taskDetail.ID,
			}).Error("[OTA] inform publish failed")
			return pubErr
		}
		taskDetail.Status = 2
		desc := "已通知设备"
		taskDetail.StatusDescription = &desc
		if _, err := query.OtaUpgradeTaskDetail.Updates(taskDetail); err != nil {
			return err
		}
		logrus.WithFields(logrus.Fields{
			"stage": "ota_inform_publish", "device_number": device.DeviceNumber,
			"topics": mqtt.OTAInformTopics(device.DeviceNumber), "task_detail": taskDetail.ID, "payload": string(palyload),
		}).Info("[OTA] inform published")
	}

	return nil
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

func (*OTA) ListOTAProgressLogs(detailID string) ([]dal.OTAProgressLogRow, error) {
	return dal.ListOTAProgressLogs(detailID)
}
