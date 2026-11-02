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

	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"gorm.io/gen"

	"github.com/sirupsen/logrus"
)

func CreateDevice(device *model.Device) error {
	if device != nil && device.ParentID != nil {
		return CreateDeviceWithLockedParent(device)
	}
	return createDevicesWithDefaultRootGroup([]*model.Device{device})
}

// 批量创建设备
func CreateDeviceBatch(devices []*model.Device) error {
	return createDevicesWithDefaultRootGroup(devices)
}

func CreateDeviceBath(devices []*model.Device) error {
	return createDevicesWithDefaultRootGroup(devices)
}

func createDevicesWithDefaultRootGroup(devices []*model.Device) error {
	if len(devices) == 0 {
		return nil
	}

	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := createDevicesWithDefaultRootGroupTx(tx.Query, devices); err != nil {
		if tx.Rollback() != nil {
			return ErrDeviceCreateOutcomeUnknown
		}
		return err
	}
	if tx.Commit() != nil {
		return ErrDeviceCreateOutcomeUnknown
	}
	return nil
}

var ErrParentDeviceUnavailable = errors.New("parent device unavailable")
var ErrDeviceCreateOutcomeUnknown = errors.New("device creation outcome unknown")

// CreateDeviceWithLockedParent holds the parent lock through the device and
// default group writes; callers must not retry an unknown outcome blindly.
func CreateDeviceWithLockedParent(device *model.Device) error {
	if device == nil || device.ParentID == nil || strings.TrimSpace(*device.ParentID) == "" || *device.ParentID == device.ID || strings.TrimSpace(device.TenantID) == "" {
		return ErrParentDeviceUnavailable
	}
	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	q := tx.Device
	parent, err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(*device.ParentID), q.TenantID.Eq(device.TenantID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrParentDeviceUnavailable
	}
	if err == nil {
		err = validateNewChildConfigurations(tx.Query, parent, device)
	}
	if err == nil {
		err = createDevicesWithDefaultRootGroupTx(tx.Query, []*model.Device{device})
	}
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return ErrDeviceCreateOutcomeUnknown
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrDeviceCreateOutcomeUnknown
	}
	return nil
}

func createDevicesWithDefaultRootGroupTx(tx *query.Query, devices []*model.Device) error {
	if len(devices) == 0 {
		return nil
	}
	if err := reserveDeviceIDs(tx, devices); err != nil {
		return err
	}
	if err := lockNewDeviceConfigurations(tx, devices); err != nil {
		return err
	}
	tenantID := devices[0].TenantID
	if err := tx.Device.Create(devices...); err != nil {
		return err
	}

	if tenantID == "" {
		return nil
	}

	groupID, err := GetAutoBindRootDeviceGroupID(tx, tenantID)
	if err != nil || groupID == "" {
		return err
	}

	relations := make([]*model.RGroupDevice, 0, len(devices))
	for _, device := range devices {
		if device == nil || device.ID == "" || device.TenantID != tenantID {
			continue
		}
		relations = append(relations, &model.RGroupDevice{
			GroupID:  groupID,
			DeviceID: device.ID,
			TenantID: tenantID,
		})
	}

	if len(relations) == 0 {
		return nil
	}

	return tx.RGroupDevice.Create(relations...)
}

// UpdateDevice is used by activation: write only activation's intended fields,
// never a stale parent/configuration snapshot read before a concurrent bind.
func UpdateDevice(device *model.Device) (*model.Device, error) {
	q := query.Device
	updates := map[string]interface{}{"name": device.Name, "activate_flag": device.ActivateFlag, "update_at": device.UpdateAt, "activate_at": device.ActivateAt}
	// Fill an absent initial state in the same write without overwriting a
	// concurrent explicit disable.
	if device.IsEnabled == "enabled" {
		updates["is_enabled"] = gorm.Expr("CASE WHEN is_enabled = '' THEN ? ELSE is_enabled END", "enabled")
	}
	info, err := q.Where(q.ID.Eq(device.ID), q.TenantID.Eq(device.TenantID)).Updates(updates)
	if err != nil {
		return nil, err
	}
	if info.RowsAffected != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return q.Where(q.ID.Eq(device.ID), q.TenantID.Eq(device.TenantID)).First()
}

var ErrDeviceBindingInvalid = errors.New("invalid device parent binding")
var ErrDeviceBindingOutcomeUnknown = errors.New("device parent binding outcome unknown")

// lockDeviceParentSet locks every referenced device in ID order. The ancestry
// snapshot is only a lock plan: after waiting we validate the current chain from
// the locked rows, rejecting any newly introduced ancestor outside that plan.
func lockDeviceParentSet(tx *query.Query, tenantID, parentID string, childIDs []string, unbound bool, configOverrides map[string]string) (map[string]*model.Device, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(parentID) == "" {
		return nil, ErrDeviceBindingInvalid
	}
	planned := map[string]bool{}
	for _, id := range childIDs {
		if id == parentID || strings.TrimSpace(id) == "" {
			return nil, ErrDeviceBindingInvalid
		}
		planned[id] = true
	}
	ancestors := map[string]bool{}
	q := tx.Device
	for id := parentID; id != ""; {
		if ancestors[id] {
			return nil, ErrDeviceBindingInvalid
		}
		ancestors[id], planned[id] = true, true
		d, err := q.Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).First()
		if err != nil {
			return nil, err
		}
		if d.ParentID == nil {
			break
		}
		id = *d.ParentID
	}
	ids := make([]string, 0, len(planned))
	for id := range planned {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	locked := make(map[string]*model.Device, len(ids))
	for _, id := range ids {
		d, err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).First()
		if err != nil {
			return nil, err
		}
		locked[id] = d
	}
	ancestors = map[string]bool{}
	for id := parentID; id != ""; {
		if ancestors[id] || locked[id] == nil {
			return nil, ErrDeviceBindingInvalid
		}
		ancestors[id] = true
		d := locked[id]
		if d.ParentID == nil {
			break
		}
		id = *d.ParentID
	}
	for _, id := range childIDs {
		if ancestors[id] || (unbound && locked[id].ParentID != nil) {
			return nil, ErrDeviceBindingInvalid
		}
	}
	// Configuration rows are locked after devices, in a consistent order as well.
	configIDs := map[string]bool{}
	for _, id := range append([]string{parentID}, childIDs...) {
		d := locked[id]
		if configID, ok := configOverrides[id]; ok {
			d.DeviceConfigID = &configID
		}
		if d.DeviceConfigID == nil || *d.DeviceConfigID == "" {
			return nil, ErrDeviceBindingInvalid
		}
		configIDs[*d.DeviceConfigID] = true
	}
	ids = ids[:0]
	for id := range configIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	configs := map[string]*model.DeviceConfig{}
	c := tx.DeviceConfig
	for _, id := range ids {
		config, err := c.Clauses(clause.Locking{Strength: "SHARE"}).Where(c.ID.Eq(id), c.TenantID.Eq(tenantID)).First()
		if err != nil {
			return nil, err
		}
		configs[id] = config
	}
	if configs[*locked[parentID].DeviceConfigID].DeviceType != "2" {
		return nil, ErrDeviceBindingInvalid
	}
	for _, id := range childIDs {
		typ := configs[*locked[id].DeviceConfigID].DeviceType
		if typ != "2" && typ != "3" {
			return nil, ErrDeviceBindingInvalid
		}
		children, err := q.Where(q.ParentID.Eq(id)).Count()
		if err != nil {
			return nil, err
		}
		if children > 0 && typ != "2" {
			return nil, ErrDeviceBindingInvalid
		}
	}
	return locked, nil
}

func finishDeviceBinding(tx *query.QueryTx, err error) error {
	if err != nil {
		if tx.Rollback() != nil {
			return ErrDeviceBindingOutcomeUnknown
		}
		return err
	}
	if tx.Commit() != nil {
		return ErrDeviceBindingOutcomeUnknown
	}
	return nil
}

// BindExistingSubDevices commits the whole request or none of it. DeleteDevice
// and ordinary child creation use the same tenant-scoped parent row barrier.
func BindExistingSubDevices(ctx context.Context, tenantID, parentID string, childIDs []string) error {
	if len(childIDs) == 0 {
		return ErrDeviceBindingInvalid
	}
	tx := query.Use(global.DB.WithContext(ctx)).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	_, err := lockDeviceParentSet(tx.Query, tenantID, parentID, childIDs, true, nil)
	if err == nil {
		q := tx.Device
		for _, id := range childIDs {
			info, updateErr := q.Where(q.ID.Eq(id), q.TenantID.Eq(tenantID), q.ParentID.IsNull()).Updates(map[string]interface{}{"parent_id": parentID, "sub_device_addr": id})
			if updateErr != nil {
				err = updateErr
				break
			}
			if info.RowsAffected != 1 {
				err = ErrDeviceBindingInvalid
				break
			}
		}
	}
	return finishDeviceBinding(tx, err)
}

func UpdateDeviceByMap(deviceID, tenantID string, deviceMap map[string]interface{}) (*model.Device, error) {
	return updateDeviceByMap(deviceID, tenantID, deviceMap, false)
}

func RuntimeUpdateDeviceByMap(deviceID, tenantID string, deviceMap map[string]interface{}) (*model.Device, error) {
	return updateDeviceByMap(deviceID, tenantID, deviceMap, true)
}

func updateDeviceByMap(deviceID, tenantID string, deviceMap map[string]interface{}, scoped bool) (*model.Device, error) {
	tx := query.Use(global.DB).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	var err error
	if scoped {
		if tx.Device.UnderlyingDB().Dialector.Name() != "postgres" {
			tx.Rollback()
			return nil, ErrDeviceIdentityState
		}
		if err := tx.Device.UnderlyingDB().Exec("SET LOCAL lock_timeout='3s'").Error; err != nil {
			tx.Rollback()
			return nil, err
		}
		db := tx.Device.UnderlyingDB().Session(&gorm.Session{NewDB: true})
		var current model.Device
		err = db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", deviceID, tenantID).Take(&current).Error
		if err == nil {
			var reservation DeviceIDReservation
			err = db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND state='active'", deviceID, tenantID).Take(&reservation).Error
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	if value, hasParent := deviceMap["parent_id"]; hasParent {
		parent, ok := value.(*string)
		if !ok || parent == nil {
			err = ErrDeviceBindingInvalid
		} else {
			overrides := map[string]string{}
			if value, exists := deviceMap["device_config_id"]; exists {
				config, valid := value.(*string)
				if !valid || config == nil {
					err = ErrDeviceBindingInvalid
				} else {
					overrides[deviceID] = *config
				}
			}
			if err == nil {
				var locked map[string]*model.Device
				locked, err = lockDeviceParentSet(tx.Query, tenantID, *parent, []string{deviceID}, false, overrides)
				if err == nil && scoped {
					ids := make([]string, 0, len(locked))
					for id := range locked {
						ids = append(ids, id)
					}
					sort.Strings(ids)
					db := tx.Device.UnderlyingDB().Session(&gorm.Session{NewDB: true})
					for _, id := range ids {
						var reservation DeviceIDReservation
						if err = db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND tenant_id=? AND state='active'", id, tenantID).Take(&reservation).Error; err != nil {
							break
						}
					}
				}
			}
		}
	}
	q := tx.Device
	if err == nil {
		if value, changesConfig := deviceMap["device_config_id"]; changesConfig {
			configID, valid := value.(*string)
			if !valid {
				err = ErrDeviceBindingInvalid
			} else {
				var current *model.Device
				current, err = q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(deviceID), q.TenantID.Eq(tenantID)).First()
				if err == nil {
					err = validateDeviceConfigurationRole(tx.Query, tenantID, current, configID)
				}
			}
		}
	}
	if err == nil {
		info, updateErr := q.Where(q.ID.Eq(deviceID), q.TenantID.Eq(tenantID)).Updates(deviceMap)
		err = updateErr
		if err == nil && info.RowsAffected != 1 {
			err = gorm.ErrRecordNotFound
		}
	}
	var device *model.Device
	if err == nil {
		device, err = q.Where(q.ID.Eq(deviceID), q.TenantID.Eq(tenantID)).First()
	}
	if err = finishDeviceBinding(tx, err); err != nil {
		return nil, err
	}
	return device, nil
}

// 更新设备状态
// 设备发的每条状态消息都如实记录到历史，不依赖缓存中的状态做预判
func UpdateDeviceStatus(deviceId string, status int16) (bool, error) {
	// 1. 获取设备信息（仅用于取 TenantID，后续写历史用）
	// 不再从 Redis 读取 is_online 做预判，避免竞态条件
	var tenantID string
	device, err := query.Device.Where(query.Device.ID.Eq(deviceId)).First()
	if err != nil {
		logrus.WithError(err).WithField("device_id", deviceId).Error("Failed to get device info")
		return false, err
	}
	tenantID = device.TenantID

	// 2. 直接更新设备状态，不检查当前值
	if status == 0 {
		// 设备离线时，同时更新 is_online 和 last_offline_time
		now := time.Now().UTC()
		info, err := query.Device.Where(query.Device.ID.Eq(deviceId)).
			UpdateColumns(map[string]interface{}{
				"is_online":         status,
				"last_offline_time": now,
			})
		if err != nil {
			logrus.Error(err)
			return false, err
		}
		if info.RowsAffected == 0 {
			return false, fmt.Errorf("update device status failed, no rows affected")
		}
	} else {
		// 设备上线时，只更新 is_online
		_, err := query.Device.Where(query.Device.ID.Eq(deviceId)).Update(query.Device.IsOnline, status)
		if err != nil {
			logrus.Error(err)
			return false, err
		}
	}

	// 3. 删除设备缓存，确保下次获取时获取最新数据
	if err := global.REDIS.Del(context.Background(), deviceId).Err(); err != nil {
		logrus.WithError(err).WithField("device_id", deviceId).Warn("Failed to delete device cache after status update")
	}

	// 4. 异步保存状态历史记录（不阻塞主流程）
	// 每条状态消息都记录，不跳过
	go func() {
		if err := SaveDeviceStatusHistory(tenantID, deviceId, status); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"device_id": deviceId,
				"status":    status,
			}).Warn("Failed to save device status history")
		}
	}()

	return true, nil
}

// DeleteDevice keeps the legacy DAL entry point inside the same identity
// transaction as service deletions. Callers needing dependency cleanup use
// DeleteDeviceWithTx inside their wider transaction.
func DeleteDevice(id string, tenantID string) error {
	return query.Use(global.DB).Transaction(func(tx *query.Query) error {
		q := tx.Device
		if _, err := q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).First(); err != nil {
			return err
		}
		if err := tombstoneDeviceID(tx, id, tenantID); err != nil {
			return err
		}
		_, err := q.Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).Delete()
		return err
	})
}

// LockTenantDeviceForDelete establishes ownership before any dependent data is touched.
func LockTenantDeviceForDelete(id, tenantID string, tx *query.QueryTx) (*model.Device, error) {
	q := tx.Device
	return q.Clauses(clause.Locking{Strength: "UPDATE"}).Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).First()
}

func HasDeviceDeleteDependencies(id string, tx *query.QueryTx) (bool, bool, error) {
	d := tx.Device
	children, err := d.Where(d.ParentID.Eq(id)).Count()
	if err != nil {
		return false, false, err
	}
	c := tx.DeviceTriggerCondition
	triggers, err := c.Where(c.TriggerSource.Eq(id)).Count()
	return children > 0, triggers > 0, err
}

func DeleteDeviceWithTx(id, tenantID string, tx *query.QueryTx) (int64, error) {
	if err := tombstoneDeviceID(tx.Query, id, tenantID); err != nil {
		return 0, err
	}
	q := tx.Device
	result, err := q.Where(q.ID.Eq(id), q.TenantID.Eq(tenantID)).Delete()
	return result.RowsAffected, err
}

// 根据子设备id获取父设备信息
func GetParentDeviceBySubDeviceID(subDeviceID string) (info *model.Device, err error) {
	device := query.Device
	info, err = device.Where(device.ID.Eq(subDeviceID)).First()
	if err != nil {
		logrus.Error(err)
	}
	return
}

func GetDeviceByID(id string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}
	if device == nil {
		return nil, fmt.Errorf("device is nil for id: %s", id)
	}
	return device, nil
}

// DeviceExistsForRead performs an ownership-scoped existence check without
// loading device metadata or credentials into the caller's process.
func DeviceExistsForRead(id, tenantID string, allTenants bool) (bool, error) {
	q := query.Device.Where(query.Device.ID.Eq(id))
	if !allTenants {
		q = q.Where(query.Device.TenantID.Eq(tenantID))
	}
	count, err := q.Count()
	return count == 1, err
}

// 获取设备详情，关联设备配置
func GetDeviceDetail(id string) (map[string]interface{}, error) {
	device := query.Device
	deviceConfig := query.DeviceConfig
	t := query.TelemetryCurrentData
	t2 := query.TelemetryCurrentData.As("t2")
	data := make(map[string]interface{})
	// 关联查询设备配置表
	err := device.LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).
		LeftJoin(t.Select(t.T.Max().As("ts"), t.DeviceID).Group(t.DeviceID).As("t2"), t2.DeviceID.EqCol(device.ID)).
		Where(device.ID.Eq(id)).
		Select(device.ALL, deviceConfig.Name.As("device_config_name"), t2.T).Scan(&data)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	if data["parent_id"] != nil {
		parentDevice, err := GetDeviceByID(data["parent_id"].(string))
		if err != nil {
			logrus.Error(err)
			return nil, err
		}
		data["gateway_device_name"] = parentDevice.Name
	}
	return data, err
}

// 通过凭证获取设备信息
func GetDeviceByVoucher(voucher string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.Voucher.Eq(voucher)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("get device by voucher: %s failed: %v", voucher, err)
		}
		return nil, err
	}
	return device, err
}

// 更新设备在线状态
func UpdateDeviceOnlineStatus(deviceId string, status int16) error {
	if status == 0 {
		// 设备离线时，同时更新is_online和last_offline_time
		now := time.Now().UTC()
		_, err := query.Device.Where(query.Device.ID.Eq(deviceId)).
			UpdateColumns(map[string]interface{}{
				"is_online":         status,
				"last_offline_time": now,
			})
		if err != nil {
			logrus.Error(err)
		}
		return err
	} else {
		// 设备上线时，只更新is_online字段
		_, err := query.Device.Where(query.Device.ID.Eq(deviceId)).Update(query.Device.IsOnline, status)
		if err != nil {
			logrus.Error(err)
		}
		return err
	}
}

// 通过设备编号获取设备信息
func GetDeviceByDeviceNumber(deviceNumber string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.DeviceNumber.Eq(deviceNumber)).First()
	if err != nil {
		logrus.Error(err)
	}
	return device, err
}

func GetDeviceBySubDeviceAddress(deviceAddress []string, parentId string) (map[string]*model.Device, error) {
	devices, err := query.Device.Where(query.Device.SubDeviceAddr.In(deviceAddress...)).
		Where(query.Device.ParentID.Eq(parentId)).Find()
	if err != nil {
		return nil, err
	}
	result := make(map[string]*model.Device)
	for _, d := range devices {
		result[*d.SubDeviceAddr] = d
	}
	return result, err
}

// 移除子设备：将设备的parent_id置为空
func RemoveSubDevice(deviceId string, tenant_id string) error {
	info, err := query.Device.Where(query.Device.ID.Eq(deviceId), query.Device.TenantID.Eq(tenant_id)).UpdateSimple(query.Device.ParentID.Null(), query.Device.SubDeviceAddr.Null())
	if err != nil {
		logrus.Error(err)
	} else if info.RowsAffected == 0 {
		return fmt.Errorf("remove sub device failed, device not found")
	}
	return err
}

// 获取设备列表，分页
func GetDeviceListByPage(req *model.GetDeviceListByPageReq, tenant_id string, allTenants bool) (int64, []model.GetDeviceListByPageRsp, error) {
	q := query.Device
	c := query.DeviceConfig
	lda := query.LatestDeviceAlarm
	ctx := context.Background()
	hasValue := func(s *string) bool {
		return s != nil && strings.TrimSpace(*s) != ""
	}
	like := func(f field.String, v string) gen.Condition {
		return f.Like(fmt.Sprintf("%%%s%%", v))
	}
	var (
		count      int64
		deviceList = []model.GetDeviceListByPageRsp{}
		builder    = q.WithContext(ctx).
				Where(q.ActivateFlag.Eq("active")).
				LeftJoin(c, c.ID.EqCol(q.DeviceConfigID)).
				LeftJoin(lda, lda.DeviceID.EqCol(q.ID))
	)
	if !allTenants {
		builder = builder.Where(q.TenantID.Eq(tenant_id))
	}
	if hasValue(req.GroupId) {
		groupIds, err := GetGroupChildrenIds(strings.TrimSpace(*req.GroupId))
		if err != nil {
			logrus.Error(err)
			return count, deviceList, err
		}
		ids, err := GetDeviceIdsByGroupIds(groupIds)
		if err != nil {
			logrus.Error(err)
			return count, deviceList, err
		}
		ids = append(ids, strings.TrimSpace(*req.GroupId))
		if len(ids) == 0 {
			return 0, []model.GetDeviceListByPageRsp{}, nil
		}
		builder = builder.Where(q.ID.In(ids...))
	}
	if req.GroupScope != nil && strings.TrimSpace(*req.GroupScope) == "ungrouped" {
		groupQuery := query.RGroupDevice.WithContext(ctx)
		if !allTenants {
			groupQuery = groupQuery.Where(query.RGroupDevice.TenantID.Eq(tenant_id))
		}
		groupedDeviceIDs := groupQuery.Select(query.RGroupDevice.DeviceID)
		builder = builder.Where(gen.Columns{q.ID}.NotIn(groupedDeviceIDs))
	}
	if hasValue(req.IsEnabled) {
		builder = builder.Where(q.IsEnabled.Eq(strings.TrimSpace(*req.IsEnabled)))
	}
	if hasValue(req.ProductID) {
		builder = builder.Where(q.ProductID.Eq(strings.TrimSpace(*req.ProductID)))
	}
	if hasValue(req.ServiceIdentifier) {
		value := strings.TrimSpace(*req.ServiceIdentifier)
		if value == "mqtt" {
			builder = builder.Where(
				query.Device.Where(c.ProtocolType.Eq(value)).Or(q.DeviceConfigID.IsNull()),
			)
		} else {
			builder = builder.Where(
				query.Device.Where(c.ProtocolType.Eq(value)).Or(q.ServiceAccessID.IsNotNull()),
			)
		}
	}
	if hasValue(req.ServiceAccessID) {
		builder = builder.Where(q.ServiceAccessID.Eq(strings.TrimSpace(*req.ServiceAccessID)))
	}
	if hasValue(req.DeviceType) {
		value := strings.TrimSpace(*req.DeviceType)
		if value == "1" {
			builder = builder.Where(
				query.Device.Where(q.DeviceConfigID.IsNull()).Or(c.DeviceType.Eq(value)),
			)
		} else {
			builder = builder.Where(c.DeviceType.Eq(value))
		}
	}
	if hasValue(req.AccessWay) {
		builder = builder.Where(q.AccessWay.Eq(strings.TrimSpace(*req.AccessWay)))
	}
	if hasValue(req.Label) {
		builder = builder.Where(like(q.Label, strings.TrimSpace(*req.Label)))
	}
	if hasValue(req.Search) {
		search := strings.ToLower(strings.TrimSpace(*req.Search))
		builder = builder.Where(
			query.Device.Where(q.Name.Lower().Like(fmt.Sprintf("%%%s%%", search))).
				Or(q.DeviceNumber.Lower().Like(fmt.Sprintf("%%%s%%", search))),
		)
	}
	if hasValue(req.Name) {
		builder = builder.Where(like(q.Name, strings.TrimSpace(*req.Name)))
	}
	if hasValue(req.CurrentVersion) {
		builder = builder.Where(q.CurrentVersion.Eq(strings.TrimSpace(*req.CurrentVersion)))
	}
	if hasValue(req.BatchNumber) {
		builder = builder.Where(like(q.BatchNumber, strings.TrimSpace(*req.BatchNumber)))
	}
	if hasValue(req.DeviceNumber) {
		builder = builder.Where(like(q.DeviceNumber, strings.TrimSpace(*req.DeviceNumber)))
	}
	if hasValue(req.DeviceConfigId) {
		builder = builder.Where(q.DeviceConfigID.Eq(strings.TrimSpace(*req.DeviceConfigId)))
	}
	if req.IsOnline != nil {
		switch *req.IsOnline {
		case 1:
			builder = builder.Where(q.IsOnline.Eq(1))
		case 0:
			builder = builder.Where(q.IsOnline.Neq(1))
		default:
			return count, deviceList, fmt.Errorf("is_online param error")
		}
	}
	if hasValue(req.DeviceTemplateID) {
		builder = builder.Where(c.DeviceTemplateID.Eq(strings.TrimSpace(*req.DeviceTemplateID)))
	}
	if hasValue(req.WarnStatus) {
		// 列表始终返回最新告警状态，此处只追加用户选择的状态过滤条件。
		value := strings.TrimSpace(*req.WarnStatus)
		if value == "N" {
			builder = builder.Where(
				lda.AlarmStatus.Eq("N"),
			).Or(lda.AlarmStatus.IsNull())
		} else {
			builder = builder.Where(lda.AlarmStatus.Neq("N"))
		}
	}
	count, err := builder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}
	if req.Page != 0 && req.PageSize != 0 {
		builder = builder.Limit(req.PageSize).
			Offset((req.Page - 1) * req.PageSize)
	}
	t := query.TelemetryCurrentData
	t2 := query.TelemetryCurrentData.As("t2")
	err = builder.Select(
		q.ID,
		q.DeviceNumber,
		q.Name,
		q.DeviceConfigID,
		q.ActivateFlag,
		q.ActivateAt,
		q.BatchNumber,
		q.Location,
		q.CurrentVersion,
		q.CreatedAt,
		q.IsOnline,
		lda.AlarmStatus.As("WarnStatus"),
		q.AccessWay,
		c.ProtocolType,
		c.DeviceType,
		c.Name.As("DeviceConfigName"),
		t2.T,
		c.ImageURL,
		q.LastOfflineTime,
		q.AdditionalInfo,
	).
		LeftJoin(t.Select(t.T.Max().As("ts"), t.DeviceID).Group(t.DeviceID).As("t2"), t2.DeviceID.EqCol(q.ID)).
		Order(q.CreatedAt.Desc()).
		Scan(&deviceList)
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}
	return count, deviceList, nil
}

func GetDevicePreRegisterListByPage(req *model.GetDevicePreRegisterListByPageReq, tenant_id string) (int64, []model.GetDevicePreRegisterListByPageRsp, error) {
	q := query.Device
	var count int64
	deviceList := []model.GetDevicePreRegisterListByPageRsp{}
	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenant_id))

	if req.ActivateFlag != nil && *req.ActivateFlag != "" {
		queryBuilder = queryBuilder.Where(q.ActivateFlag.Eq(*req.ActivateFlag))
	}

	if req.IsEnabled != nil && *req.IsEnabled != "" {
		queryBuilder = queryBuilder.Where(q.IsEnabled.Eq(*req.IsEnabled))
	}

	if req.ProductID != "" {
		queryBuilder = queryBuilder.Where(q.ProductID.Eq(req.ProductID))
	}

	if req.DeviceConfigID != nil && *req.DeviceConfigID != "" {
		queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(*req.DeviceConfigID))
	}

	if req.BatchNumber != nil && *req.BatchNumber != "" {
		queryBuilder = queryBuilder.Where(q.BatchNumber.Like(fmt.Sprintf("%%%s%%", *req.BatchNumber)))
	}

	if req.DeviceNumber != nil && *req.DeviceNumber != "" {
		queryBuilder = queryBuilder.Where(q.DeviceNumber.Like(fmt.Sprintf("%%%s%%", *req.DeviceNumber)))
	}

	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}

	// count查询
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	err = queryBuilder.Select(
		q.ID, q.Name, q.DeviceNumber, q.ActivateFlag, q.ActivateAt, q.BatchNumber, q.CurrentVersion).
		Order(q.CreatedAt.Desc()).
		Scan(&deviceList)
	if err != nil {
		logrus.Error(err)
		return count, deviceList, err
	}
	return count, deviceList, err
}

func GetDevicesCount() int64 {
	count, _ := query.Device.Count()
	return count
}

// 通过设备id获取设备信息
func GetDeviceCacheById(deviceId string) (*model.Device, error) {
	device, err := query.Device.Where(query.Device.ID.Eq(deviceId)).First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return device, nil
}

type DeviceQuery struct{}

func (DeviceQuery) Count(ctx context.Context) (count int64, err error) {
	count, err = query.Device.Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceQuery) CountByTenantID(ctx context.Context, TenantID string) (count int64, err error) {
	device := query.Device
	count, err = device.Where(device.TenantID.Eq(TenantID)).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 获取网关未关联网关设备的子设备列表,并做关联查询设备配置表
func (DeviceQuery) GetGatewayUnrelatedDeviceList(ctx context.Context, tenantId string, search *string, deviceType *string) (list []map[string]interface{}, err error) {
	device := query.Device
	deviceConfig := query.DeviceConfig
	// 条件：device-父设备为空，设备配置不为空
	// 条件：device_config_id-设备类型为2（网关）或3（子设备）
	queryBuilder := device.
		WithContext(ctx).
		Select(device.ID, device.Name, device.DeviceConfigID.As("device_config_id"), deviceConfig.Name.As("device_config_name"), deviceConfig.DeviceType.As("device_type")).
		Where(device.TenantID.Eq(tenantId)).
		Where(device.DeviceConfigID.IsNotNull()).
		Where(device.ParentID.IsNull()). // 父设备为空
		LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).
		Where(device.ActivateFlag.Eq("active")) // 已激活设备

	// 设备类型过滤：如果指定了device_type则过滤，否则返回网关设备和子设备
	if deviceType != nil && *deviceType != "" {
		queryBuilder = queryBuilder.Where(deviceConfig.DeviceType.Eq(*deviceType))
	} else {
		// 默认返回网关设备(2)和子设备(3)
		queryBuilder = queryBuilder.Where(deviceConfig.DeviceType.In("2", "3"))
	}

	// 增加设备名称模糊匹配
	if search != nil && *search != "" {
		queryBuilder = queryBuilder.Where(device.Name.Like(fmt.Sprintf("%%%s%%", *search)))
	}

	err = queryBuilder.Scan(&list)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceQuery) CountByWhere(ctx context.Context, option ...gen.Condition) (count int64, err error) {
	device := query.Device
	count, err = device.Where(option...).Count()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceQuery) First(ctx context.Context, option ...gen.Condition) (info *model.Device, err error) {
	info, err = query.Device.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

func (DeviceQuery) Find(ctx context.Context, option ...gen.Condition) (list []*model.Device, err error) {
	list, err = query.Device.WithContext(ctx).Where(option...).Find()
	if err != nil {
		logrus.Error(ctx, err)
	}
	return
}

// 获取设备下拉列表
// 返回设备id、设备名称、设备配置id、设备配置名称
func (DeviceQuery) GetDeviceSelect(tenantId string, deviceName string, bindConfig int) (list []map[string]interface{}, err error) {
	device := query.Device
	deviceConfig := query.DeviceConfig
	query := device.
		WithContext(context.Background()).
		Select(device.ID, device.Name, device.DeviceConfigID.As("device_config_id"), deviceConfig.Name.As("device_config_name")).
		Where(device.TenantID.Eq(tenantId)).
		Where(device.ActivateFlag.Eq("active")). // 激活状态
		Where(device.Name.Like(fmt.Sprintf("%%%s%%", deviceName))).
		LeftJoin(deviceConfig, deviceConfig.ID.EqCol(device.DeviceConfigID)).Order(device.CreatedAt.Desc())
	switch bindConfig {
	case 1:
		query = query.Where(device.DeviceConfigID.IsNotNull())
	case 2:
		query = query.Where(device.DeviceConfigID.IsNull())
	}
	err = query.Scan(&list)
	if err != nil {
		logrus.Error(err)
	}
	return
}

// 更新指定字段
func (DeviceQuery) Update(ctx context.Context, info *model.Device, option ...field.Expr) error {
	device := query.Device
	_, err := query.Device.WithContext(ctx).Where(device.ID.Eq(info.ID)).Select(option...).UpdateColumns(info)
	if err != nil {
		logrus.Error(ctx, err)
	}
	return err
}

// 更新设备配置
func (DeviceQuery) ChangeDeviceConfig(deviceID, tenantID string, deviceConfigID *string) error {
	return ReplaceTenantDeviceConfigurations(context.Background(), tenantID, []string{deviceID}, deviceConfigID, false)
}

func (DeviceQuery) GetSubList(ctx context.Context, parent_id string, pageSize, page int64, tenantID string) ([]model.GetSubListResp, int64, error) {
	var (
		q     = query.Device
		count int64
		resp  []model.GetSubListResp
	)
	query := q.WithContext(ctx).Where(q.ParentID.Eq(parent_id), q.TenantID.Eq(tenantID), q.ActivateFlag.Eq("active"))
	count, err := query.Count()
	if err != nil {
		return resp, count, err
	}
	err = query.Offset(int(page-1) * int(pageSize)).Limit(int(pageSize)).Order(q.CreatedAt.Desc()).Scan(&resp)
	if err != nil {
		return resp, count, err
	}
	return resp, count, nil
}

// 获取子设备列表
func GetSubDeviceListByParentID(parentId string) ([]*model.Device, error) {
	device := query.Device
	list, err := device.Where(device.ParentID.Eq(parentId)).Find()
	if err != nil {
		logrus.Error(err)
	}
	return list, err
}

func GetDeviceTemplateChartSelect(tenantId string) (any, error) {
	data := []map[string]interface{}{}
	d := query.Device
	dc := query.DeviceConfig
	dm := query.DeviceTemplate
	err := d.LeftJoin(dc, dc.ID.EqCol(d.DeviceConfigID)).
		LeftJoin(dm, dm.ID.EqCol(dc.DeviceTemplateID)).
		Where(d.TenantID.Eq(tenantId)).
		Where(d.ActivateFlag.Eq("active")).
		Where(d.DeviceConfigID.IsNotNull()).
		Where(dc.DeviceTemplateID.IsNotNull()).
		Where(dm.WebChartConfig.IsNotNull()).
		Select(d.ID.As("device_id"), d.Name.As("device_name"), dm.WebChartConfig).Scan(&data)
	if err != nil {
		logrus.Error(err)
	}
	return data, nil
}

func GetDeviceCurrentStatus(deviceId string) (string, error) {
	data, err := query.Device.Where(query.Device.ID.Eq(deviceId)).First()
	var result string = "OFF-LINE"
	if err != nil {
		return result, err
	} else if err == gorm.ErrRecordNotFound {
		return result, nil
	}
	if data.IsOnline == 1 {
		result = "ON-LINE"
	}
	return result, nil
}

func GetDeviceTemplateIdByDeviceId(deviceId string) (string, error) {
	var result model.DeviceConfig
	query.Device.LeftJoin(query.DeviceConfig, query.Device.DeviceConfigID.EqCol(query.DeviceConfig.ID)).
		Where(query.Device.ID.Eq(deviceId)).Select(query.DeviceConfig.DeviceTemplateID).Scan(&result)
	if result.DeviceTemplateID != nil {
		return *result.DeviceTemplateID, nil
	}
	return "", nil
}

// 通过设备配置id获取设备列表
func GetDevicesByDeviceConfigID(deviceConfigID string) ([]*model.Device, error) {
	device := query.Device
	list, err := device.Where(device.DeviceConfigID.Eq(deviceConfigID)).Find()
	if err != nil {
		logrus.Error(err)
	}
	return list, err
}

// 通过子设备配置id获取已绑定网关的子设备列表
// func GetDevicesBySubDeviceConfigID(deviceConfigID string) ([]*model.Device, error) {
// 	var device = query.Device
// 	list, err := device.Where(device.DeviceConfigID.Eq(deviceConfigID), device.ParentID.IsNotNull()).Find()
// 	if err != nil {
// 		logrus.Error(err)
// 	}
// 	return list, err
// }

// 通过子设配置ID查询所有关联这个配置的子设备的网关设备列表
func GetGatewayDevicesBySubDeviceConfigID(deviceConfigID string) ([]string, error) {
	device := query.Device
	var deviceIDList []string
	err := device.Where(device.DeviceConfigID.Eq(deviceConfigID), device.ParentID.IsNotNull()).Select(device.ParentID.Distinct()).Scan(&deviceIDList)
	if err != nil {
		logrus.Error(err)
	}
	return deviceIDList, err
}

// 查询服务接入点关联的设备
func GetServiceDeviceList(serviceAccessId string) ([]model.Device, error) {
	var devices []model.Device
	err := query.Device.Where(query.Device.ServiceAccessID.Eq(serviceAccessId)).Scan(&devices)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return devices, err
}

// GetSubDeviceExists
// @description 查询子设备是否存在
func GetSubDeviceExists(deviceId, subAddr string) bool {
	num, err := query.Device.Where(query.Device.ParentID.Eq(deviceId), query.Device.SubDeviceAddr.Eq(subAddr)).Count()
	if num > 0 || err != nil {
		return true
	}
	return false
}

// CheckDeviceNumberExists
// CheckDeviceNumberExists checks if a device number already exists in the database
func CheckDeviceNumberExists(deviceNumber string) (bool, error) {
	count, err := query.Device.Where(query.Device.DeviceNumber.Eq(deviceNumber)).Count()
	if err != nil {
		logrus.Error(err)
		return false, err
	}
	return count > 0, nil
}

// CheckServiceDeviceNumberExists detects legacy service-bound devices that
// still use the connector's un-namespaced external number.
func CheckServiceDeviceNumberExists(serviceAccessID, deviceNumber string) (bool, error) {
	count, err := query.Device.Where(
		query.Device.ServiceAccessID.Eq(serviceAccessID),
		query.Device.DeviceNumber.Eq(deviceNumber),
	).Count()
	if err != nil {
		logrus.Error(err)
		return false, err
	}
	return count > 0, nil
}

// CheckVoucherExists
// CheckVoucherExists checks if a voucher already exists for another device (excluding the specified deviceID)
func CheckVoucherExists(voucher string, excludeDeviceID string) (bool, error) {
	count, err := query.Device.Where(query.Device.Voucher.Eq(voucher)).
		Where(query.Device.ID.Neq(excludeDeviceID)).Count()
	if err != nil {
		logrus.Error(err)
		return false, err
	}
	return count > 0, nil
}

// 获取设备选择器
func GetDeviceSelector(req model.DeviceSelectorReq, tenantId string) (*model.DeviceSelectorRes, error) {
	device := query.Device
	deviceConfig := query.DeviceConfig

	query := device.WithContext(context.Background()).LeftJoin(deviceConfig, device.DeviceConfigID.EqCol(deviceConfig.ID))

	if req.HasDeviceConfig != nil {
		if *req.HasDeviceConfig {
			query = query.Where(device.DeviceConfigID.IsNotNull())
		} else {
			query = query.Where(device.DeviceConfigID.IsNull())
		}
	}
	if req.Search != nil && *req.Search != "" {
		query = query.Where(device.Name.Like(fmt.Sprintf("%%%s%%", *req.Search)))
	}

	query = query.Where(device.TenantID.Eq(tenantId))

	query = query.Select(device.ID.As("device_id"), device.Name.As("device_name"), deviceConfig.DeviceType.As("device_type"))

	query = query.Order(device.UpdateAt.Desc())

	count, err := query.Count()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	query = query.Limit(req.PageSize)
	query = query.Offset((req.Page - 1) * req.PageSize)

	var list []*model.DeviceSelectorData
	err = query.Scan(&list)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return &model.DeviceSelectorRes{
		Total: count,
		List:  list,
	}, nil
}

// 通过协议标识符获取设备列表
func GetDeviceListByProtocolType(req model.GetDevicesByProtocolPluginReq, devicesRsp *model.GetDevicesByProtocolPluginRsp) error {
	device := query.Device
	deviceConfig := query.DeviceConfig
	query := device.WithContext(context.Background()).LeftJoin(deviceConfig, device.DeviceConfigID.EqCol(deviceConfig.ID))
	if req.DeviceType == "1" {
		query = query.Where(deviceConfig.ProtocolType.Eq(req.ServiceIdentifier)).
			Where(deviceConfig.DeviceType.Eq("1")).
			Where(device.ActivateFlag.Eq("active")).
			Where(deviceConfig.ProtocolType.Eq(req.ServiceIdentifier))

		count, err := query.Count()
		if err != nil {
			logrus.Error(err)
			return err
		}
		devicesRsp.Total = count

		// 临时结构体，用于接收JSON字符串
		type tempDevice struct {
			ID                     string  `json:"id"`
			Voucher                string  `json:"voucher"`
			DeviceNumber           string  `json:"device_number"`
			DeviceType             string  `json:"device_type"`
			ProtocolType           string  `json:"protocol_type"`
			Config                 *string `json:"config"`
			ProtocolConfigTemplate *string `json:"protocol_config_template"`
		}

		var tempList []tempDevice
		err = query.Limit(req.PageSize).Offset((req.Page-1)*req.PageSize).
			Select(device.ID, device.Voucher, device.DeviceNumber, deviceConfig.DeviceType, deviceConfig.ProtocolType, device.ProtocolConfig.As("config"), deviceConfig.ProtocolConfig.As("protocol_config_template")).
			Scan(&tempList)
		if err != nil {
			logrus.Error(err)
			return err
		}

		// 转换为最终结构
		devicesRsp.List = make([]model.DeviceConfigForProtocolPlugin, len(tempList))
		for i, temp := range tempList {
			devicesRsp.List[i] = model.DeviceConfigForProtocolPlugin{
				ID:           temp.ID,
				Voucher:      temp.Voucher,
				DeviceNumber: temp.DeviceNumber,
				DeviceType:   temp.DeviceType,
				ProtocolType: temp.ProtocolType,
				SubDivices:   []model.SubDeviceConfigForProtocolPlugin{},
			}

			// 处理config字段
			if temp.Config != nil && *temp.Config != "" {
				var config map[string]interface{}
				if err := json.Unmarshal([]byte(*temp.Config), &config); err != nil {
					logrus.Error("解析config JSON失败:", err)
					devicesRsp.List[i].Config = make(map[string]interface{})
				} else {
					devicesRsp.List[i].Config = config
				}
			} else {
				devicesRsp.List[i].Config = make(map[string]interface{})
			}

			// 处理protocol_config_template字段
			if temp.ProtocolConfigTemplate != nil && *temp.ProtocolConfigTemplate != "" {
				var protocolConfigTemplate map[string]interface{}
				if err := json.Unmarshal([]byte(*temp.ProtocolConfigTemplate), &protocolConfigTemplate); err != nil {
					logrus.Error("解析protocol_config_template JSON失败:", err)
					devicesRsp.List[i].ProtocolConfigTemplate = make(map[string]interface{})
				} else {
					devicesRsp.List[i].ProtocolConfigTemplate = protocolConfigTemplate
				}
			} else {
				devicesRsp.List[i].ProtocolConfigTemplate = make(map[string]interface{})
			}
		}

		return nil
	}
	// 暂不支持非直连设备
	return errors.New("暂不支持非直连设备")
}

// GetDeviceLatestAlarmStatus 获取设备的最新告警状态
func GetDeviceLatestAlarmStatus(deviceID string) (string, error) {
	lda := query.LatestDeviceAlarm
	alarm, err := lda.Where(lda.DeviceID.Eq(deviceID)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "N", nil
		}
		return "", err
	}
	if alarm.AlarmStatus == nil || *alarm.AlarmStatus == "N" || *alarm.AlarmStatus == "" {
		return "N", nil
	}
	return "Y", nil
}
