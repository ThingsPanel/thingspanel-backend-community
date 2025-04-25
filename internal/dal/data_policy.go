package dal

import (
	"context"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func CreateDataPolicy(datapolicy *model.DataPolicy) error {
	return query.DataPolicy.Create(datapolicy)
}

func UpdateDataPolicy(datapolicy *model.DataPolicy) error {
	p := query.DataPolicy
	_, err := query.DataPolicy.Where(p.ID.Eq(datapolicy.ID)).Updates(datapolicy)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func DeleteDataPolicy(id string) error {
	_, err := query.DataPolicy.Where(query.DataPolicy.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetDataPolicyListByPage(datapolicy *model.GetDataPolicyListByPageReq) (int64, interface{}, error) {
	q := query.DataPolicy
	var count int64
	var datapolicyList interface{}
	queryBuilder := q.WithContext(context.Background())

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, datapolicyList, err
	}

	if datapolicy.Page != 0 && datapolicy.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(datapolicy.PageSize)
		queryBuilder = queryBuilder.Offset((datapolicy.Page - 1) * datapolicy.PageSize)
	}

	datapolicyList, err = queryBuilder.Select().Order(q.ID.Asc()).Find()
	if err != nil {
		logrus.Error(err)
		return count, datapolicyList, err
	}

	return count, datapolicyList, err
}

func GetDataPolicy() ([]*model.DataPolicy, error) {
	p := query.DataPolicy
	datapolicyList, err := p.Select().Find()
	return datapolicyList, err
}
