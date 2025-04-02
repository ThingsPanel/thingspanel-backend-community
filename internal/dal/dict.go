package dal

import (
	"fmt"

	model "project/internal/model"
	query "project/internal/query"
	utils "project/pkg/utils"

	"gorm.io/gen/field"
)

func CreateDict(dict *model.SysDict, tx *query.QueryTx) error {
	if tx != nil {
		return tx.SysDict.Create(dict)
	} else {
		return query.SysDict.Create(dict)
	}
}

func GetDictById(dictId string) (*model.SysDict, error) {
	dict, err := query.SysDict.Where(query.SysDict.ID.Eq(dictId)).First()
	if err != nil {
		return nil, err
	}
	return dict, err
}

func DeleteDictById(dictId string) error {
	_, err := query.SysDict.Where(query.SysDict.ID.Eq(dictId)).Delete()
	return err
}

func GetDictListByCode(dictCode string) ([]*model.SysDict, error) {
	dict, err := query.SysDict.Where(query.SysDict.DictCode.Eq(dictCode)).Find()
	if err != nil {
		return nil, err
	}
	return dict, err
}

func GetDictListByPage(dictListReq *model.GetDictLisyByPageReq, claims *utils.UserClaims) (count int64, dictList interface{}, err error) {
	q := query.SysDict

	if claims.Authority != SYS_ADMIN {
		return count, nil, fmt.Errorf("authority exception")
	}

	if dictListReq.DictCode != nil {
		dictList, err = q.Select(q.ALL).
			Where(field.Attrs(map[string]interface{}{"dict_code": dictListReq.DictCode})).
			Order(q.CreatedAt.Desc()).
			Offset((dictListReq.Page - 1) * dictListReq.PageSize).
			Limit(dictListReq.PageSize).
			Find()
	} else {
		dictList, err = q.Select(q.ALL).
			Order(q.CreatedAt.Desc()).
			Offset((dictListReq.Page - 1) * dictListReq.PageSize).
			Limit(dictListReq.PageSize).
			Find()
	}

	if err != nil {
		return count, dictList, err
	}

	if dictListReq.DictCode != nil {
		count, err = q.Where(field.Attrs(map[string]interface{}{"dict_code": dictListReq.DictCode})).Count()

	} else {
		count, err = q.Count()

	}

	return count, dictList, err
}

// 根据字典标识符和多语言标识符获取字典
func GetDictLanguageByDictCodeAndLanguageCode(dictCode, languageCode string) ([]map[string]interface{}, error) {
	var data []map[string]interface{}
	sd := query.SysDict
	sdl := query.SysDictLanguage
	err := sd.Select(sd.DictValue, sdl.Translation).LeftJoin(sdl, sdl.DictID.EqCol(sd.ID)).Where(sd.DictCode.Eq(dictCode), sdl.LanguageCode.Eq(languageCode)).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, err
}
