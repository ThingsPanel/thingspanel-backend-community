package dal

import (
	model "project/internal/model"
	query "project/query"
)

func CreateDictLanguage(dictLanguage *model.SysDictLanguage, tx *query.QueryTx) error {
	if tx != nil {
		return tx.SysDictLanguage.Create(dictLanguage)
	} else {
		return query.SysDictLanguage.Create(dictLanguage)
	}
}

func DeleteDictLanguageById(id string) error {
	_, err := query.SysDictLanguage.Where(query.SysDictLanguage.ID.Eq(id)).Delete()
	return err
}

func GetDictLanguageByDictIdListAndLanguageCode(dictIdList []string, languageCode string) (dictLanList []*model.SysDictLanguage, err error) {
	q := query.SysDictLanguage
	if len(languageCode) != 0 {
		dictLanList, err = q.Select(q.ALL).Where(q.DictID.In(dictIdList...)).Where(q.LanguageCode.Eq(languageCode)).Find()
		if len(dictIdList) == 0 {
			return dictLanList, nil
		}
	} else {
		dictLanList, err = q.Select(q.ALL).Where(q.DictID.In(dictIdList...)).Find()
	}
	return dictLanList, err
}

func GetDictLanguageListByDictId(dictId string) ([]*model.SysDictLanguage, error) {
	q := query.SysDictLanguage
	var d []*model.SysDictLanguage
	d, err := q.Select(q.ALL).Where(q.DictID.Eq(dictId)).Order(q.LanguageCode).Find()
	return d, err
}
