package service

import (
	"github.com/go-basic/uuid"
	"github.com/jinzhu/copier"
	"project/dal"
	"project/model"
	"project/query"
	"time"
)

type ServiceAccess struct{}

func (s *ServiceAccess) CreateAccess(req *model.CreateAccessReq) (map[string]interface{}, error) {
	var serviceAccess model.ServiceAccess
	copier.Copy(&serviceAccess, req)
	serviceAccess.ID = uuid.New()
	if *serviceAccess.ServiceAccessConfig == "" {
		*serviceAccess.ServiceAccessConfig = "{}"
	}
	serviceAccess.CreateAt = time.Now().UTC()
	serviceAccess.UpdateAt = time.Now().UTC()
	err := query.ServiceAccess.Create(&serviceAccess)
	if err != nil {
		return nil, err
	}
	resp := make(map[string]interface{})
	resp["id"] = serviceAccess.ID
	return resp, nil
}

func (s *ServiceAccess) List(req *model.GetServiceAccessByPageReq) (map[string]interface{}, error) {
	total, list, err := dal.GetServiceAccessListByPage(req)
	listRsp := make(map[string]interface{})
	listRsp["total"] = total
	listRsp["list"] = list

	return listRsp, err
}

func (s *ServiceAccess) Update(req *model.UpdateAccessReq) error {
	updates := make(map[string]interface{})
	updates["service_access_config"] = req.ServiceAccessConfig
	updates["update_at"] = time.Now().UTC()
	err := dal.UpdateServiceAccess(req.ID, updates)
	return err
}

func (s *ServiceAccess) Delete(req *model.DeleteAccessReq) error {
	err := dal.DeleteServiceAccess(req.ID)
	return err
}
