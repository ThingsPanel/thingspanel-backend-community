package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID       string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
}

// AddDevice 校验
type AddDevice struct {
	Token    string `json:"token"`
	Protocol string `json:"protocol"`
}
