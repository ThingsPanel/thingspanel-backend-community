package valid

// TokenDevice 校验
type TokenDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// EditDevice 校验
type EditDevice struct {
	ID        string `json:"id" alias:"ID" valid:"Required;MaxSize(36)"`
	Token     string `json:"token"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Publish   string `json:"publish"`
	Subscribe string `json:"subscribe"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// AddDevice 校验
type AddDevice struct {
	Token     string `json:"token"`
	Protocol  string `json:"protocol"`
	Port      string `json:"port"`
	Publish   string `json:"publish"`
	Subscribe string `json:"subscribe"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

// DeleteDevice 校验
type DeleteDevice struct {
	ID string `json:"id" alias:"id" valid:"Required;MaxSize(36)"`
}

// ConfigureDevice 校验
type ConfigureDevice struct {
	Protocol string `json:"protocol" alias:"protocol" valid:"Required;MaxSize(36)"`
}

// ConfigureDevice 校验
type OperatingDevice struct {
	Token   string `json:"token" alias:"token" valid:"Required;MaxSize(500)"`
	Message string `json:"message" alias:"message" valid:"Required;MaxSize(500)"`
}
