package dal

import (
	"context"
	"fmt"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	"project/pkg/global"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
)

func CreateRole(data *model.Role) error {
	return query.Role.Create(data)
}

func GetRoleByID(id string) (model.Role, error) {
	var data model.Role
	err := query.Role.Where(query.Role.ID.Eq(id)).Scan(&data)
	if err != nil {
		logrus.Error(err)
	}
	return data, err
}

func UpdateRole(data *model.Role) (gen.ResultInfo, error) {
	p := query.Role

	t := time.Now().UTC()
	data.UpdatedAt = &t

	info, err := query.Role.Where(p.ID.Eq(data.ID)).Updates(data)
	if err != nil {
		logrus.Error(err)
	}
	return info, err
}

func DeleteRole(id string) error {
	_, err := query.Role.Where(query.Role.ID.Eq(id)).Delete()
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetRoleListByPage(data *model.GetRoleListByPageReq, tenantID string) (int64, interface{}, error) {
	q := query.Role
	var count int64
	var dataList interface{}
	queryBuilder := q.WithContext(context.Background())

	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	if data.Name != nil && *data.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *data.Name)))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, dataList, err
	}

	if data.Page != 0 && data.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(data.PageSize)
		queryBuilder = queryBuilder.Offset((data.Page - 1) * data.PageSize)
	}

	dataList, err = queryBuilder.Select().Order(q.UpdatedAt).Find()
	if err != nil {
		logrus.Error(err)
		return count, dataList, err
	}

	return count, dataList, err
}

// 查询用户的角色
func GetRolesByUserId(userId string) ([]string, bool) {
	policys := global.CasbinEnforcer.GetFilteredNamedGroupingPolicy("g", 0, userId)
	var roles []string
	for _, policy := range policys {
		roles = append(roles, policy[1])
	}
	return roles, true
}
