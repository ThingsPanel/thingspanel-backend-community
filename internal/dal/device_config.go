package dal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"gorm.io/gen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/sirupsen/logrus"
)

var ErrNativeBindingRevisionConflict = errors.New("native binding revision conflict")

func CreateDeviceConfig(deviceconfig *model.DeviceConfig) error {
	return query.DeviceConfig.Create(deviceconfig)
}

// 修改配置模板id
func UpdateDeviceConfigTemplateID(id string, templateID *string) error {
	// nil值也要更新
	_, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).Update(query.DeviceConfig.DeviceTemplateID, templateID)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func UpdateDeviceConfig(id string, condsMap map[string]interface{}) error {
	config, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).First()
	if err != nil {
		return err
	}
	return UpdateDeviceConfigForTenant(id, config.TenantID, condsMap)
}

func UpdateDeviceConfigForTenant(id, tenantID string, condsMap map[string]interface{}) error {
	condsMap["updated_at"] = time.Now().UTC()
	return updateDeviceConfigForOwner(id, tenantID, condsMap, nil)
}

func UpdateDeviceConfigWithBindingCAS(id, tenantID string, expectedRevision int, condsMap map[string]interface{}) error {
	condsMap["updated_at"] = time.Now().UTC()
	condsMap["native_binding_revision"] = gorm.Expr("native_binding_revision + 1")
	return updateDeviceConfigForOwner(id, tenantID, condsMap, &expectedRevision)
}

func DeleteDeviceConfig(id string) error {
	config, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).First()
	if err != nil {
		return err
	}
	return DeleteDeviceConfigForTenant(id, config.TenantID)
}

func DeleteDeviceConfigForTenant(id, tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return ErrDeviceBindingInvalid
	}
	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	c, d := tx.DeviceConfig, tx.Device
	_, err := c.Clauses(clause.Locking{Strength: "UPDATE"}).Where(c.ID.Eq(id), c.TenantID.Eq(tenantID)).First()
	if err == nil {
		var count int64
		count, err = d.Where(d.DeviceConfigID.Eq(id)).Count()
		if err == nil && count > 0 {
			err = &DeviceConfigurationInUseError{Count: count}
		}
	}
	if err == nil {
		info, deleteErr := c.Where(c.ID.Eq(id), c.TenantID.Eq(tenantID)).Delete()
		err = deleteErr
		if err == nil && info.RowsAffected != 1 {
			err = ErrDeviceConfigurationConflict
		}
	}
	return finishDeviceBinding(tx, err)
}

func GetDeviceConfigByID(id string) (*model.DeviceConfig, error) {
	// 1. 先从 Redis 缓存读取
	cacheKey := id + "_config"
	result, err := global.REDIS.Get(context.Background(), cacheKey).Result()
	if err == nil {
		// 缓存命中
		var deviceconfig model.DeviceConfig
		if err := json.Unmarshal([]byte(result), &deviceconfig); err == nil {
			return &deviceconfig, nil
		}
		// JSON 反序列化失败，继续从数据库加载
	}

	// 2. 缓存未命中，从数据库加载
	deviceconfig, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	if deviceconfig == nil {
		return nil, fmt.Errorf("deviceconfig not found: %s", id)
	}

	// 3. 将结果写入缓存（永久有效）
	jsonData, err := json.Marshal(deviceconfig)
	if err == nil {
		err = global.REDIS.Set(context.Background(), cacheKey, jsonData, 0).Err()
		if err != nil {
			// 缓存写入失败不影响主流程，只记录日志
			logrus.WithFields(logrus.Fields{
				"module":    "dal.device_config",
				"cache_key": cacheKey,
				"error":     err.Error(),
			}).Warn("failed to cache device config")
		}
	}

	return deviceconfig, nil
}

func GetDeviceConfigListByPage(deviceconfig *model.GetDeviceConfigListByPageReq, claims *utils.UserClaims) (int64, interface{}, error) {
	q := query.DeviceConfig
	var count int64
	var data []model.DeviceConfigRsp
	var deviceconfigList []*model.DeviceConfig
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(claims.TenantID))

	if deviceconfig.DeviceTemplateId != nil && *deviceconfig.DeviceTemplateId != "" {
		queryBuilder = queryBuilder.Where(q.DeviceTemplateID.Eq(*deviceconfig.DeviceTemplateId))
	}
	if deviceconfig.DeviceType != nil && *deviceconfig.DeviceType != "" {
		queryBuilder = queryBuilder.Where(q.DeviceType.Eq(*deviceconfig.DeviceType))
	}
	if deviceconfig.ProtocolType != nil && *deviceconfig.ProtocolType != "" {
		queryBuilder = queryBuilder.Where(q.ProtocolType.Eq(*deviceconfig.ProtocolType))
	}
	if deviceconfig.Name != nil && *deviceconfig.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *deviceconfig.Name)))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceconfigList, err
	}

	if deviceconfig.Page != 0 && deviceconfig.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(deviceconfig.PageSize)
		queryBuilder = queryBuilder.Offset((deviceconfig.Page - 1) * deviceconfig.PageSize)
	}
	queryBuilder.Order(q.CreatedAt.Desc())
	deviceconfigList, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)
		return count, deviceconfigList, err
	}
	for i := range deviceconfigList {
		c, err := query.Device.
			Where(query.Device.ActivateFlag.Eq("active")).
			Where(query.Device.DeviceConfigID.Eq(deviceconfigList[i].ID)).Count()
		if err != nil {
			logrus.Error(err)
		}
		data = append(data, model.DeviceConfigRsp{DeviceConfig: deviceconfigList[i], DeviceCount: c})
	}

	return count, data, err
}

// 获取设备配置下拉菜单
func GetDeviceConfigSelectList(deviceConfigName *string, tenantID string, deviceType *string, protocolType *string) (any, error) {
	q := query.DeviceConfig
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	if deviceConfigName != nil {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *deviceConfigName)))
	}
	if deviceType != nil {
		queryBuilder = queryBuilder.Where(q.DeviceType.Eq(*deviceType))
	}
	if protocolType != nil {
		queryBuilder = queryBuilder.Where(q.ProtocolType.Eq(*protocolType))
	}
	var data []map[string]interface{}
	err := queryBuilder.Select(q.ID, q.Name).Order(q.CreatedAt.Desc()).Scan(&data)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return data, err
}

type DeviceConfigQuery struct{}

func (DeviceConfigQuery) First(ctx context.Context, option ...gen.Condition) (info *model.DeviceConfig, err error) {
	info, err = query.DeviceConfig.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func GetDeviceConfigByIDForTenant(ctx context.Context, id, tenantID string) (*model.DeviceConfig, error) {
	info, err := query.DeviceConfig.WithContext(ctx).
		Where(query.DeviceConfig.ID.Eq(id), query.DeviceConfig.TenantID.Eq(tenantID)).
		First()
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("device config not found for tenant")
	}
	return info, nil
}

func (DeviceConfigQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.DeviceConfig, err error) {
	list, err = query.DeviceConfig.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

type DeviceConfigVo struct{}

func (DeviceConfigVo) PoToVo(deviceConfigInfo *model.DeviceConfig) (info *model.DeviceConfigsRes) {
	info = &model.DeviceConfigsRes{
		ID:         deviceConfigInfo.ID,
		Name:       deviceConfigInfo.Name,
		DeviceType: deviceConfigInfo.DeviceType,
		CreatedAt:  deviceConfigInfo.CreatedAt,
		UpdatedAt:  deviceConfigInfo.UpdatedAt,
	}
	if deviceConfigInfo.DeviceTemplateID != nil {
		info.DeviceTemplateID = *deviceConfigInfo.DeviceTemplateID
	}
	if deviceConfigInfo.ProtocolType != nil {
		info.ProtocolType = *deviceConfigInfo.ProtocolType
	}
	if deviceConfigInfo.VoucherType != nil {
		info.VoucherType = *deviceConfigInfo.VoucherType
	}
	if deviceConfigInfo.ProtocolConfig != nil {
		info.ProtocolConfig = *deviceConfigInfo.ProtocolConfig
	}
	if deviceConfigInfo.DeviceConnType != nil {
		info.DeviceConnType = *deviceConfigInfo.DeviceConnType
	}
	if deviceConfigInfo.AdditionalInfo != nil {
		info.VoucherType = *deviceConfigInfo.AdditionalInfo
	}
	if deviceConfigInfo.Description != nil {
		info.Description = *deviceConfigInfo.Description
	}
	if deviceConfigInfo.Remark != nil {
		info.Remark = *deviceConfigInfo.Remark
	}
	return
}

// func GetDeviceOnline(ctx context.Context, deviceOnlines []model.DeviceOnline) (map[string]int, error) {
// 	var (
// 		result               = make(map[string]int, 0)
// 		deviceConfigIds      []string
// 		deviveConfigOtherMap = make(map[string]model.DeviceConfigOtherConfig, 0)
// 		deviceIds            []string
// 		deviceMap            = make(map[string]string, 0)
// 	)

// 	if len(deviceOnlines) == 0 {
// 		return result, nil
// 	}
// 	for _, v := range deviceOnlines {
// 		if v.DeviceConfigId == nil || *v.DeviceConfigId == "" {
// 			continue
// 		}
// 		deviceIds = append(deviceIds, v.DeviceId)
// 		deviceConfigIds = append(deviceConfigIds, *v.DeviceConfigId)
// 		deviceMap[v.DeviceId] = *v.DeviceConfigId
// 	}
// 	list, err := query.DeviceConfig.WithContext(ctx).Where(query.DeviceConfig.ID.In(deviceConfigIds...)).Find()
// 	if err != nil {
// 		return result, nil
// 	}
// 	for _, v := range list {
// 		if v.OtherConfig == nil || *v.OtherConfig == "" {
// 			continue
// 		}
// 		var config model.DeviceConfigOtherConfig
// 		err = json.Unmarshal([]byte(*v.OtherConfig), &config)
// 		if err != nil {
// 			continue
// 		}
// 		deviveConfigOtherMap[v.ID] = config
// 	}
// 	t := query.TelemetryCurrentData
// 	rows, err := t.WithContext(ctx).Where(t.DeviceID.In(deviceIds...)).Group(t.DeviceID).Select(t.DeviceID, t.T.Max().As("ts")).Find()
// 	if err != nil {
// 		return result, nil
// 	}
// 	now := time.Now().UTC()
// 	for _, v := range rows {
// 		logrus.Warning(v.DeviceID)
// 		var (
// 			deviceConfigId string
// 			ok             bool
// 		)
// 		if deviceConfigId, ok = deviceMap[v.DeviceID]; !ok {
// 			continue
// 		}
// 		if config, ok := deviveConfigOtherMap[deviceConfigId]; ok {

// 			if config.Heartbeat > 0 {
// 				//当前时间-最近一次遥测时间 大于心跳秒数  表示离线
// 				if now.Sub(v.T).Seconds() > float64(config.Heartbeat) {
// 					result[v.DeviceID] = 0
// 				} else {
// 					result[v.DeviceID] = 1
// 				}
// 				continue
// 			}
// 			//设置了超时时间 当前时间-最近一次遥测时间 大于超时时间（分）  表示离线
// 			if config.OnlineTimeout > 0 {
// 				if now.Sub(v.T).Minutes() > float64(config.OnlineTimeout) {
// 					result[v.DeviceID] = 0
// 				} else {
// 					result[v.DeviceID] = 1
// 				}

// 			}
// 		}
// 	}
// 	return result, nil
// }

// 修改凭证类型
func UpdateDeviceConfigVoucherType(id string, voucherType *string) error {
	// nil值也要更新
	_, err := query.DeviceConfig.Where(query.DeviceConfig.ID.Eq(id)).Update(query.DeviceConfig.VoucherType, voucherType)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

// GetDeviceConfigByTemplateID retrieves a device config by device template ID.
func GetDeviceConfigByTemplateID(templateID string) (*model.DeviceConfig, error) {
	dc, err := query.DeviceConfig.Where(query.DeviceConfig.DeviceTemplateID.Eq(templateID)).First()
	if err != nil {
		return nil, err
	}
	return dc, nil
}

// GetDeviceConfigIdByName retrieves a device config ID by name.
func GetDeviceConfigIdByName(name string) *string {
	var configId string
	err := query.DeviceConfig.Where(query.DeviceConfig.Name.Eq(name)).Select(query.DeviceConfig.ID).Limit(1).Scan(&configId)
	if err != nil {
		return nil
	}
	return &configId
}

// 根据功能模板ID查询想关联的配置模板数量
func GetDeviceConfigCountByFuncTemplateId(id string) (int64, error) {
	count, err := query.DeviceConfig.Where(query.DeviceConfig.DeviceTemplateID.Eq(id)).Count()
	if err != nil {
		logrus.Error(err)
	}
	return count, err
}

// 给设备增加设备模板
func UpdateDeviceDeviceConfigID(deviceID, tenantID string, deviceConfigID *string) error {
	return ReplaceTenantDeviceConfigurations(context.Background(), tenantID, []string{deviceID}, deviceConfigID, true)
}

// updateDeviceConfigForOwner keeps role-changing updates on the existing
// configuration owner. Device rows are always locked before configuration rows,
// matching parent binding and device configuration replacement.
func updateDeviceConfigForOwner(id, tenantID string, updates map[string]interface{}, expectedRevision *int) error {
	if strings.TrimSpace(tenantID) == "" {
		return ErrDeviceBindingInvalid
	}
	if _, changesOwner := updates["tenant_id"]; changesOwner {
		return ErrDeviceBindingInvalid
	}
	if _, changesID := updates["id"]; changesID {
		return ErrDeviceBindingInvalid
	}
	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	q, c := tx.Device, tx.DeviceConfig
	var err error
	var lockedIDs map[string]bool
	var desiredType string
	roleChange := false
	if value, provided := updates["device_type"]; provided {
		roleChange = true
		switch value := value.(type) {
		case string:
			desiredType = value
		case *string:
			if value != nil {
				desiredType = *value
			}
		}
		if desiredType != "1" && desiredType != "2" && desiredType != "3" {
			err = ErrDeviceBindingInvalid
		}
	}
	if err == nil && roleChange {
		var refs []*model.Device
		refs, err = q.Where(q.DeviceConfigID.Eq(id)).Order(q.ID).Find()
		lockedIDs = map[string]bool{}
		if err == nil {
			for _, d := range refs {
				if d.TenantID != tenantID {
					err = ErrDeviceBindingInvalid
					break
				}
				_, err = q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(d.ID), q.TenantID.Eq(tenantID)).First()
				if err != nil {
					break
				}
				lockedIDs[d.ID] = true
			}
		}
	}
	if err == nil {
		var config *model.DeviceConfig
		config, err = c.Clauses(clause.Locking{Strength: "UPDATE"}).Where(c.ID.Eq(id), c.TenantID.Eq(tenantID)).First()
		if err == nil && expectedRevision != nil && config.NativeBindingRevision != *expectedRevision {
			err = ErrNativeBindingRevisionConflict
		}
	}
	if err == nil && roleChange {
		var refs []*model.Device
		refs, err = q.Where(q.DeviceConfigID.Eq(id)).Order(q.ID).Find()
		if err == nil {
			for _, d := range refs {
				// A reference added while the lock plan was being acquired must be retried
				// with a new plan; acquiring it now would reverse the global lock order.
				if !lockedIDs[d.ID] {
					err = ErrDeviceConfigurationConflict
					break
				}
				if d.TenantID != tenantID {
					err = ErrDeviceBindingInvalid
					break
				}
				if d.ParentID != nil && *d.ParentID != "" && desiredType != "2" && desiredType != "3" {
					err = ErrDeviceBindingInvalid
					break
				}
				var children int64
				children, err = q.Where(q.ParentID.Eq(d.ID)).Count()
				if err != nil {
					break
				}
				if children > 0 && desiredType != "2" {
					err = ErrDeviceBindingInvalid
					break
				}
			}
		}
	}
	if err == nil {
		scoped := c.Where(c.ID.Eq(id), c.TenantID.Eq(tenantID))
		if expectedRevision != nil {
			scoped = scoped.Where(c.NativeBindingRevision.Eq(*expectedRevision))
		}
		info, updateErr := scoped.Updates(updates)
		err = updateErr
		if err == nil && info.RowsAffected != 1 {
			err = ErrNativeBindingRevisionConflict
		}
	}
	return finishDeviceBinding(tx, err)
}

var ErrDeviceConfigurationConflict = errors.New("device configuration references changed; retry with current state")

// validateDeviceConfigurationRole is called with the affected device row locked.
// Child creation/binding takes that same parent lock, so dependency inspection
// and configuration replacement cannot pass each other unnoticed.
func validateDeviceConfigurationRole(tx *query.Query, tenantID string, device *model.Device, configID *string) error {
	q, c := tx.Device, tx.DeviceConfig
	children, err := q.Where(q.ParentID.Eq(device.ID)).Count()
	if err != nil {
		return err
	}
	isChild := device.ParentID != nil && *device.ParentID != ""
	if configID == nil || *configID == "" {
		if isChild || children > 0 {
			return ErrDeviceBindingInvalid
		}
		return nil
	}
	config, err := c.Clauses(clause.Locking{Strength: "SHARE"}).Where(c.ID.Eq(*configID), c.TenantID.Eq(tenantID)).First()
	if err != nil {
		return err
	}
	if isChild && config.DeviceType != "2" && config.DeviceType != "3" {
		return ErrDeviceBindingInvalid
	}
	if children > 0 && config.DeviceType != "2" {
		return ErrDeviceBindingInvalid
	}
	return nil
}

func ReplaceTenantDeviceConfigurations(ctx context.Context, tenantID string, deviceIDs []string, configID *string, requireEmpty bool) error {
	if strings.TrimSpace(tenantID) == "" || len(deviceIDs) == 0 {
		return ErrDeviceBindingInvalid
	}
	unique := map[string]bool{}
	ids := []string{}
	for _, id := range deviceIDs {
		if strings.TrimSpace(id) == "" {
			return ErrDeviceBindingInvalid
		}
		if !unique[id] {
			unique[id] = true
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	tx := query.Use(global.DB.WithContext(ctx)).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	q := tx.Device
	devices := []*model.Device{}
	var err error
	for _, id := range ids {
		var device *model.Device
		device, err = q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).First()
		if err != nil {
			break
		}
		if requireEmpty && device.DeviceConfigID != nil && *device.DeviceConfigID != "" {
			err = ErrDeviceConfigurationConflict
			break
		}
		devices = append(devices, device)
	}
	if err == nil {
		for _, device := range devices {
			if err = validateDeviceConfigurationRole(tx.Query, tenantID, device, configID); err != nil {
				break
			}
		}
	}
	if err == nil {
		for _, id := range ids {
			info, updateErr := q.Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).Update(q.DeviceConfigID, configID)
			err = updateErr
			if err == nil && info.RowsAffected != 1 {
				err = ErrDeviceConfigurationConflict
			}
			if err != nil {
				break
			}
		}
	}
	return finishDeviceBinding(tx, err)
}

// New-child creation also participates in configuration serialization. A missing
// configuration keeps the existing unconfigured-device contract; any configured
// role must be owned by this tenant and compatible with its parent/child role.
func validateNewChildConfigurations(tx *query.Query, parent, child *model.Device) error {
	ids := []string{}
	seen := map[string]bool{}
	for _, d := range []*model.Device{parent, child} {
		if d.DeviceConfigID != nil && *d.DeviceConfigID != "" && !seen[*d.DeviceConfigID] {
			seen[*d.DeviceConfigID] = true
			ids = append(ids, *d.DeviceConfigID)
		}
	}
	sort.Strings(ids)
	configs := map[string]*model.DeviceConfig{}
	q := tx.DeviceConfig
	for _, id := range ids {
		c, err := q.Clauses(clause.Locking{Strength: "SHARE"}).Where(q.ID.Eq(id), q.TenantID.Eq(child.TenantID)).First()
		if err != nil {
			return err
		}
		configs[id] = c
	}
	if parent.DeviceConfigID != nil && *parent.DeviceConfigID != "" && configs[*parent.DeviceConfigID].DeviceType != "2" {
		return ErrDeviceBindingInvalid
	}
	if child.DeviceConfigID != nil && *child.DeviceConfigID != "" {
		typ := configs[*child.DeviceConfigID].DeviceType
		if typ != "2" && typ != "3" {
			return ErrDeviceBindingInvalid
		}
	}
	return nil
}

// Every new configuration reference, including an unparented device, holds the
// configuration row until commit. Configuration updates/deletion can therefore
// inspect the complete committed reference set under their exclusive lock.
func lockNewDeviceConfigurations(tx *query.Query, devices []*model.Device) error {
	owners := map[string]string{}
	ids := []string{}
	for _, d := range devices {
		if d == nil {
			return ErrDeviceBindingInvalid
		}
		if d.DeviceConfigID == nil || *d.DeviceConfigID == "" {
			continue
		}
		id := *d.DeviceConfigID
		if owner, exists := owners[id]; exists {
			if owner != d.TenantID {
				return ErrDeviceBindingInvalid
			}
		} else {
			owners[id] = d.TenantID
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	q := tx.DeviceConfig
	for _, id := range ids {
		if _, err := q.Clauses(clause.Locking{Strength: "SHARE"}).Where(q.ID.Eq(id), q.TenantID.Eq(owners[id])).First(); err != nil {
			return err
		}
	}
	return nil
}

type DeviceConfigurationInUseError struct{ Count int64 }

func (e *DeviceConfigurationInUseError) Error() string {
	return "device configuration is still referenced"
}
