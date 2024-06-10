package model

type GetCommandListRes struct {
	Name        string `json:"data_name"`
	Identifier  string `json:"data_identifier"`
	Params      string `json:"params"`
	Description string `json:"description"`
}
