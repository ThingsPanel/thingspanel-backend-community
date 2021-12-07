package valid

// HomeShow 校验
type HomeShowValidate struct {
	ID string `json:"did" alias:"设备" valid:"Required;"`
}
