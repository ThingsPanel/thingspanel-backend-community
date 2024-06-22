package dal

import (
	"context"
	"github.com/sirupsen/logrus"
	"project/model"
	"project/query"
)

func DeleteServiceAccess(id string) error {
	q := query.ServiceAccess
	queryBuilder := q.WithContext(context.Background())
	_, err := queryBuilder.Where(q.ID.Eq(id)).Delete()
	return err
}

func UpdateServiceAccess(id string, updates map[string]interface{}) error {
	q := query.ServiceAccess
	queryBuilder := q.WithContext(context.Background())
	_, err := queryBuilder.Where(q.ID.Eq(id)).Updates(updates)
	return err
}

func GetServiceAccessListByPage(req *model.GetServiceAccessByPageReq) (int64, interface{}, error) {
	var count int64
	var serviceAccess = []model.ServiceAccess{}

	q := query.ServiceAccess
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.ServicePluginID.Eq(req.ServicePluginID))

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, serviceAccess, err
	}
	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	err = queryBuilder.Select().Order(q.CreateAt).Scan(&serviceAccess)
	if err != nil {
		logrus.Error(err)
		return count, serviceAccess, err
	}
	return count, serviceAccess, err
}
