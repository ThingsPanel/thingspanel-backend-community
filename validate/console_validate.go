package valid

type AddConsole struct {
	// ID             string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	Name     string `json:"name" alias:"看板名称" valid:"Required; MaxSize(255)"`
	Data     string `json:"data" alias:"看板数据" valid:"MaxSize(255)"`
	Config   string `json:"config" alias:"看板配置" valid:"MaxSize(500)"`
	Template string `json:"templates" alias:"看板模版" valid:"MaxSize(500)"`
}

type EditConsole struct {
	ID       string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
	Name     string `json:"name" alias:"看板名称" valid:"MaxSize(255)"`
	Data     string `json:"data" alias:"看板数据" valid:"MaxSize(255)"`
	Config   string `json:"config" alias:"看板配置" valid:"MaxSize(500)"`
	Template string `json:"templates" alias:"看板模版" valid:"MaxSize(500)"`
}

type DetailAndDetailConsole struct {
	ID string `json:"id" alias:"ID" valid:"Required; MaxSize(36)"`
}

type ListConsole struct {
	Name        string `json:"name" alias:"name"`
	CurrentPage int    `json:"current_page"`
	PerPage     int    `json:"per_page"`
}
