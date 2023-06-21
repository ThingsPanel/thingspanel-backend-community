package valid

// Paginate  校验
type PaginateBusiness struct {
	Name  string `json:"name" alias:"名称" valid:"MaxSize(255)"`
	Limit int    `json:"limit" alias:"条数" valid:"Max(100)"`
	Page  int    `json:"page" alias:"页面" valid:"Min(1)"`
}

// AddBusiness 校验
type AddBusiness struct {
	Name string `json:"name" alias:"名称" valid:"Required; MaxSize(255)"`
	Sort int64  `json:"sort" alias:"排序" valid:"Required;"`
}

// EditBusiness 校验
type EditBusiness struct {
	ID   string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	Name string `json:"name" alias:"名称" valid:"Required; MaxSize(255)"`
	Sort int64  `json:"sort" alias:"排序" valid:"Required;"`
}

// DeleteBusiness 校验
type DeleteBusiness struct {
	ID string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
}
