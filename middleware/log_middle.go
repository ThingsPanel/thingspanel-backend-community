package middleware

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	jwt "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
)

type OperationLogDetailed struct {
	Input   string `json:"input"`
	Data    string `json:"data"`
	Path    string `json:"path"`
	Method  string `json:"method"`
	Ip      string `json:"ip"`
	Usernum string `json:"usernum"`
	Name    string `json:"name"`
}

// LogMiddle 中间件
var filterLog = func(ctx *context.Context) {
	detailedStruct := getRequestDetailed(ctx)
	var name string
	//非登录接口从token中获取用户name
	if len(ctx.Request.Header["Authorization"]) != 0 {
		authorization := ctx.Request.Header["Authorization"][0]
		userToken := authorization[7:]
		userClaims, err := jwt.ParseCliamsToken(userToken)
		if err == nil {
			name = userClaims.Name
		}
	}
	//获取url类型
	urlKey := strings.Replace(ctx.Input.URL(), "/", "_", -1)
	urlType := urlMap(urlKey)
	//传递name
	detailedStruct.Name = name
	detailedJsonByte, err := json.Marshal(detailedStruct) //转换成JSON返回的是byte[]
	if err != nil {
		fmt.Println(err.Error())
	}
	//组合描述
	describe := name + "-send:" + detailedStruct.Path
	var uuid = uuid.GetUuid()
	logData := models.OperationLog{
		ID:        uuid,
		Type:      urlType, //需要确认需求
		Describe:  describe,
		DataID:    "",
		CreatedAt: time.Now().Unix(),
		Detailed:  string(detailedJsonByte),
	}
	if err := psql.Mydb.Create(&logData).Error; err != nil {
		fmt.Println("log insert fail")
	}

}

//url映射
func urlMap(k string) string {
	myColors := map[string]string{
		"_api_auth_login":                "1",
		"_api_auth_logout":               "1",
		"_api_auth_refresh":              "1",
		"_api_auth_me":                   "1",
		"_api_home_list":                 "2",
		"_api_home_chart":                "2",
		"_api_index_show":                "2",
		"_api_index_device":              "2",
		"_api_user_index":                "3",
		"_api_user_add":                  "3",
		"_api_user_edit":                 "3",
		"_api_user_delete":               "3",
		"_api_user_password":             "3",
		"_api_user_update":               "3",
		"_api_user_permission":           "3",
		"_api_customer_index":            "4",
		"_api_customer_add":              "4",
		"_api_customer_edit":             "4",
		"_api_customer_delete":           "4",
		"_api_asset_index":               "5",
		"_api_asset_add":                 "5",
		"_api_asset_edit":                "5",
		"_api_asset_delete":              "5",
		"_api_asset_widget":              "5",
		"_api_asset_list":                "5",
		"_apiasset_work_index":           "5",
		"_apiasset_work_add":             "5",
		"_apiasset_work_edit":            "5",
		"_apiasset_work_delete":          "5",
		"_apibusiness_index":             "5",
		"_apibusiness_add":               "5",
		"_apibusiness_edit":              "5",
		"_apibusiness_delete":            "5",
		"_apibusiness_tree":              "5",
		"_api_device_token":              "6",
		"_api_device_index":              "6",
		"_api_device_edit":               "6",
		"_api_device_add":                "6",
		"_api_device_delete":             "6",
		"_api_device_configure":          "6",
		"_api_dashboard_index":           "7",
		"_api_dashboard_add":             "7",
		"_api_dashboard_edit":            "7",
		"_api_dashboard_delete":          "7",
		"_api_dashboard_paneladd":        "7",
		"_api_dashboard_paneldelete":     "7",
		"_api_dashboard_paneledit":       "7",
		"_api_dashboard_list":            "7",
		"_api_dashboard_business":        "7",
		"_api_dashboard_property":        "7",
		"_api_dashboard_device":          "7",
		"_api_dashboard_inserttime":      "7",
		"_api_dashboard_gettime":         "7",
		"_api_dashboard_dashboard":       "7",
		"_api_dashboard_realTime":        "7",
		"_api_dashboard_updateDashboard": "7",
		"_api_dashboard_component":       "7",
		"_api_markets_list":              "7",
		"_api_warning_index":             "8",
		"_api_warning_list":              "8",
		"_api_warning_field":             "8",
		"_api_warning_add":               "8",
		"_api_warning_edit":              "8",
		"_api_warning_delete":            "8",
		"_api_warning_show":              "8",
		"_api_warning_update":            "8",
		"_api_automation_index":          "9",
		"_api_automation_add":            "9",
		"_api_automation_edit":           "9",
		"_api_automation_delete":         "9",
		"_api_automation_get_by_id":      "9",
		"_api_automation_status":         "9",
		"_api_automation_symbol":         "9",
		"_api_automation_property":       "9",
		"_api_automation_show":           "9",
		"_api_automation_update":         "9",
		"_api_automation_instruct":       "9",
		"_api_operation_index":           "10",
		"_api_operation_list":            "10",
		"_api_structure_add":             "10",
		"_api_structure_list":            "10",
		"_api_structure_update":          "10",
		"_api_structure_delete":          "10",
		"_api_structure_field":           "10",
		"_api_navigation_add":            "10",
		"_api_navigation_list":           "10",
		"_api_kv_list":                   "10",
		"_api_kv_index":                  "10",
		"_api_kv_export":                 "10",
	}
	_, ok := myColors[k]
	if ok {
		return myColors[k]
	} else {
		return ""
	}
}

//获取请求详细数据
func getRequestDetailed(ctx *context.Context) *OperationLogDetailed {

	var detailedStruct OperationLogDetailed
	detailedStruct.Path = ctx.Input.URL()
	detailedStruct.Method = ctx.Request.Method
	detailedStruct.Ip = ctx.Input.IP()
	return &detailedStruct

}

func LogMiddle() {
	adapter.InsertFilter("/*", adapter.BeforeRouter, filterLog, false)
}
