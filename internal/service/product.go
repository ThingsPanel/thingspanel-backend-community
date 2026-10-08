package service

import (
	"encoding/json"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"

	"github.com/go-basic/uuid"
)

type Product struct{}

func mergeTOSImageURL(info, imageURL *string) *string {
	data := map[string]interface{}{}
	if info != nil && *info != "" {
		_ = json.Unmarshal([]byte(*info), &data)
	}
	if imageURL != nil {
		data["tos_image_url"] = *imageURL
	}
	raw, _ := json.Marshal(data)
	value := string(raw)
	return &value
}

func readTOSImageURL(info *string) *string {
	if info == nil || *info == "" {
		return nil
	}
	data := map[string]interface{}{}
	if json.Unmarshal([]byte(*info), &data) != nil {
		return nil
	}
	value, _ := data["tos_image_url"].(string)
	if value == "" {
		return nil
	}
	return &value
}

func hydrateProductTOSImageURLs(list []model.ProductList) {
	for index := range list {
		list[index].TOSImageURL = readTOSImageURL(list[index].AdditionalInfo)
	}
}

func (*Product) CreateProduct(req *model.CreateProductReq, claims *utils.UserClaims) (*model.Product, error) {
	now := time.Now().UTC()
	productKey := uuid.New()
	if req.ProductKey != nil && *req.ProductKey != "" {
		productKey = *req.ProductKey
	}
	info := "{}"
	if req.AdditionalInfo != nil && *req.AdditionalInfo != "" {
		info = *req.AdditionalInfo
	}
	additionalInfo := mergeTOSImageURL(&info, req.TOSImageURL)
	item := &model.Product{
		ID:             uuid.New(),
		Name:           req.Name,
		Description:    req.Description,
		ProductType:    req.ProductType,
		ProductKey:     &productKey,
		ProductModel:   req.ProductModel,
		ImageURL:       req.ImageUrl,
		CreatedAt:      now,
		Remark:         req.Remark,
		AdditionalInfo: additionalInfo,
		TOSImageURL:    req.TOSImageURL,
		TenantID:       &claims.TenantID,
		DeviceConfigID: req.DeviceConfigID,
	}
	if err := dal.CreateProduct(item); err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	return item, nil
}

func (*Product) UpdateProduct(req *model.UpdateProductReq, claims *utils.UserClaims) error {
	item, err := dal.GetProductByID(req.Id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	if item.TenantID != nil && *item.TenantID != "" && *item.TenantID != claims.TenantID && claims.Authority != "SYS_ADMIN" {
		return errcode.New(errcode.CodeNoPermission)
	}
	if req.Name != nil {
		item.Name = *req.Name
	}
	if req.Description != nil {
		item.Description = req.Description
	}
	if req.ProductModel != nil {
		item.ProductModel = req.ProductModel
	}
	if req.ImageUrl != nil {
		item.ImageURL = req.ImageUrl
	}
	if req.TOSImageURL != nil {
		item.AdditionalInfo = mergeTOSImageURL(item.AdditionalInfo, req.TOSImageURL)
		item.TOSImageURL = req.TOSImageURL
	}
	if req.ProductType != nil {
		item.ProductType = req.ProductType
	}
	if err := dal.UpdateProduct(item); err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	return nil
}

func (*Product) DeleteProduct(id string, claims *utils.UserClaims) error {
	item, err := dal.GetProductByID(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	if item.TenantID != nil && *item.TenantID != "" && *item.TenantID != claims.TenantID && claims.Authority != "SYS_ADMIN" {
		return errcode.New(errcode.CodeNoPermission)
	}
	active, inactive, err := dal.CountDevicesByProductID(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	cascade, err := guardDeleteProduct(int(active), int(inactive))
	if err != nil {
		return errcode.NewWithMessage(errcode.CodeOpDenied, err.Error())
	}
	if cascade {
		if err := dal.DeleteInactiveDevicesByProductID(id); err != nil {
			return errcode.NewWithMessage(errcode.CodeOpDenied, "产品下仍有关联设备，无法删除")
		}
	}
	if err := dal.DeleteProduct(id); err != nil {
		return errcode.NewWithMessage(errcode.CodeOpDenied, "产品下仍有关联设备，无法删除")
	}
	return nil
}

func (*Product) GetProductListByPage(req *model.GetProductListByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	tenantID := claims.TenantID
	if claims.Authority == "SYS_ADMIN" {
		tenantID = ""
	}
	total, list, err := dal.GetProductListByPage(req, tenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{"sql_error": err.Error()})
	}
	hydrateProductTOSImageURLs(list)
	return map[string]interface{}{"total": total, "list": list}, nil
}
