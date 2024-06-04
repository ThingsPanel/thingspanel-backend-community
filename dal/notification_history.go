package dal

import (
	"context"
	"fmt"
	"math"
	"time"

	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func GetNotificationHisoryListByPage(notifications *model.GetNotificationHistoryListByPageReq) (int64, []*model.NotificationHistory, error) {
	if notifications.Page <= 0 || notifications.PageSize <= 0 {
		return 0, nil, fmt.Errorf("page and pageSize must be greater than 0")
	}

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

	if notifications.SendTimeStart != nil && *notifications.SendTimeStart != "" {
		// string to time
		// startTime, err := time.Parse("2006-01-02 15:04:05", *notifications.SendTimestart)
		startTime, err := time.Parse("2006-01-02 15:04:05", *notifications.SendTimeStart)
		if err != nil {
			return 0, nil, err
		}

		var stopTime time.Time
		if notifications.SendTimeStop != nil && *notifications.SendTimeStop != "" {
			stopTime, err = time.Parse("2006-01-02 15:04:05", *notifications.SendTimeStop)
			if err != nil {
				return 0, nil, err
			}
		} else {
			stopTime = time.Now()
		}
		queryBuilder = queryBuilder.Where(q.SendTime.Between(startTime, stopTime))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	// toal_pages向上取整
	total_pages := int64(math.Ceil(float64(count) / float64(notifications.PageSize)))

	queryBuilder = queryBuilder.Limit(notifications.PageSize)
	queryBuilder = queryBuilder.Offset((notifications.Page - 1) * notifications.PageSize)

	notificationList, err := queryBuilder.Find()
	if err != nil {
		logrus.Error("queryBuilder.Find error: ", err)
	}
	return total_pages, notificationList, err
}

func CreateNotificationHistory(notificationHistory *model.NotificationHistory) error {
	err := query.NotificationHistory.Create(notificationHistory)
	if err != nil {
		logrus.Error(err)
		return err
	}
	return nil
}
