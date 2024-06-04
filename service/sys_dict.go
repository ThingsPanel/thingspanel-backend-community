package service

import (
	"fmt"
	"time"

	dal "project/dal"
	model "project/model"
	utils "project/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type Dict struct{}

func (d *Dict) CreateDictColumn(createDictReq *model.CreateDictReq, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return fmt.Errorf("wrong user authority")
	}

	var dict = model.SysDict{}

	dict.ID = uuid.New()
	dict.DictCode = createDictReq.DictCode
	dict.DictValue = createDictReq.DictValue
	dict.CreatedAt = time.Now().UTC()
	dict.Remark = createDictReq.Remark

	err := dal.CreateDict(&dict, nil)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (d *Dict) CreateDictLanguage(createDictLanguage *model.CreateDictLanguageReq, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return fmt.Errorf("wrong user authority")
	}

	// 验证sys_dict的id是否存在
	_, err := dal.GetDictById(createDictLanguage.DictId)
	if err != nil {
		logrus.Error(err)
		return err
	}
	// 创建 sys_dict_language
	var dictLanguage = model.SysDictLanguage{}

	dictLanguage.ID = uuid.New()
	dictLanguage.DictID = createDictLanguage.DictId
	dictLanguage.LanguageCode = createDictLanguage.LanguageCode
	dictLanguage.Translation = createDictLanguage.Translation

	err = dal.CreateDictLanguage(&dictLanguage, nil)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (d *Dict) DeleteDict(id string, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return fmt.Errorf("wrong user authority")
	}
	err := dal.DeleteDictById(id)
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func (d *Dict) DeleteDictLanguage(id string, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return fmt.Errorf("wrong user authority")
	}
	err := dal.DeleteDictLanguageById(id)
	if err != nil {
		logrus.Error(err)
	}

	return nil
}

func (d *Dict) GetDict(params *model.DictListReq) (list []model.DictListRsp, err error) {
	dict, err := dal.GetDictListByCode(params.DictCode)
	if err != nil {
		logrus.Error(err)
		return list, err
	}
	var dictIDList []string
	for _, v := range dict {
		dictIDList = append(dictIDList, v.ID)
	}

	var lanCode string
	if params.LanguageCode == nil {
		lanCode = ""
	} else {
		lanCode = *params.LanguageCode
	}

	langList, err := dal.GetDictLanguageByDictIdListAndLanguageCode(dictIDList, lanCode)
	if err != nil {
		logrus.Error(err)
		return list, err
	}

	for _, v := range dict {
		var tmp model.DictListRsp
		var trans string
		for _, v2 := range langList {
			if v2.DictID == v.ID {
				trans = v2.Translation
			}
		}

		if len(trans) != 0 {
			tmp.DictValue = v.DictValue
			tmp.Translation = trans
			list = append(list, tmp)
		} else {
			tmp.DictValue = v.DictValue
			tmp.Translation = ""
			list = append(list, tmp)
		}

	}

	return list, err
}

// 获取协议接入下拉菜单
func (d *Dict) GetProtocolMenu(protocolMenuReq *model.ProtocolMenuReq) (reqData []map[string]interface{}, err error) {
	if protocolMenuReq.LanguageCode == nil {
		protocolMenuReq.LanguageCode = StringPtr("zh")
	}
	var reqDataList []map[string]interface{}
	dict1, err := dal.GetDictLanguageByDictCodeAndLanguageCode("DRIECT_ATTACHED_PROTOCOL", *protocolMenuReq.LanguageCode)
	if err != nil {
		logrus.Error(err)
		return reqDataList, err
	}
	dict2, err := dal.GetDictLanguageByDictCodeAndLanguageCode("GATEWAY_PROTOCOL", *protocolMenuReq.LanguageCode)
	if err != nil {
		logrus.Error(err)
		return reqDataList, err
	}
	for _, v := range dict1 {
		v["device_type"] = "1"
		reqDataList = append(reqDataList, v)
	}
	for _, v := range dict2 {
		v["device_type"] = "2"
		reqDataList = append(reqDataList, v)
	}
	for _, v := range dict2 {
		// 深拷贝v
		var m = make(map[string]interface{})
		for k, v := range v {
			m[k] = v
		}
		m["device_type"] = "3"
		reqDataList = append(reqDataList, m)
	}
	return reqDataList, err
}

func (d *Dict) GetDictListByPage(params *model.GetDictLisyByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetDictListByPage(params, claims)
	if err != nil {
		return nil, err
	}
	dictListRspMap := make(map[string]interface{})
	dictListRspMap["total"] = total
	dictListRspMap["list"] = list
	return dictListRspMap, nil

}

func (d *Dict) GetDictLanguageListById(id string) ([]*model.SysDictLanguage, error) {
	data, err := dal.GetDictLanguageListByDictId(id)
	return data, err
}
