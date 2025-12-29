package dal

import (
	"errors"
	"log"
	"project/internal/model"
	"project/internal/query"
	"time"

	"github.com/go-basic/uuid"
	"gorm.io/gorm"
)

func CreateMessagePushMange(data *model.MessagePushManage) error {
	return query.MessagePushManage.Create(data)
}

func GetMessagePushMangeExists(userId, pushId string) (bool, error) {
	index, err := query.MessagePushManage.Where(query.MessagePushManage.PushID.Eq(pushId), query.MessagePushManage.UserID.Eq(userId)).Count()
	if err != nil {
		return false, nil
	}
	return index > 0, nil
}

func ActiveMessagePushMange(userId, pushId, deviceType string) error {
	_, err := query.MessagePushManage.
		Where(query.MessagePushManage.PushID.Eq(pushId), query.MessagePushManage.UserID.Eq(userId)).Updates(map[string]interface{}{
		"device_type": deviceType,
		"status":      1,
		"delete_time": nil,
		"update_time": time.Now(),
	})
	return err
}

func LogoutMessagePushMange(userId, pushId string) error {
	_, err := query.MessagePushManage.
		Where(query.MessagePushManage.PushID.Eq(pushId), query.MessagePushManage.UserID.Eq(userId)).Updates(map[string]interface{}{
		"status":      2,
		"update_time": time.Now(),
	})
	return err
}

func MessagePushMangeSendUpdate(id string, updates map[string]interface{}) error {
	_, err := query.MessagePushManage.Where(query.MessagePushManage.ID.Eq(id)).Updates(updates)

	return err
}

func GetMessagePushConfig() (*model.MessagePushConfigRes, error) {
	var result model.MessagePushConfigRes
	return &result, query.MessagePushConfig.Where(query.MessagePushConfig.ConfigType.Eq(1)).Scan(&result)
}

func SetMessagePushConfig(req *model.MessagePushConfigReq) error {
	config, err := query.MessagePushConfig.Where(query.MessagePushConfig.ConfigType.Eq(1)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return query.MessagePushConfig.Create(&model.MessagePushConfig{
			ID:         uuid.New(),
			URL:        req.Url,
			ConfigType: 1,
			CreateTime: time.Now(),
		})
	} else if err != nil {
		log.Println("err:", err)
		return err
	}
	_, err = query.MessagePushConfig.Where(query.MessagePushConfig.ID.Eq(config.ID)).Updates(map[string]interface{}{
		"url":         req.Url,
		"update_time": time.Now(),
	})
	return err
}

func GetUserMessagePushId(tenantId string) ([]model.MessagePushManage, error) {
	var result []model.MessagePushManage
	return result, query.User.LeftJoin(query.MessagePushManage, query.MessagePushManage.UserID.EqCol(query.User.ID), query.MessagePushManage.DeleteTime.IsNull(), query.MessagePushManage.Status.Eq(1)).
		Where(query.User.TenantID.Eq(tenantId)).Distinct(query.MessagePushManage.PushID).Select(query.MessagePushManage.ALL).Scan(&result)
}

func MessagePushSendLogSave(log *model.MessagePushLog) error {
	return query.MessagePushLog.Save(log)
}

func GetUserMessagePushManage(userId string) (*model.MessagePushManage, error) {
	return query.MessagePushManage.Where(
		query.MessagePushManage.UserID.Eq(userId),
		query.MessagePushManage.DeleteTime.IsNull(),
		query.MessagePushManage.Status.Eq(1),
	).First()
}

// GetUserMessagePushManages 查询用户的所有有效推送记录（支持多设备）
func GetUserMessagePushManages(userId string) ([]*model.MessagePushManage, error) {
	return query.MessagePushManage.Where(
		query.MessagePushManage.UserID.Eq(userId),
		query.MessagePushManage.DeleteTime.IsNull(),
		query.MessagePushManage.Status.Eq(1),
	).Find()
}

func GetMessagePushMangeInactiveWithSeven() error {
	var result []model.MessagePushManage
	err := query.MessagePushManage.Where(query.MessagePushManage.InactiveTime.Lt(time.Now().Add(-7 * time.Hour * 24))).Scan(&result)
	if err != nil {
		return err
	}
	//查询最近是否上线 如果上线取消 不活跃标识
	var userId []string
	for _, v := range result {
		userId = append(userId, v.UserID)
	}
	var inactiveUserId []string
	err = query.User.Where(query.User.LastVisitTime.Lt(time.Now().Add(-30 * time.Hour * 24))).Where(query.User.ID.In(userId...)).
		Select(query.User.ID).Scan(&inactiveUserId)
	if err != nil {
		return err
	}
	var (
		mangeIds  []string
		activeIds []string
	)
	for _, v := range result {
		if ContainsFunc(inactiveUserId, v.UserID, func(a, b string) bool {
			return a == b
		}) {
			mangeIds = append(mangeIds, v.ID)
		} else {
			activeIds = append(activeIds, v.ID)
		}
	}
	_, err = query.MessagePushManage.Where(query.MessagePushManage.ID.In(mangeIds...)).Update(query.MessagePushManage.Status, 2)
	if err != nil {
		return err
	}
	_, err = query.MessagePushManage.Where(query.MessagePushManage.ID.In(activeIds...)).Update(query.MessagePushManage.InactiveTime, nil)
	return err
}

func GetMessagePushMangeInactive() error {
	var result []model.MessagePushManage
	err := query.MessagePushManage.Where(query.MessagePushManage.ErrCount.Gt(3), query.MessagePushManage.InactiveTime.IsNull()).Scan(&result)
	if err != nil {
		return err
	}
	var userId []string
	for _, v := range result {
		userId = append(userId, v.UserID)
	}
	var inactiveUserId []string
	err = query.User.Where(query.User.LastVisitTime.Lt(time.Now().Add(-30 * time.Hour * 24))).Where(query.User.ID.In(userId...)).
		Select(query.User.ID).Scan(&inactiveUserId)
	if err != nil {
		return err
	}
	var inactiveIds []string
	for _, v := range result {
		if ContainsFunc(inactiveUserId, v.UserID, func(a, b string) bool {
			return a == b
		}) {
			inactiveIds = append(inactiveIds, v.ID)
		}
	}
	_, err = query.MessagePushManage.Where(query.MessagePushManage.ID.In(inactiveIds...)).Update(query.MessagePushManage.InactiveTime, time.Now())
	return err
}

func ContainsFunc[T any](slice []T, target T, equal func(a, b T) bool) bool {
	for _, item := range slice {
		if equal(item, target) {
			return true
		}
	}
	return false
}
