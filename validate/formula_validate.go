package valid

import (
	"ThingsPanel-Go/models"
	"time"
)

type AddRecipeValidator struct {
	Id               string     `json:"Id"`
	BottomPotId      string     `json:"BottomPotId" alias:"锅底ID" valid:"Required"`
	BottomPot        string     `json:"BottomPot" alias:"锅底" valid:"Required"`
	PotTypeId        string     `json:"PotTypeId" alias:"锅型ID" valid:"Required"`
	PotTypeName      string     `json:"PotTypeName" alias:"锅型名称" valid:"Required"`
	MaterialsId      string     `json:"MaterialsId" alias:"物料ID"`
	TasteId          string     `json:"TasteId" alias:"口味ID"`
	Tastes           []string   `json:"Taste" alias:"口味"`
	TastesArr        []Taste    `json:"TasteArr" alias:"口味" `
	BottomProperties string     `json:"BottomProperties" alias:"锅底属性" valid:"Required"`
	SoupStandard     int64      `json:"SoupStandard" alias:"加汤水位标准" `
	MaterialsArr     []Material `json:"MaterialArr" alias:"物料"`
	Materials        []string   `json:"Materials" alias:"物料"`
	CurrentWaterLine int64      `json:"CurrentWaterLine" alias:"当前加汤水位线"`
}

type EditRecipeValidator struct {
	Id               string     `json:"Id"`
	BottomPotId      string     `json:"BottomPotId" alias:"锅底ID" valid:"Required"`
	BottomPot        string     `json:"BottomPot" alias:"锅底" valid:"Required"`
	PotTypeId        string     `json:"PotTypeId" alias:"锅型ID" valid:"Required"`
	PotTypeName      string     `json:"PotTypeName" alias:"锅型名称" valid:"Required"`
	MaterialsId      string     `json:"MaterialsId" alias:"物料ID"`
	TasteId          string     `json:"TasteId" alias:"口味ID"`
	Tastes           []string   `json:"Taste" alias:"口味"`
	TastesArr        []Taste    `json:"TasteArr" alias:"口味" `
	BottomProperties string     `json:"BottomProperties" alias:"锅底属性" valid:"Required"`
	SoupStandard     int64      `json:"SoupStandard" alias:"加汤水位标准" `
	MaterialsArr     []Material `json:"MaterialArr" alias:"物料"`
	Materials        []string   `json:"Materials" alias:"物料"`
	CurrentWaterLine int64      `json:"CurrentWaterLine" alias:"当前加汤水位线"`
	CreateAt         int        `json:"CreateAt"`
	DeleteAt         time.Time  `json:"DeleteAt"`
	IsDel            bool       `json:"IsDel"`
}

type Taste struct {
	Taste         string `json:"Taste"`
	TasteId       string `json:"TasteId"`
	MaterialsName string `json:"materials_name"`
	Dosage        int    `json:"Dosage"`
	Unit          string `json:"Unit"`
	WaterLine     int    `json:"WaterLine"`
	Station       string `json:"Station"`
	Action        string `json:"Action"`
}

type Material struct {
	Id        string `json:"id"`
	Name      string `json:"Name"`
	Dosage    int    `json:"Dosage"`
	Unit      string `json:"Unit"`
	WaterLine int    `json:"WaterLine"`
	Station   string `json:"Station"`
	Action    string `json:"Action"`
}

type RecipePaginationValidate struct {
	CurrentPage int    `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int    `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Id          string `json:"id" alias:"配方ID"`
}

type RspRecipePaginationValidate struct {
	CurrentPage int                  `json:"current_page"  alias:"当前页" valid:"Required;Min(1)"`
	PerPage     int                  `json:"per_page"  alias:"每页页数" valid:"Required;Max(10000)"`
	Data        []models.RecipeValue `json:"data" alias:"返回数据"`
	Total       int64                `json:"total" alias:"总数" valid:"Max(10000)"`
}

type DelRecipeValidator struct {
	Id string `json:"id" valid:"Required"`
}

type SearchMaterialNameValidator struct {
	Keyword string `json:"keyword"`
}

type DelMaterialValidator struct {
	Id string `json:"id" valid:"Required"`
}

type DelTasteValidator struct {
	Id string `json:"id" valid:"Required"`
}

type SendToMQTTValidator struct {
	AccessToken string `json:"access_token" valid:"Required"`
	AssetId     string `json:"asset_id" valid:"Required"`
}

type SearchTasteValidator struct {
	Keyword string `json:"keyword"`
}

type GetMaterialValidator struct {
	Name string `json:"name" valid:"Required"`
}

type CreateMaterialValidator struct {
	Name      string `json:"Name" valid:"Required"`
	Dosage    int    `json:"Dosage" valid:"Required"`
	Unit      string `json:"Unit" valid:"Required"`
	WaterLine int    `json:"WaterLine" valid:"Required"`
	Station   string `json:"Station" valid:"Required"`
}

type CreateTasteValidator struct {
	Taste      string `json:"Taste" valid:"Required"`
	TasteId    string `json:"TasteId" valid:"Required"`
	MaterialId string `json:"MaterialId" valid:"Required"`
	Action string `json:"Action"`
}
