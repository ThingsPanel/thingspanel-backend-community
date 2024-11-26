package service

import (
	"fmt"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
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

	// 代表创建的是子分组
	if req.ParentId != nil {
		deviceGroup.ParentID = req.ParentId

		// 验证子分组重名问题
		g, err := dal.GetChildrenGroupNameExist(*req.ParentId, req.Name, claims.TenantID)
		if err != nil {
			return err
		}
		if g != nil {
			return fmt.Errorf("group name is exist")
		}
	}

	// TODO 缺少验证父分组是否真实存在

	// 验证重名问题(创建的是顶级)
	if req.ParentId == nil {
		// 查找
		g, err := dal.GetTopGroupNameExist(req.Name, claims.TenantID)
		if err != nil {
			return err
		}
		if g.ID != "" {
			return fmt.Errorf("group name is exist")
		}
	}

	// 暂时不计算层级
	deviceGroup.Tier = -1
	deviceGroup.Description = req.Description
	deviceGroup.CreatedAt = t
	deviceGroup.UpdatedAt = t
	deviceGroup.Name = req.Name
	deviceGroup.Remark = req.Remark
	deviceGroup.TenantID = claims.TenantID

	return dal.CreateDeviceGroup(&deviceGroup)
}

func (*DeviceGroup) DeleteDeviceGroup(id string) error {
	return dal.DeleteDeviceGroup(id)
}

func (*DeviceGroup) UpdateDeviceGroup(req model.UpdateDeviceGroupReq, claims *utils.UserClaims) error {
	// 验证分组是否冲突
	if req.Id == req.ParentId {
		return fmt.Errorf("原分组不得与目标分组相同")
	}

	var deviceGroup = model.Group{}

	deviceGroup.ID = req.Id
	deviceGroup.ParentID = &req.ParentId
	deviceGroup.UpdatedAt = time.Now()
	deviceGroup.Name = req.Name
	deviceGroup.Remark = req.Remark
	deviceGroup.Description = req.Description
	deviceGroup.TenantID = claims.TenantID

	return dal.UpdateDeviceGroup(&deviceGroup)
}

func (*DeviceGroup) GetDeviceGroupListByPage(req model.GetDeviceGroupsListByPageReq, userClaims *utils.UserClaims) (interface{}, error) {
	total, list, err := dal.GetDeviceGroupListByPage(req, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	deviceGroupList := make(map[string]interface{})
	deviceGroupList["total"] = total
	deviceGroupList["list"] = list

	return deviceGroupList, err

}

func (*DeviceGroup) GetDeviceGroupByTree(userClaims *utils.UserClaims) (interface{}, error) {
	data, err := dal.GetDeviceGroupAll(userClaims.TenantID)
	if err != nil {
		return nil, err
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
		return nil, err
	}

	tier, err := dal.GetDeviceGroupTierById(id)
	if err != nil {
		return nil, err
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
