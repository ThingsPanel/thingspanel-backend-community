package valid

import (
	"ThingsPanel-Go/models"
)

// PotType 校验
type PotType struct {
	Id           string `alias:"锅型ID"`
	Name         string `alias:"锅型名称" valid:"Required; MaxSize(255)"`
	Image        string `alias:"图片" valid:"Required"`
	SoupStandard int    `alias:"加汤水位线标准" valid:"Required"`
	PotTypeId    string `alias:"锅型ID" valid:"Required"`
}

func (p *PotType) TableName() string {
	return "pot_type"
}

type RspPotTypePaginationValidate struct {
	CurrentPage int              `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int              `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.PotType `json:"data" alias:"返回数据"`
	Total       int64            `json:"total" alias:"总数" valid:"Max(10000)"`
}

type PotTypeIdValidate struct {
	Id string `json:"id"  gorm:"primaryKey" valid:"Required;MaxSize(36)"`
}
