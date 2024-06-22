package dal

import (
	"context"
	"github.com/sirupsen/logrus"
	"project/model"
	"project/query"
)

func DeleteServicePlugin(id string) error {
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	_, err := queryBuilder.Where(query.ServicePlugin.ID.Eq(id)).Delete()
	return err
}

func UpdateServicePlugin(id string, updates map[string]interface{}) error {
	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	_, err := queryBuilder.Where(query.ServicePlugin.ID.Eq(id)).Updates(updates)
	return err
}

func GetServicePluginListByPage(req *model.GetServicePluginByPageReq) (int64, interface{}, error) {
	var count int64
	var servicePlugins = []model.ServicePlugin{}

	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())
	if req.ServiceType != 0 {
		queryBuilder = queryBuilder.Where(q.ServiceType.Eq(req.ServiceType))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, servicePlugins, err
	}
	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}

	err = queryBuilder.Select().Order(q.CreateAt).Scan(&servicePlugins)
	if err != nil {
		logrus.Error(err)
		return count, servicePlugins, err
	}
	return count, servicePlugins, err
}

func GetServicePlugin(req *model.GetServicePluginReq) (interface{}, error) {
	var servicePlugin model.ServicePlugin

	q := query.ServicePlugin
	queryBuilder := q.WithContext(context.Background())

	queryBuilder.Where(q.ID.Eq(req.ID))

	err := queryBuilder.Select().Scan(&servicePlugin)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return servicePlugin, err
}