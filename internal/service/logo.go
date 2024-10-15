package service

import (
	dal "project/internal/dal"
	model "project/internal/model"

	"github.com/sirupsen/logrus"
)

type Logo struct{}

func (p *Logo) UpdateLogo(UpdateLogoReq *model.UpdateLogoReq) error {
	condsMap, err := StructToMapAndVerifyJson(UpdateLogoReq)
	if err != nil {
		return err
	}

	err = dal.UpdateLogo(UpdateLogoReq.Id, condsMap)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (p *Logo) GetLogoList() (map[string]interface{}, error) {

	total, list, err := dal.GetLogoList()
	if err != nil {
		return nil, err
	}
	logoListRsp := make(map[string]interface{})
	logoListRsp["total"] = total
	logoListRsp["list"] = list

	return logoListRsp, err
}
