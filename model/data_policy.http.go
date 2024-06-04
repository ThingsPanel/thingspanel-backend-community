package model

type UpdateDataPolicyReq struct {
	Id            string  `json:"id" validate:"required,max=36"`       // Id
	RetentionDays int32   `json:"retention_days" validate:"required"`  // 数据保留时间（天）
	Enabled       string  `json:"enabled" validate:"required"`         // 是否启用：1启用 2停用
	Remark        *string `json:"remark" validate:"required,max=2000"` // 备注

}

type GetDataPolicyListByPageReq struct {
	PageReq
}
