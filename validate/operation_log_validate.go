package valid

// OperationLog 校验
type OperationLogListValidate struct {
	Page  int    `json:"page" alias:"页码" valid:"Required;Min(1)"`
	Limit int    `json:"limit" alias:"条数" valid:"Required;Min(10)"`
	Ip    string `json:"ip" alias:"ip" valid:"MaxSize(128)"`
	Path  string `json:"path" alias:"path" valid:"MaxSize(255)"`
}
