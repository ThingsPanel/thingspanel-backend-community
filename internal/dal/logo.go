package dal

import (
	"context"

	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func UpdateLogo(logoID string, logomap map[string]interface{}) error {
	p := query.Logo
	_, err := query.Logo.Where(p.ID.Eq(logoID)).Updates(logomap)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetLogoList() (int64, interface{}, error) {
	q := query.Logo
	var count int64
	queryBuilder := q.WithContext(context.Background())

	logoList, err := queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)
		return count, logoList, err
	}
	count, err = queryBuilder.Count()
	return count, logoList, err
}
