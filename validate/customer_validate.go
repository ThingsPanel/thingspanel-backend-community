package valid

// PaginateCustomer  校验
type PaginateCustomer struct {
	Search string `json:"search" alias:"查询内容" valid:"MaxSize(36)"`
	Limit  int    `json:"limit" alias:"条数" valid:"Max(100)"`
	Page   int    `json:"page" alias:"页面" valid:"Min(1)"`
}

// AddCustomer 校验
type AddCustomer struct {
	Title string `json:"title" alias:"用户名称" valid:"Required; MaxSize(255)"`
	Email string `json:"email" alias:"邮箱" valid:"Required; Email; MaxSize(255)"`
}

// EditCustomer 校验
type EditCustomer struct {
	ID             string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	Title          string `json:"title" alias:"用户名称" valid:"Required; MaxSize(255)"`
	Email          string `json:"email" alias:"邮箱" valid:"Required; Email; MaxSize(255)"`
	AdditionalInfo string `json:"additional_info" alias:"附加信息" valid:"MaxSize(255)"`
	Address        string `json:"address" alias:"地址" valid:"MaxSize(255)"`
	Address2       string `json:"address2" alias:"地址2" valid:"MaxSize(255)"`
	City           string `json:"city" alias:"城市" valid:"MaxSize(255)"`
	Country        string `json:"country" alias:"国家" valid:"MaxSize(255)"`
	Phone          string `json:"phone" alias:"电话" valid:"MaxSize(255)"`
	Zip            string `json:"zip" alias:"邮编" valid:"MaxSize(255)"`
}

// DeleteCustomer 校验
type DeleteCustomer struct {
	ID string `alias:"ID" valid:"Required; MaxSize(36)"`
}
