package models

type AddSoupData struct {
	Id               string `gorm:"column:id;NOT NULL"`
	ShopName         string `gorm:"column:shop_name;NOT NULL"`
	OrderSn          string `gorm:"column:order_sn;NOT NULL"`
	BottomPot        string `gorm:"column:bottom_pot;NOT NULL"`
	TableNumber      string `gorm:"column:table_number"`
	OrderTime        string `gorm:"column:order_time"`
	SoupStartTime    string `gorm:"column:soup_start_time"`
	SoupEndTime      string `gorm:"column:soup_end_time"`
	FeedingStartTime string `gorm:"column:feeding_start_time"`
	FeedingEndTime   string `gorm:"column:feeding_end_time"`
	TurningPotEnd    string `gorm:"column:turning_pot_end"`
	ShopId           string `gorm:"column:shop_id"`
}

func (a *AddSoupData) TableName() string {
	return "add_soup_data"
}
