package valid

// NavigationAdd 校验
type NavigationAdd struct {
	Type int64  `json:"type" alias:"类型" valid:"Required;"`
	Name string `json:"name" alias:"名称" valid:"Required;MaxSize(255)"`
	Data string `json:"data" alias:"数据" valid:"Required;MaxSize(255)"`
}
