package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID       string `alias:"id" valid:"Required;MaxSize(36)"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
}
