package valid

// PaginateProduction  校验
type PaginateProduction struct {
}

// AddProduction 校验
type AddProduction struct {
	Type      string `alias:"种植类型" valid:"Required"`
	Name      string `alias:"种植名称" valid:"Required; MaxSize(255)"`
	CreatedAt string `json:"created_at" alias:"时间" valid:"Required"`
	Value     string `alias:"产出结果" valid:"Required; MaxSize(255)"`
	Remark    string `alias:"备注" valid:"MaxSize(255)"`
}

// EditProduction 校验
type EditProduction struct {
	ID        string `alias:"ID" valid:"Required; MaxSize(36)"`
	Type      string `alias:"种植类型" valid:"Required"`
	Name      string `alias:"种植名称" valid:"Required; MaxSize(255)"`
	CreatedAt string `json:"created_at" alias:"时间" valid:"Required"`
	Value     string `alias:"产出结果" valid:"Required; MaxSize(255)"`
	Remark    string `alias:"备注" valid:"MaxSize(255)"`
}

// UpdateProduction 校验
type UpdateProduction struct {
	ID string `alias:"ID" valid:"Required; MaxSize(36)"`
}

// DeleteProduction 校验
type DeleteProduction struct {
	ID string `alias:"ID" valid:"Required; MaxSize(36)"`
}
