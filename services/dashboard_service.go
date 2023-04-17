package services

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"errors"
	"fmt"
	"github.com/beego/beego/v2/core/logs"
	"strings"

	"gorm.io/gorm"
)

type DashBoardService struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

// Paginate 分页获取dashBoard数据
func (*DashBoardService) Paginate(title string, offset int, pageSize int) ([]models.DashBoard, int64) {
	var dashBoards []models.DashBoard
	var count int64
	if title != "" {
		result := psql.Mydb.Model(&models.DashBoard{}).Where("title LIKE ?", "%"+title+"%").Limit(pageSize).Offset(offset).Order("title asc").Find(&dashBoards)
		psql.Mydb.Model(&models.DashBoard{}).Where("title LIKE ?", "%"+title+"%").Count(&count)
		if len(dashBoards) == 0 {
			dashBoards = []models.DashBoard{}
		}
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return dashBoards, 0
		}
		return dashBoards, count
	} else {
		result := psql.Mydb.Model(&models.DashBoard{}).Limit(pageSize).Offset(offset).Order("title asc").Find(&dashBoards)
		psql.Mydb.Model(&models.DashBoard{}).Count(&count)
		if len(dashBoards) == 0 {
			dashBoards = []models.DashBoard{}
		}
		if result.Error != nil {
			logs.Error(result.Error.Error())
			return dashBoards, 0
		}
		return dashBoards, count
	}
}

// 根据id获取一条dashBoard数据
func (*DashBoardService) GetDashBoardById(id string) (*models.DashBoard, int64) {
	var dashBoard models.DashBoard
	result := psql.Mydb.Where("id = ?", id).First(&dashBoard)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return &dashBoard, 0
	}
	return &dashBoard, result.RowsAffected
}

// Add新增一条dashBoard数据
func (*DashBoardService) Add(businessId string, title string) (bool, string) {
	var uuid = uuid.GetUuid()
	configuration := "{\"start_time\":\"2020-10-01T14:23\",\"end_time\":\"2020-10-08T15:23\",\"theme\":1,\"interval_time\":0,\"bg_theme\":0}"
	dashBoard := models.DashBoard{ID: uuid, BusinessID: businessId, Title: title, Configuration: configuration}
	result := psql.Mydb.Create(&dashBoard)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return false, ""
	}
	return true, uuid
}

// 根据ID编辑一条dashboard数据
func (*DashBoardService) Edit(id string, businessId string, title string) bool {
	result := psql.Mydb.Model(&models.DashBoard{}).Where("id = ?", id).Updates(map[string]interface{}{"business_id": businessId, "title": title})
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 根据ID删除一条dashboard数据
func (*DashBoardService) Delete(id string) bool {
	result := psql.Mydb.Where("id = ?", id).Delete(&models.DashBoard{})
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return false
	}
	return true
}

// 根据configuration创建
func (*DashBoardService) ConfigurationAdd(configuration string) (*models.DashBoard, bool) {
	var uuid = uuid.GetUuid()
	dashBoard := models.DashBoard{ID: uuid, Configuration: configuration}
	result := psql.Mydb.Create(&dashBoard)
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return &dashBoard, false
	}
	return &dashBoard, true
}

// 根据configuration更新
func (*DashBoardService) ConfigurationEdit(id string, configuration string) (*models.DashBoard, bool) {
	var dashBoard models.DashBoard
	edit := psql.Mydb.Model(&models.DashBoard{}).Where("id = ?", id).Updates(map[string]interface{}{
		"configuration": configuration,
	})
	if edit.Error != nil {
		//errors.Is(edit.Error, gorm.ErrRecordNotFound)
		logs.Error(edit.Error.Error())
		return &dashBoard, false
	}
	add := psql.Mydb.Model(&models.DashBoard{}).Where("id = ?", id).First(&dashBoard)
	if add.Error != nil {
		//errors.Is(add.Error, gorm.ErrRecordNotFound)
		logs.Error(add.Error.Error())
		return &dashBoard, false
	}
	return &dashBoard, true
}

func (*DashBoardService) All() ([]models.DashBoard, int64) {
	var dashBoards []models.DashBoard
	result := psql.Mydb.Find(&dashBoards)
	if len(dashBoards) == 0 {
		dashBoards = []models.DashBoard{}
	}
	if result.Error != nil {
		//errors.Is(result.Error, gorm.ErrRecordNotFound)
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return dashBoards, result.RowsAffected
}

// 根据条件获取一条dashBoard数据
func (*DashBoardService) GetDashBoardByCondition(business_id string, id string) (*models.DashBoard, int64) {
	var dashBoard models.DashBoard
	result := psql.Mydb.Where("business_id = ? AND id = ?", business_id, id).First(&dashBoard)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return &dashBoard, 0
		}
		logs.Error(result.Error.Error())
		return nil, 0
	}
	return &dashBoard, result.RowsAffected
}

func (*DashBoardService) GetPlugList() []models.PlugSt {
	var pluginList []models.PlugSt
	_, dirs, _ := utils.GetFilesAndDirs("./extensions")
	for _, dir := range dirs {
		dir = strings.Replace(dir, "\\", "/", -1)
		plugFiles, _ := utils.GetFiles(dir + "/view")
		for _, file := range plugFiles {
			fmt.Println(file)
			if file[len(file)-3:] == ".js" {
				fmt.Println(file)
				var plugSt models.PlugSt
				//大驼峰
				plugSt.ChartType = utils.Ucfirst(file[:len(file)-3])
				//中划线
				plugSt.Component = utils.Camel2Case(file[:len(file)-3])
				plugSt.Url = (dir + "/view/" + file)[1:]
				pluginList = append(pluginList, plugSt)
			}
		}
	}
	return pluginList
}
