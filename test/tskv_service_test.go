package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

//var TSKVService services.TSKVService

//func TestGetTelemetry(t *testing.T) {
//	device_ids := []string{"7d61d336-c800-7e74-b54b-dace04507166"}
//	warningLogs := TSKVService.GetTelemetry(device_ids, 1644288241274, 1644888241274, "")
//	fmt.Println(warningLogs)
//
//}

func TestName(t *testing.T) {
	type Recipe struct {
		Id               string    `gorm:"primaryKey;column:id;NOT NULL"`
		BottomPotId      string    `gorm:"column:bottom_pot_id"`
		BottomPot        string    `gorm:"column:bottom_pot"`
		PotTypeId        string    `gorm:"column:pot_type_id"`
		PotTypeName      string    `gorm:"column:pot_type_name"`
		Materials        string    `gorm:"column:materials"`
		MaterialsId      string    `gorm:"column:materials_id"`
		TasteId          string    `gorm:"column:taste_id"`
		Taste            string    `gorm:"column:taste"`
		BottomProperties string    `gorm:"column:bottom_properties"`
		SoupStandard     string    `gorm:"column:soup_standard"`
		CreateAt         int64     `gorm:"column:create_at"`
		UpdateAt         time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
		DeleteAt         string    `gorm:"column:delete_at"`
		IsDel            bool      `gorm:"column:is_del;default:false"`
	}
	obj := new(Recipe)
	by, _ := json.Marshal(obj)
	t.Log(string(by))
}

func TestSearch(t *testing.T) {
	now := time.Now()
	arr := []float64{506143.19,
		255066.07,
		199538.88,
		324143.50,
		434528.62,
		244183.76,
		250523.16,
		401432.80,
		594336.67,
		279726.94,
		55873.76,
		57257.88,
		78415.25,
		55832.27,
		70532.19,
		87102.77,
		94298.74,
		92222.00,
		93935.00,
		119789.48}
	search := 753553.00
	result := checkFloatValue(search,arr)
	fmt.Println(result)
	fmt.Println(time.Since(now))
	now1 := time.Now()
	result1 := checkFloatValue(search,arr)
	fmt.Println(result1)
	fmt.Println(time.Since(now1))
}

func checkFloatValue(x float64, arr []float64) bool {
	for _, val := range arr {
		if x >= val*2 && x <= val*10 {
			continue
		} else {
			return false
		}
	}
	return true
}

func checkFloatValue1(x float64, arr []float64) bool {
	// 对数组进行排序
	sort.Float64s(arr)

	for _, val := range arr {
		low := 2 * val
		high := 10 * val
		if x < low {
			// 如果 x 小于 low，则后面的元素也不会满足条件，可以直接返回 false
			return false
		}
		if x <= high {
			// 如果 x 在 low 和 high 之间，则说明找到了一个符合条件的元素，可以继续检查下一个元素
			continue
		}
		// 如果 x 大于 high，则需要在 high 后面的元素中查找符合条件的元素
		index := sort.SearchFloat64s(arr, high)
		if index >= len(arr) {
			// 如果 high 大于数组中的最大值，则说明没有符合条件的元素了，可以直接返回 false
			return false
		}
	}
	return true
}
