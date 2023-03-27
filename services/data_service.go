package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"fmt"
	"github.com/beego/beego/v2/core/logs"
	"gorm.io/gorm"
)

type SoupDataService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// 获取列表
func (*SoupDataService) GetList(PaginationValidate valid.SoupDataPaginationValidate) (bool, []models.AddSoupDataValue, int64) {
	var SoupData []models.AddSoupDataValue
	offset := (PaginationValidate.CurrentPage - 1) * PaginationValidate.PerPage
	db := psql.Mydb.Model(&models.AddSoupData{})
	if PaginationValidate.ShopName != "" {
		asset := &models.Asset{}
		if err := psql.Mydb.Model(&models.Asset{}).Where("name like ?", "%"+PaginationValidate.ShopName+"%").First(asset).Error; err != nil {
			return false, nil, 0
		}
		db.Where("shop_id = ?", asset.ID)
	}

	var count int64
	db.Count(&count)
	result := db.Model(new(models.AddSoupData)).Select("recipe.bottom_pot,add_soup_data.order_sn,add_soup_data.table_number,add_soup_data.order_time,add_soup_data.soup_start_time,add_soup_data.soup_end_time,add_soup_data.feeding_start_time,add_soup_data.feeding_end_time,add_soup_data.turning_pot_end_time,add_soup_data.turning_pot_end_time,asset.name").Joins("left join recipe on add_soup_data.bottom_id = recipe.bottom_pot_id").Joins("left join asset on add_soup_data.shop_id = asset.id").Limit(PaginationValidate.PerPage).Offset(offset).Find(&SoupData)
	if result.Error != nil {
		logs.Error(result.Error, gorm.ErrRecordNotFound)
		return false, SoupData, 0
	}
	return true, SoupData, count
}

// 分页查询数据
func (*SoupDataService) Paginate(shopName string, limit int, offset int) ([]models.AddSoupDataValue, int64) {
	tSKVs := []models.SoupDataKVResult{}
	tsk := []models.AddSoupDataValue{}
	var count int64
	result := psql.Mydb
	result2 := psql.Mydb
	if limit <= 0 {
		limit = 1000000
	}
	if offset <= 0 {
		offset = 0
	}
	filters := map[string]interface{}{}

	if shopName != "" { //店铺id
		Asset := models.Asset{}
		if err := psql.Mydb.Where("name like ?", "%"+shopName+"%").First(&Asset).Error; err != nil {
			return nil, 0
		}
		filters["asd.shop_id"] = Asset.ID
	}

	SQLWhere, params := utils.TsKvFilterToSql(filters)

	countsql := "SELECT Count(*) AS count FROM add_soup_data as asd LEFT JOIN asset as a ON asd.shop_id=a.id   " + SQLWhere
	if err := result2.Raw(countsql, params...).Count(&count).Error; err != nil {
		logs.Info(err.Error())
		return tsk, 0
	}
	fmt.Println(countsql)
	//select business.name bname,ts_kv.*,concat_ws('-',asset.name,device.name) AS name,device.token
	//FROM ts_kv LEFT join device on device.id=ts_kv.entity_id
	//LEFT JOIN asset  ON asset.id=device.asset_id
	//LEFT JOIN business ON business.id=asset.business_id
	//WHERE 1=1  and ts_kv.ts >= 1654790400000000 and ts_kv.ts < 1655481599000000 ORDER BY ts_kv.ts DESC limit 10 offset 0
	SQL := `select add_soup_data.order_sn,asset.name,add_soup_data.table_number,add_soup_data.order_time,recipe.bottom_pot,
add_soup_data.soup_start_time,add_soup_data.soup_end_time,add_soup_data.feeding_start_time,add_soup_data.feeding_end_time,add_soup_data.turning_pot_end_time ,asset.name FROM add_soup_data  LEFT JOIN asset  ON add_soup_data.shop_id=asset.id LEFT JOIN recipe on add_soup_data.bottom_id = recipe.bottom_pot_id` + SQLWhere
	if limit > 0 && offset >= 0 {
		SQL = fmt.Sprintf("%s limit ? offset ? ", SQL)
		params = append(params, limit, offset)
	}
	if err := result.Raw(SQL, params...).Scan(&tSKVs).Error; err != nil {
		logs.Error(err.Error())
		return tsk, 0
	}
	for _, v := range tSKVs {
		ts := models.AddSoupDataValue{
			ShopName:         v.ShopName,
			OrderSn:          v.OrderSn,
			BottomPot:        v.BottomPot,
			TableNumber:      v.TableNumber,
			OrderTime:        v.OrderTime,
			SoupStartTime:    v.SoupStartTime,
			SoupEndTime:      v.SoupEndTime,
			FeedingStartTime: v.FeedingStartTime,
			FeedingEndTime:   v.FeedingEndTime,
			TurningPotEnd:    v.TurningPotEnd,

		}
		tsk = append(tsk, ts)
	}
	return tsk, count
}
