// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package model

const TableNameSysDictLanguage = "sys_dict_language"

// SysDictLanguage mapped from table <sys_dict_language>
type SysDictLanguage struct {
	ID           string `gorm:"column:id;primaryKey;comment:主键ID" json:"id"`                     // 主键ID
	DictID       string `gorm:"column:dict_id;not null;comment:sys_dict.id" json:"dict_id"`      // sys_dict.id
	LanguageCode string `gorm:"column:language_code;not null;comment:语言代码" json:"language_code"` // 语言代码
	Translation  string `gorm:"column:translation;not null;comment:翻译" json:"translation"`       // 翻译
}

// TableName SysDictLanguage's table name
func (*SysDictLanguage) TableName() string {
	return TableNameSysDictLanguage
}
