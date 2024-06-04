package model

type CreateDictLanguageReq struct {
	DictId       string `json:"dict_id" validate:"required,max=36"`
	LanguageCode string `json:"language_code"  validate:"required,max=36"`
	Translation  string `json:"translation" validate:"required,max=255"`
}
