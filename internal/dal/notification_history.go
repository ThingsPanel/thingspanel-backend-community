package dal

import (
	"context"
	"fmt"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func GetNotificationHisoryListByPage(notifications *model.GetNotificationHistoryListByPageReq) (int64, []*model.NotificationHistory, error) {
	q := query.NotificationHistory
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(notifications.TenantID))
	if notifications.NotificationType != nil && *notifications.NotificationType != "" {
		queryBuilder = queryBuilder.Where(q.NotificationType.Like(fmt.Sprintf("%%%s%%", *notifications.NotificationType)))
	}

	if notifications.SendTarget != nil && *notifications.SendTarget != "" {
		queryBuilder = queryBuilder.Where(q.SendTarget.Eq(*notifications.SendTarget))
	}

	if notifications.SendTimeStart != nil && notifications.SendTimeStop != nil {
		queryBuilder = queryBuilder.Where(q.SendTime.Between(*notifications.SendTimeStart, *notifications.SendTimeStop))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	queryBuilder = queryBuilder.Limit(notifications.PageSize)
	queryBuilder = queryBuilder.Offset((notifications.Page - 1) * notifications.PageSize).Order(q.SendTime.Desc())

	notificationList, err := queryBuilder.Find()
	if err != nil {
		logrus.Error("queryBuilder.Find error: ", err)
	}
	return count, notificationList, err
}

func CreateNotificationHistory(notificationHistory *model.NotificationHistory) error {
	err := query.NotificationHistory.Create(notificationHistory)
	if err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}

// UpdateNotificationHistory 更新通知历史记录状态
func UpdateNotificationHistory(id string, status *string, remark *string) (int64, error) {
	q := query.NotificationHistory
	updates := make(map[string]interface{})
	
	if status != nil {
		updates["send_result"] = *status
	}
	if remark != nil {
		updates["remark"] = *remark
	}
	
	result, err := q.Where(q.ID.Eq(id)).Updates(updates)
	if err != nil {
		logrus.Error("更新通知历史记录失败:", err)
		return 0, err
	}
	
	return result.RowsAffected, nil
}

// UpdateNotificationHistoryWithContent 更新通知历史记录状态和内容
func UpdateNotificationHistoryWithContent(id string, status *string, remark *string, content *string) (int64, error) {
	q := query.NotificationHistory
	updates := make(map[string]interface{})
	
	if status != nil {
		updates["send_result"] = *status
	}
	if remark != nil {
		updates["remark"] = *remark
	}
	if content != nil {
		updates["send_content"] = *content
	}
	
	result, err := q.Where(q.ID.Eq(id)).Updates(updates)
	if err != nil {
		logrus.Error("更新通知历史记录失败:", err)
		return 0, err
	}
	
	return result.RowsAffected, nil
}
