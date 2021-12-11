package valid

// Login 校验
type LoginValidate struct {
	Email    string `json:"email" alias:"用户名" valid:"Email; MaxSize(100)"`
	Password string `json:"password" alias:"密码" valid:"Required; MinSize(6)"`
}

// Register 校验
type RegisterValidate struct {
	Name       string `json:"name" alias:"用户名" valid:"Required; MaxSize(255)"`
	Email      string `json:"email" alias:"邮箱" valid:"Email; MaxSize(100)"`
	Password   string `json:"password" alias:"密码" valid:"Required; MinSize(6)"`
	CustomerID string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
}
