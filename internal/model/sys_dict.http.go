package model

type CreateDictReq struct {
	DictCode  string  `json:"dict_code" validate:"required,max=36"`
	DictValue string  `json:"dict_value"  validate:"required,max=255"`
	Remark    *string `json:"remark" validate:"omitempty,max=255"`
}

type DictListReq struct {
	DictCode     string  `json:"dict_code" form:"dict_code" validate:"required,max=36"`
	LanguageCode *string `json:"language_code" form:"language_code" validate:"omitempty,max=36"`
}

type DictListRsp struct {
	DictValue   string `json:"dict_value" form:"dict_value"`
	Translation string `json:"translation" form:"translation"`
}

type GetDictLisyByPageReq struct {
	PageReq
	DictCode *string `json:"dict_code" form:"dict_code" validate:"omitempty,max=36"`
}

type ProtocolMenuReq struct {
	LanguageCode *string `json:"language_code" form:"language_code" validate:"omitempty,max=36"`
}
