package dal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
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
	g, err := query.Group.Where(query.Group.TenantID.Eq(tenantId)).Order(query.Group.CreatedAt.Desc()).Find()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return g, nil
}

func GetAutoBindRootDeviceGroupID(tx *query.Query, tenantId string) (string, error) {
	rootGroups, err := tx.Group.
		Where(tx.Group.TenantID.Eq(tenantId)).
		Where(tx.Group.ParentID.Eq("0")).
		Order(tx.Group.CreatedAt.Asc()).
		Find()
	if err != nil {
		logrus.Error(err)
		return "", err
	}

	if len(rootGroups) != 1 {
		return "", nil
	}

	return rootGroups[0].ID, nil
}

func GetDeviceGroupDetail(id string) (*model.Group, error) {
	d, err := query.Group.Where(query.Group.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	}
	return d, err
}

type deviceGroupStatisticsRow struct {
	DeviceTotal  int64 `json:"device_total"`
	OnlineTotal  int64 `json:"online_total"`
	OfflineTotal int64 `json:"offline_total"`
	AlarmTotal   int64 `json:"alarm_total"`
}

func GetDeviceGroupStatistics(groupID string, tenantID string) (*model.DeviceGroupStatistics, error) {
	groupIDs, err := GetGroupChildrenIds(groupID)
	if err != nil {
		return nil, err
	}
	if len(groupIDs) == 0 {
		return &model.DeviceGroupStatistics{}, nil
	}

	deviceIDs, err := GetDeviceIdsByGroupIds(groupIDs)
	if err != nil {
		return nil, err
	}
	if len(deviceIDs) == 0 {
		return &model.DeviceGroupStatistics{}, nil
	}

	var row deviceGroupStatisticsRow
	placeholders := strings.TrimRight(strings.Repeat("?,", len(deviceIDs)), ",")
	sql := fmt.Sprintf(`
		SELECT
			COUNT(DISTINCT d.id) AS device_total,
			COALESCE(SUM(CASE WHEN d.is_online = 1 THEN 1 ELSE 0 END), 0) AS online_total,
			COALESCE(SUM(CASE WHEN d.is_online = 1 THEN 0 ELSE 1 END), 0) AS offline_total,
			COALESCE(SUM(CASE WHEN lda.alarm_status IS NOT NULL AND lda.alarm_status <> 'N' THEN 1 ELSE 0 END), 0) AS alarm_total
		FROM devices d
		LEFT JOIN latest_device_alarms lda ON lda.device_id = d.id
		WHERE d.tenant_id = ?
		  AND d.activate_flag = 'active'
		  AND d.id IN (`+placeholders+`)
	`)
	args := make([]interface{}, 0, len(deviceIDs)+1)
	args = append(args, tenantID)
	for _, deviceID := range deviceIDs {
		args = append(args, deviceID)
	}

	err = global.DB.Raw(sql, args...).Scan(&row).Error
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return &model.DeviceGroupStatistics{
		DeviceTotal:  row.DeviceTotal,
		OnlineTotal:  row.OnlineTotal,
		OfflineTotal: row.OfflineTotal,
		AlarmTotal:   row.AlarmTotal,
	}, nil
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

func GetTopGroupNameExist(name string, tenantId string) (*model.Group, error) {
	g, err := query.Group.
		Where(query.Group.TenantID.Eq(tenantId)).
		Where(query.Group.ParentID.Eq("0")).
		Where(query.Group.Name.Eq(name)).
		First()
	if err != nil {
		logrus.Error(err)
		return nil, err
	}
	return g, nil
}

func GetChildrenGroupNameExist(parentId string, name string, tenantId string) (*model.Group, error) {
	g, err := query.Group.
		Where(query.Group.TenantID.Eq(tenantId)).
		Where(query.Group.ParentID.Eq(parentId)).
		Where(query.Group.Name.Eq(name)).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return g, nil
		}
		logrus.Error(err)
		return nil, err
	}
	return g, nil
}

// GetGroupNameExistByTenant 检查租户下是否存在指定名称的分组（无论层级）
func GetGroupNameExistByTenant(name string, tenantId string) (*model.Group, error) {
	g, err := query.Group.
		Where(query.Group.TenantID.Eq(tenantId)).
		Where(query.Group.Name.Eq(name)).
		First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		logrus.Error(err)
		return nil, err
	}
	return g, nil
}
