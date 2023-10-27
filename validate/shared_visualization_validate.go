package valid

type GetShareLinkValidate struct {
	Id    string `json:"id"  valid:"MaxSize(36)"`
}


type GetShareInfoValidate struct {
	Id    string `json:"id"  valid:"MaxSize(36)"`
}

