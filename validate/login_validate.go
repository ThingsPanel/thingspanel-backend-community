package valid

// Login 校验
type LoginValidate struct {
	PhoneNumber      string `json:"phone_number" alias:"手机号" valid:"MaxSize(11)"`
	Email            string `json:"email" alias:"用户名" valid:"MaxSize(100)"`
	Password         string `json:"password" alias:"密码" valid:"MinSize(6)"`
	VerificationCode string `json:"verification_code" alias:"验证码" valid:"MaxSize(36)"`
}

// Register 校验
type RegisterValidate struct {
	Name       string `json:"name" alias:"用户名" valid:"Required; MaxSize(255)"`
	Email      string `json:"email" alias:"邮箱" valid:"Email; MaxSize(100)"`
	Password   string `json:"password" alias:"密码" valid:"Required; MinSize(6)"`
	CustomerID string `json:"customer_id" alias:"客户" valid:"MaxSize(36)"`
}

// Register 校验
type TenantRegisterValidate struct {
	Email            string `json:"email" alias:"邮箱" valid:"Email; MaxSize(100)"`
	Password         string `json:"password" alias:"密码" valid:"Required; MinSize(6)"`
	PhoneNumber      string `json:"phone_number" alias:"手机号" valid:"Required;MaxSize(11)"`
	VerificationCode string `json:"verification_code" alias:"验证码" valid:"Required;MaxSize(36)"`
}
