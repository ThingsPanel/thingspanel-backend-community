package services

import (
	"encoding/json"
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
		CreateAt         int64    `gorm:"column:create_at"`
		UpdateAt         time.Time `gorm:"column:update_at;default:CURRENT_TIMESTAMP"`
		DeleteAt         string    `gorm:"column:delete_at"`
		IsDel            bool      `gorm:"column:is_del;default:false"`
	}
	obj := new(Recipe)
	by,_ := json.Marshal(obj)
	t.Log(string(by))
}
