package valid

type OpenValidate struct {
	Token  string                 `json:"token"`
	Values map[string]interface{} `json:"values"`
}
