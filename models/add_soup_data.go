package models

type AddSoupData struct {
	Id               string `gorm:"column:id;NOT NULL" json:"id,omitempty"`
	ShopName         string `gorm:"column:name;NOT NULL"`
	OrderSn          string `gorm:"column:order_sn;NOT NULL"`
	BottomPot        string `gorm:"column:bottom_id;NOT NULL"`
	TableNumber      string `gorm:"column:table_number"`
	OrderTime        int64  `gorm:"column:order_time"`
	SoupStartTime    int64  `gorm:"column:soup_start_time"`
	SoupEndTime      int64  `gorm:"column:soup_end_time"`
	FeedingStartTime int64  `gorm:"column:feeding_start_time"`
	FeedingEndTime   int64  `gorm:"column:feeding_end_time"`
	TurningPotEnd    int64  `gorm:"column:turning_pot_end_time"`
	ShopId           string `gorm:"column:shop_id" json:"shop_id,omitempty"`
}

type AddSoupDataValue struct {
	Id               string `gorm:"column:id;NOT NULL" json:"id,omitempty"`
	ShopName         string `gorm:"column:name;NOT NULL"`
	OrderSn          string `gorm:"column:order_sn;NOT NULL"`
	BottomPot        string `gorm:"column:bottom_pot;NOT NULL"`
	TableNumber      string `gorm:"column:table_number"`
	OrderTime        int64  `gorm:"column:order_time"`
	SoupStartTime    int64  `gorm:"column:soup_start_time"`
	SoupEndTime      int64  `gorm:"column:soup_end_time"`
	FeedingStartTime int64  `gorm:"column:feeding_start_time"`
	FeedingEndTime   int64  `gorm:"column:feeding_end_time"`
	TurningPotEnd    int64  `gorm:"column:turning_pot_end_time"`
}

func (a *AddSoupData) TableName() string {
	return "add_soup_data"
}

type SoupDataKVResult struct {
	AddSoupDataValue
	Name     string `json:"name"`
	PluginId string `json:"plugin_id"`
}
