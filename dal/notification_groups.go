package dal

import (
	"context"
	"fmt"
	"math"

	model "project/model"
	query "project/query"
	utils "project/utils"

	"github.com/sirupsen/logrus"
)

func CreateNotificationGroup(notificationGroup *model.NotificationGroup) error {
	return query.NotificationGroup.Create(notificationGroup)
}

func UpdateNotificationGroup(notificationGroup *model.NotificationGroup) error {
	if _, err := query.NotificationGroup.Where(query.NotificationGroup.ID.Eq(notificationGroup.ID)).Updates(notificationGroup); err != nil {
		return err
	}
	return nil
}

func DeleteNotificationGroup(id string) error {
	res, err := query.NotificationGroup.Where(query.NotificationGroup.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	if res.RowsAffected == 0 {
		logrus.Error("delete notification group failed: not found ", id)
		return fmt.Errorf("delete notification group failed: not found %s", id)
	}
	logrus.Info("delete notification group success: id ", id)
	return err
}

func GetNotificationGroupList(page, pageSize int) (int64, interface{}, error) {
	var count int64
	queryBuilder := query.NotificationGroup.WithContext(context.Background())
	if page != 0 && pageSize != 0 {
		queryBuilder = queryBuilder.Limit(pageSize)
		queryBuilder = queryBuilder.Offset((page - 1) * pageSize)
	}
	notificationGroupList, err := queryBuilder.Select().Find()
	if err != nil {
		return count, notificationGroupList, err
	}
	count, err = queryBuilder.Count()
	return count, notificationGroupList, err
}

func GetNotificationGroupById(id string) (*model.NotificationGroup, error) {
	p := query.NotificationGroup
	notificationGroup, err := query.NotificationGroup.Where(p.ID.Eq(id)).Select().First()
	if err != nil {
		return nil, err
	}
	return notificationGroup, err
}

func GetNotificationGroupByTenantId(tenantid string) (notificationGroups []*model.NotificationGroup, count int, err error) {
	q := query.NotificationGroup
	notificationGroups, err = q.Where(q.TenantID.Eq(tenantid)).Find()
	if err != nil {
		return nil, 0, err
	}
	count = len(notificationGroups)
	return notificationGroups, count, err
}

func GetNotificationGroupListByPage(notifications *model.GetNotificationGroupListByPageReq, u *utils.UserClaims) (int64, []*model.NotificationGroup, error) {
	if notifications.Page <= 0 || notifications.PageSize <= 0 {
		return 0, nil, fmt.Errorf("page and pageSize must be greater than 0")
	}

	q := query.NotificationGroup
	var count int64
	queryBuilder := q.WithContext(context.Background())
	if notifications.Name != nil {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *notifications.Name)))
	}

	if notifications.NotificationType != nil {
		queryBuilder = queryBuilder.Where(q.NotificationType.Eq(*notifications.NotificationType))
	}

	if notifications.Status != nil {
		queryBuilder = queryBuilder.Where(q.Status.Eq(*notifications.Status))
	}

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(u.TenantID))

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}

	// toal_pages向上取整
	total_pages := int64(math.Ceil(float64(count) / float64(notifications.PageSize)))

	queryBuilder = queryBuilder.Limit(notifications.PageSize)
	queryBuilder = queryBuilder.Offset((notifications.Page - 1) * notifications.PageSize)

	notificationList, err := queryBuilder.Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		logrus.Error("queryBuilder.Find error: ", err)
	}
	return total_pages, notificationList, err
}
