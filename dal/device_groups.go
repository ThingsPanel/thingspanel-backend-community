package dal

import (
	"context"
	"fmt"

	global "project/global"
	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
)

func CreateDeviceGroup(r *model.Group) error {
	return query.Group.Create(r)
}

func DeleteDeviceGroup(id string) error {
	_, err := query.Group.Where(query.Group.ID.Eq(id)).Delete()
	return err
}

func UpdateDeviceGroup(r *model.Group) error {
	_, err := query.Group.Where(query.Group.ID.Eq(r.ID)).Updates(r)
	return err
}

func GetDeviceGroupListByPage(req model.GetDeviceGroupsListByPageReq, tenantId string) (int64, interface{}, error) {
	q := query.Group
	var count int64
	var groupList interface{}
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantId))
	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}

	if req.ParentId != nil && *req.ParentId != "" {
		queryBuilder = queryBuilder.Where(q.ParentID.Eq(*req.ParentId))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, groupList, err
	}

	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize)
		queryBuilder = queryBuilder.Offset((req.Page - 1) * req.PageSize)
	}
	queryBuilder = queryBuilder.Order(q.CreatedAt.Desc())
	groupList, err = queryBuilder.Select().Find()
	if err != nil {
		logrus.Error(err)
		return count, groupList, err
	}

	return count, groupList, err
}

func GetDeviceGroupAll(tenantId string) ([]*model.Group, error) {
	g, err := query.Group.Where(query.Group.TenantID.Eq(tenantId)).Find()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return g, nil
}

func GetDeviceGroupDetail(id string) (*model.Group, error) {
	d, err := query.Group.Where(query.Group.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	}
	return d, err
}

func GetDeviceGroupTierById(id string) (map[string]interface{}, error) {
	r := make(map[string]interface{})
	sql := `
	WITH RECURSIVE group_chain AS (
		SELECT id, parent_id, name, 1 as level
		FROM groups
		WHERE id = ?
		UNION ALL
		SELECT g.id, g.parent_id, g.name, gc.level + 1
		FROM groups g
		INNER JOIN group_chain gc ON gc.parent_id = g.id
	  )
	  SELECT string_agg(name, '/' ORDER BY level DESC) AS group_path
	  FROM group_chain;
	`
	err := global.DB.Raw(sql, id).Scan(&r)
	if err.Error != nil {
		return nil, err.Error
	}
	return r, nil
}

// 获取目标分组的所有子分组id
func GetGroupChildrenIds(id string) ([]string, error) {
	var ids []string
	sql := `
	WITH RECURSIVE group_chain AS (
		SELECT id, parent_id
		FROM groups
		WHERE id = ?
		UNION ALL
		SELECT g.id, g.parent_id
		FROM groups g
		INNER JOIN group_chain gc ON gc.id = g.parent_id
	  )
	  SELECT id
	  FROM group_chain;
	`
	err := global.DB.Raw(sql, id).Scan(&ids)
	if err.Error != nil {
		return nil, err.Error
	}
	return ids, nil
}
