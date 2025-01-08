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

type Dict struct{}

func (*Dict) CreateDictColumn(createDictReq *model.CreateDictReq, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": "wrong user authority",
		})
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
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	return err
}

func (*Dict) CreateDictLanguage(createDictLanguage *model.CreateDictLanguageReq, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": "wrong user authority",
		})
	}

	// 验证sys_dict的id是否存在
	_, err := dal.GetDictById(createDictLanguage.DictId)
	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
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
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	return err
}

func (*Dict) DeleteDict(id string, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": "wrong user authority",
		})
	}
	err := dal.DeleteDictById(id)
	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	return err
}

func (*Dict) DeleteDictLanguage(id string, claims *utils.UserClaims) error {
	if claims.Authority != dal.SYS_ADMIN {
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": "wrong user authority",
		})
	}
	err := dal.DeleteDictLanguageById(id)
	if err != nil {
		logrus.Error(err)
		return errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}

	return nil
}

func (*Dict) GetDict(params *model.DictListReq, lang string) (list []model.DictListRsp, err error) {
	dict, err := dal.GetDictListByCode(params.DictCode)
	if err != nil {
		logrus.Error(err)
		return list, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	var dictIDList []string
	for _, v := range dict {
		dictIDList = append(dictIDList, v.ID)
	}

	// 解析lang，提取出第一个语言并转为类似zh_CN这种格式
	lanCode := utils.FormatLangCode(lang)

	langList, err := dal.GetDictLanguageByDictIdListAndLanguageCode(dictIDList, lanCode)
	if err != nil {
		logrus.Error(err)
		return list, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
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
func (*Dict) GetProtocolMenu(protocolMenuReq *model.ProtocolMenuReq) (reqData []map[string]interface{}, err error) {
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

func (*Dict) GetDictListByPage(params *model.GetDictLisyByPageReq, claims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetDictListByPage(params, claims)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	dictListRspMap := make(map[string]interface{})
	dictListRspMap["total"] = total
	dictListRspMap["list"] = list
	return dictListRspMap, nil

}

func (*Dict) GetDictLanguageListById(id string) ([]*model.SysDictLanguage, error) {
	data, err := dal.GetDictLanguageListByDictId(id)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeParamError, map[string]interface{}{
			"err": err.Error(),
		})
	}
	return data, err
}
