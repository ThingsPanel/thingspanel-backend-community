package middleware

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	jwt "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	adapter "github.com/beego/beego/v2/adapter"
	"github.com/beego/beego/v2/adapter/context"
)

type OperationLogDetailed struct {
	Input       string `json:"input"`
	Data        string `json:"data"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	Ip          string `json:"ip"`
	Usernum     string `json:"usernum"`
	Name        string `json:"name"`
	RequestTime string `json:"request_time"`
}

// LogMiddle 中间件
var filterLog = func(ctx *context.Context) {
	if ctx.Input.URL() != "/api/home/chart" && ctx.Input.URL() != "/api/home/list" {

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
		//获取租户id
		tenantId, ok := ctx.Input.GetData("tenant_id").(string)
		if !ok {
			tenantId = ""
		}
		//获取url类型
		urlKey := strings.Replace(ctx.Input.URL(), "/", "_", -1)
		urlType := urlMap(urlKey)
		//传递name
		detailedStruct := getRequestDetailed(ctx)
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
			TenantId:  tenantId,
		}
		if err := psql.Mydb.Create(&logData).Error; err != nil {
			fmt.Println("log insert fail")
		}
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
		//新增
		//"_api_auth_register":                "1",
		"_api_home_system_time":            "2",
		"_api_user_role_add":               "1",
		"_api_user_role_edit":              "1",
		"_api_user_role_delete":            "1",
		"_api_user_function_add":           "1",
		"_api_user_function_edit":          "1",
		"_api_user_function_delete":        "1",
		"_api_user_function_auth":          "1",
		"_api_menu_role_add":               "11",
		"_api_menu_role_edit":              "11",
		"_api_menu_user":                   "11",
		"_api_casbin_role_function_add":    "12",
		"_api_casbin_role_function_update": "12",
		"_api_casbin_role_function_delete": "12",
		"_api_casbin_user_role_add":        "12",
		"_api_casbin_user_role_update":     "12",
		"_api_casbin_user_role_delete":     "12",
		"_api_field_add_only":              "13",
		"_api_field_update_only":           "13",
		"_api_asset_add_only":              "14",
		"_api_asset_update_only":           "14",
		"_api_asset_simple":                "14",
		"_api_device_add_only":             "15",
		"_api_device_update_only":          "16",
		"_api_device_operating_device":     "16",
		"_api_device_reset":                "16",
		"_api_device_data":                 "16",
		"_api_device_cascade":              "16",
		"_api_device_map":                  "16",
		//"_api_device_status":                "16",
		"_api_dashboard_business_component": "17",
		"_api_warning_view":                 "18",
		// "_api_automation_details":                      "20",
		// "_api_automation_manual_trigger":               "20",
		"_api_kv_current":          "10",
		"_api_kv_current_business": "10",
		"_api_kv_current_asset":    "10",
		"_api_kv_current_asset_a":  "10",
		"_api_kv_current_symbol":   "10",
		"_api_kv_device_history":   "10",
		"_api_kv_history":          "10",
		"_api_system_logo_update":  "19",
		//"_api_widget_extend_update":                    "20",
		"_api_file_up":               "20",
		"_api_open_data":             "21",
		"_api_data_transpond_add":    "21",
		"_api_data_transpond_edit":   "21",
		"_api_data_transpond_delete": "21",
		"_api_chart_add":             "22",
		"_api_chart_edit":            "22",
		"_api_chart_delete":          "22",
		"_api_device_model_add":      "23",
		"_api_device_model_edit":     "23",
		"_api_device_model_delete":   "23",
		"_api_dict_add":              "24",
		"_api_dict_edit":             "24",
		"_api_dict_delete":           "24",
		"_api_object_model_add":      "25",
		"_api_object_model_edit":     "25",
		"_api_object_model_delete":   "25",
		"_api_tp_dashboard_add":      "7",
		"_api_tp_dashboard_edit":     "7",
		"_api_tp_dashboard_delete":   "7",
		//"_api_plugin_device_config":                    "20",
		//"_api_form_config":                             "20",
		//"_api_plugin_device_sub-device-detail": "27",
		"_api_tp_script_add":               "28",
		"_api_tp_script_edit":              "28",
		"_api_tp_script_delete":            "28",
		"_api_tp_product_add":              "29",
		"_api_tp_product_edit":             "29",
		"_api_tp_product_delete":           "29",
		"_api_tp_batch_add":                "30",
		"_api_tp_batch_edit":               "30",
		"_api_tp_batch_delete":             "30",
		"_api_tp_batch_generate":           "30",
		"_api_tp_batch_export":             "30",
		"_api_tp_batch_import":             "30",
		"_api_plugin_register":             "31",
		"_api_tp_protocol_plugin_add":      "32",
		"_api_tp_protocol_plugin_edit":     "32",
		"_api_tp_protocol_plugin_delete":   "32",
		"_api_tp_generate_device_activate": "29",
		"_api_tp_generate_device_delete":   "29",
		"_api_tp_ota_add":                  "33",
		"_api_tp_ota_delete":               "33",
		"_api_tp_ota_task_add":             "33",
		"_api_tp_ota_device_add":           "33",
		"_api_tp_ota_device_modfiyupdate":  "33",
		//"_api_wvp_ptz":                                 "20",
		"_api_gb_record_query":       "34",
		"_api_playback_start":        "34",
		"_api_wvp_query_devices":     "34",
		"_api_wvp_play_start":        "34",
		"_api_wvp_play_stop":         "34",
		"_api_scenario_strategy_add": "35",
		//"_api_scenario_strategy_detail":                "20",
		"_api_scenario_strategy_edit":                  "36",
		"_api_scenario_strategy_delete":                "36",
		"_api_v1_automation_add":                       "9",
		"_api_v1_automation_detail":                    "9",
		"_api_v1_automation_delete":                    "9",
		"_api_v1_automation_edit":                      "9",
		"_api_v1_automation_enabled":                   "9",
		"_api_v1_warning_information_edit":             "9",
		"_api_v1_warning_information_batch_processing": "9",
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
	//获取起始时间
	startTime := ctx.Request.Header.Get("req_start_time")
	//string转int
	startTimeInt, _ := strconv.Atoi(startTime)
	reqTimeInt := int(time.Now().UnixNano()/1e6) - startTimeInt
	var detailedStruct OperationLogDetailed
	detailedStruct.Path = ctx.Input.URL()
	detailedStruct.Method = ctx.Request.Method
	detailedStruct.Ip = ctx.Input.IP()
	detailedStruct.RequestTime = strconv.Itoa(reqTimeInt)
	return &detailedStruct

}

//将开始时间放入cookie
var getStartTime = func(ctx *context.Context) {
	startTime := time.Now().UnixNano() / 1e6
	startTimeStr := strconv.FormatInt(startTime, 10)
	ctx.Request.Header.Set("req_start_time", startTimeStr)
}

func LogMiddle() {
	adapter.InsertFilter("/*", adapter.FinishRouter, filterLog, false)
	adapter.InsertFilter("/*", adapter.BeforeRouter, getStartTime, false)
}
