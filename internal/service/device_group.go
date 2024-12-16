package service

import (
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
)

type DeviceGroup struct{}

type TreeNode struct {
	Group    *model.Group `json:"group"`
	Children []*TreeNode  `json:"children,omitempty"`
}

func (*DeviceGroup) CreateDeviceGroup(req model.CreateDeviceGroupReq, claims *utils.UserClaims) error {
	var deviceGroup = model.Group{}
	t := time.Now().UTC()
	deviceGroup.ID = uuid.New()

	// 处理子分组创建
	if req.ParentId != nil {
		deviceGroup.ParentID = req.ParentId

		// 验证子分组重名
		g, err := dal.GetChildrenGroupNameExist(*req.ParentId, req.Name, claims.TenantID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error":      err.Error(),
				"parent_id":  *req.ParentId,
				"group_name": req.Name,
				"tenant_id":  claims.TenantID,
			})
		}
		if g != nil {
			return errcode.WithVars(203002, map[string]interface{}{
				"group_name": req.Name,
				"parent_id":  *req.ParentId,
			})
		}

		// TODO: 建议添加父分组存在性验证
		parentGroup, err := dal.GetDeviceGroupDetail(*req.ParentId)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error":     err.Error(),
				"parent_id": *req.ParentId,
			})
		}
		if parentGroup == nil {
			return errcode.WithVars(errcode.CodeNotFound, map[string]interface{}{
				"error":     "parent_group_not_found",
				"parent_id": *req.ParentId,
			})
		}
	} else {
		// 验证顶级分组重名
		g, err := dal.GetTopGroupNameExist(req.Name, claims.TenantID)
		if err != nil {
			return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"error":      err.Error(),
				"group_name": req.Name,
				"tenant_id":  claims.TenantID,
			})
		}
		if g.ID != "" {
			return errcode.WithVars(203003, map[string]interface{}{
				"group_name": req.Name,
			})
		}
	}

	// 设置分组基本信息
	deviceGroup.Tier = -1 // 暂时不计算层级
	deviceGroup.Description = req.Description
	deviceGroup.CreatedAt = t
	deviceGroup.UpdatedAt = t
	deviceGroup.Name = req.Name
	deviceGroup.Remark = req.Remark
	deviceGroup.TenantID = claims.TenantID

	// 创建分组
	if err := dal.CreateDeviceGroup(&deviceGroup); err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"group_id":  deviceGroup.ID,
			"tenant_id": claims.TenantID,
		})
	}

	return nil
}

func (*DeviceGroup) DeleteDeviceGroup(id string) error {
	return dal.DeleteDeviceGroup(id)
}

func (*DeviceGroup) UpdateDeviceGroup(req model.UpdateDeviceGroupReq, claims *utils.UserClaims) error {
	// 验证分组是否冲突
	if req.Id == req.ParentId {
		return errcode.WithVars(errcode.CodeParamError, map[string]interface{}{
			"error":     "group_id_conflict",
			"message":   "old group id is same as new group id",
			"group_id":  req.Id,
			"parent_id": req.ParentId,
		})
	}

	// 构建更新对象
	var deviceGroup = model.Group{
		ID:          req.Id,
		ParentID:    &req.ParentId,
		UpdatedAt:   time.Now(),
		Name:        req.Name,
		Remark:      req.Remark,
		Description: req.Description,
		TenantID:    claims.TenantID,
	}

	// 更新数据库
	if err := dal.UpdateDeviceGroup(&deviceGroup); err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"group_id":  req.Id,
			"tenant_id": claims.TenantID,
		})
	}

	return nil
}

func (*DeviceGroup) GetDeviceGroupListByPage(req model.GetDeviceGroupsListByPageReq, userClaims *utils.UserClaims) (interface{}, error) {
	total, list, err := dal.GetDeviceGroupListByPage(req, userClaims.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"tenant_id": userClaims.TenantID,
			"page":      req.Page,
			"page_size": req.PageSize,
		})
	}
	deviceGroupList := make(map[string]interface{})
	deviceGroupList["total"] = total
	deviceGroupList["list"] = list

	return deviceGroupList, err

}

func (*DeviceGroup) GetDeviceGroupByTree(userClaims *utils.UserClaims) (interface{}, error) {
	data, err := dal.GetDeviceGroupAll(userClaims.TenantID)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":     err.Error(),
			"tenant_id": userClaims.TenantID,
		}), nil
	}

	nodeMap := make(map[string]*TreeNode)
	var rootNodes []*TreeNode

	// Initialize nodes
	for _, group := range data {
		nodeMap[group.ID] = &TreeNode{
			Group: group,
		}
	}

	// Build tree
	for _, node := range nodeMap {
		if node.Group.ParentID == nil || *node.Group.ParentID == "0" {
			rootNodes = append(rootNodes, node)
		} else {
			if parentNode, ok := nodeMap[*node.Group.ParentID]; ok {
				parentNode.Children = append(parentNode.Children, node)
			}
		}
	}

	return rootNodes, nil
}

func (*DeviceGroup) GetDeviceGroupDetail(id string) (interface{}, error) {

	dataMap := make(map[string]interface{})

	data, err := dal.GetDeviceGroupDetail(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":    err.Error(),
			"group_id": id,
		}), nil
	}

	tier, err := dal.GetDeviceGroupTierById(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"error":    err.Error(),
			"group_id": id,
		}), nil
	}

	dataMap["detail"] = data
	dataMap["tier"] = tier

	return dataMap, nil
}

func (*DeviceGroup) CreateDeviceGroupRelation(req model.CreateDeviceGroupRelationReq, claims *utils.UserClaims) error {
	var dataList = []*model.RGroupDevice{}
	for _, v := range req.DeviceIDList {
		var deviceGroupRelation = model.RGroupDevice{}
		deviceGroupRelation.DeviceID = v
		deviceGroupRelation.GroupID = req.GroupId
		deviceGroupRelation.TenantID = claims.TenantID
		dataList = append(dataList, &deviceGroupRelation)
	}
	// 批量创建
	return dal.BatchCreateRGroupDevice(dataList)
}

func (*DeviceGroup) DeleteDeviceGroupRelation(group_id, device_id string) error {
	err := dal.DeleteRGroupDevice(group_id, device_id)
	return err
}

func (*DeviceGroup) GetDeviceGroupRelation(req model.GetDeviceListByGroup) (interface{}, error) {
	total, list, err := dal.GetRGroupDeviceByGroupId(req)
	if err != nil {
		return nil, err
	}
	devicesList := make(map[string]interface{})
	devicesList["total"] = total
	devicesList["list"] = list

	return devicesList, err
}

func (*DeviceGroup) GetDeviceGroupByDeviceId(device_id string) (interface{}, error) {
	var rspData = []map[string]interface{}{}
	data, err := dal.GetRGroupDeviceByDeviceId(device_id)
	//分组名称处理成xxx/xxx/xxx
	for i := range data {
		tier, err := dal.GetDeviceGroupTierById(data[i].GroupID)
		if err != nil {
			return nil, err
		}
		rspData = append(rspData, map[string]interface{}{
			"group_id": data[i].GroupID,
			"tier":     tier["group_path"],
		})
	}

	return rspData, err
}
