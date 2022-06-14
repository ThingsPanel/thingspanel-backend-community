package valid

// HomeShow 校验
type HomeShowValidate struct {
	ID string `json:"did" alias:"设备" valid:"Required;"`
}

// 协议 校验
type ProtocolValidate struct {
	Protocol string `json:"protocol" alias:"协议" valid:"Required;"`
}
