package service

import (
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type UiElements struct{}

func (*UiElements) CreateUiElements(CreateUiElementsReq *model.CreateUiElementsReq) error {

	var UiElements = model.SysUIElement{}

	UiElements.ID = uuid.New()
	UiElements.ParentID = CreateUiElementsReq.ParentID
	UiElements.ElementCode = CreateUiElementsReq.ElementCode
	UiElements.ElementType = int16(CreateUiElementsReq.ElementType)
	aa := int16(CreateUiElementsReq.Orders)
	UiElements.Order_ = &aa
	UiElements.Param1 = CreateUiElementsReq.Param1
	UiElements.Param2 = CreateUiElementsReq.Param2
	UiElements.Param3 = CreateUiElementsReq.Param3
	UiElements.CreatedAt = time.Now().UTC()
	UiElements.Authority = CreateUiElementsReq.Authority
	UiElements.Description = CreateUiElementsReq.Description
	UiElements.Remark = CreateUiElementsReq.Remark
	UiElements.Multilingual = CreateUiElementsReq.Multilingual
	UiElements.RoutePath = CreateUiElementsReq.RoutePath
	err := dal.CreateUiElements(&UiElements)

	if err != nil {
		logrus.Error(err)
	}

	return err
}

func (*UiElements) UpdateUiElements(UpdateUiElementsReq *model.UpdateUiElementsReq) error {
	var UiElements = model.SysUIElement{}
	UiElements.ID = UpdateUiElementsReq.Id
	UiElements.ParentID = *UpdateUiElementsReq.ParentID
	UiElements.ElementCode = *UpdateUiElementsReq.ElementCode
	UiElements.ElementType = *UpdateUiElementsReq.ElementType
	UiElements.Order_ = UpdateUiElementsReq.Orders
	UiElements.Param1 = UpdateUiElementsReq.Param1
	UiElements.Param2 = UpdateUiElementsReq.Param2
	UiElements.Param3 = UpdateUiElementsReq.Param3
	UiElements.Authority = *UpdateUiElementsReq.Authority
	UiElements.Description = UpdateUiElementsReq.Description
	UiElements.Multilingual = UpdateUiElementsReq.Multilingual
	UiElements.RoutePath = UpdateUiElementsReq.RoutePath
	UiElements.Remark = UpdateUiElementsReq.Remark

	err := dal.UpdateUiElements(&UiElements)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (*UiElements) DeleteUiElements(id string) error {
	err := dal.DeleteUiElements(id)
	return err
}

func (*UiElements) ServeUiElementsListByPage(Params *model.ServeUiElementsListByPageReq) (map[string]interface{}, error) {

	total, list, err := dal.ServeUiElementsListByPage(Params)
	if err != nil {
		return nil, err
	}
	UiElementsListRsp := make(map[string]interface{})
	UiElementsListRsp["total"] = total
	UiElementsListRsp["list"] = list

	return UiElementsListRsp, err
}

func (*UiElements) ServeUiElementsListByAuthority(u *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.ServeUiElementsListByAuthority(u)
	if err != nil {
		logrus.Error("[ServeUiElementsListByAuthority] query failed:", err)
		return nil, errcode.WithData(errcode.CodeDBError, err.Error(), map[string]interface{}{
			"operation": "query_ui_elements",
			"user_id":   u.ID,
			"error":     err.Error(),
		})
	}

	return map[string]interface{}{
		"total": total,
		"list":  list,
	}, nil
}

// 获取租户下权限配置表单树
func (*UiElements) GetTenantUiElementsList() (map[string]interface{}, error) {

	list, err := dal.GetTenantUiElementsList()
	if err != nil {
		return nil, err
	}
	UiElementsListRsp := make(map[string]interface{})
	UiElementsListRsp["list"] = list

	return UiElementsListRsp, err
}
