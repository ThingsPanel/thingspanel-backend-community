package model

type GetTenantRes struct {
	UserTotal          int64                    `json:"user_total"`
	UserAddedYesterday int64                    `json:"user_added_yesterday"`
	UserAddedMonth     int64                    `json:"user_added_month"`
	UserListMonth      []*GetBoardUserListMonth `json:"user_list_month"`
}

type GetBoardUserListMonth struct {
	Month int `json:"mon" gorm:"column:mon"`
	Num   int `json:"num"`
}
