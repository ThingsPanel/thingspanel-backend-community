package valid

type TpFunctionValidate struct {
	Id           string `json:"id"  alias:"ID" valid:"MaxSize(36)"` // ID
	FunctionName string `json:"function_name"  alias:"功能名称" valid:"MaxSize(99)"`
}
