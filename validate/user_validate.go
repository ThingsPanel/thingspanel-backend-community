package valid

// PaginateUser  校验
type PaginateUser struct {
	Search string `alias:"查询内容" valid:"MaxSize(36)"`
	Limit  int    `alias:"条数" valid:"Max(100)"`
	Page   int    `alias:"页面" valid:"Min(1)"`
}

// AddUser 校验
type AddUser struct {
	Name     string `alias:"姓名" valid:"Required; MaxSize(255)"`
	Email    string `alias:"邮箱" valid:"Required; Email; MaxSize(100)"`
	Password string `alias:"密码" valid:"Required; MaxSize(255)"`
	Enabled  string `alias:"状态" valid:"MaxSize(5)"`
	Mobile   string `alias:"手机号" valid:"Mobile;"`
	Remark   string `alias:"备注" valid:"MaxSize(255)"`
}

// EditUser 校验
type EditUser struct {
	ID     string `alias:"ID" valid:"Required; MaxSize(36)"`
	Name   string `alias:"姓名" valid:"Required; MaxSize(255)"`
	Email  string `alias:"邮箱" valid:"Email; MaxSize(100)"`
	Mobile string `alias:"手机号" valid:"Mobile; Required;"`
	Remark string `alias:"备注" valid:"MaxSize(255)"`
}

// DeleteUser 校验
type DeleteUser struct {
	ID string `alias:"ID" valid:"Required; MaxSize(36)"`
}

// PasswordUser 校验
type PasswordUser struct {
	ID       string `alias:"ID" valid:"Required; MaxSize(36)"`
	Password string `alias:"密码" valid:"Required; MaxSize(255)"`
}
