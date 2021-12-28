package middleware

import (
	"ThingsPanel-Go/initialize/psql"
	"ThingsPanel-Go/models"
	jwt "ThingsPanel-Go/utils"
	uuid "ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
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
	detailedJsonByte, err := json.Marshal(detailedStruct) //转换成JSON返回的是byte[]
	if err != nil {
		fmt.Println(err.Error())
	}
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
	detailedStruct.Name = name
	describe := name + "-send:" + detailedStruct.Path
	var uuid = uuid.GetUuid()
	logData := models.OperationLog{
		ID:        uuid,
		Type:      "1", //需要确认需求
		Describe:  describe,
		DataID:    "",
		CreatedAt: time.Now().Unix(),
		Detailed:  string(detailedJsonByte),
	}
	if err := psql.Mydb.Create(&logData).Error; err != nil {
		fmt.Println("log insert fail")
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
