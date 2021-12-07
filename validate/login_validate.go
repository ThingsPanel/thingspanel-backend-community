package valid

// Login 校验
type LoginValidate struct {
	Email    string `alias:"用户名" valid:"Email; MaxSize(100)"`
	Password string `alias:"密码" valid:"Required; MinSize(6)"`
}

// Register 校验
type RegisterValidate struct {
	Name       string `alias:"用户名" valid:"Required; MaxSize(255)"`
	Email      string `alias:"邮箱" valid:"Email; MaxSize(100)"`
	Password   string `alias:"密码" valid:"Required; MinSize(6)"`
	CustomerID string `alias:"客户" valid:"MaxSize(36)"`
}
