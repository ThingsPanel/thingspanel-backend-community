package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type NavigationController struct {
	beego.Controller
}

type NavigationItem struct {
	ID         string `json:"id"`
	BusinessID string `json:"business_id"`
	ChartID    string `json:"chart_id"`
}

type NavigationList struct {
	ID    string         `json:"id"`
	Type  int64          `json:"type"`
	Name  string         `json:"name"`
	Data  NavigationItem `json:"data"`
	Count int64          `json:"count"`
}

func (this *NavigationController) Add() {
	navigationAddValidate := valid.NavigationAdd{}
	err := json.Unmarshal(this.Ctx.Input.RequestBody, &navigationAddValidate)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(navigationAddValidate)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(navigationAddValidate, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			response.SuccessWithMessage(1000, message, (*context2.Context)(this.Ctx))
			break
		}
		return
	}
	var NavigationService services.NavigationService
	n, c := NavigationService.GetNavigationByCondition(navigationAddValidate.Type, navigationAddValidate.Name, navigationAddValidate.Data)
	if c > 0 {
		f := NavigationService.Increment(n.ID, n.Count, 1)
		if f {
			response.SuccessWithMessage(200, "更新成功", (*context2.Context)(this.Ctx))
			return
		} else {
			response.SuccessWithMessage(400, "更新失败", (*context2.Context)(this.Ctx))
			return
		}
	} else {
		f, _ := NavigationService.Add(navigationAddValidate.Name, navigationAddValidate.Type, navigationAddValidate.Data)
		if f {
			response.SuccessWithMessage(200, "新增成功", (*context2.Context)(this.Ctx))
			return
		} else {
			response.SuccessWithMessage(400, "新增失败", (*context2.Context)(this.Ctx))
			return
		}
	}
}

func (this *NavigationController) List() {
	var navigationList []NavigationList
	var NavigationService services.NavigationService
	var BusinessService services.BusinessService
	var WarningConfigService services.WarningConfigService
	var DashBoardService services.DashBoardService
	nl, nc := NavigationService.List()
	if nc > 0 {
		for _, nv := range nl {
			var ni NavigationItem
			err := json.Unmarshal([]byte(nv.Data), &ni) //第二个参数要地址传递
			if err != nil {
				fmt.Println("err = ", err)
				return
			}
			ni = NavigationItem{
				ID:         ni.ID,
				BusinessID: ni.BusinessID,
				ChartID:    ni.ChartID,
			}
			if len(navigationList) < 6 {
				if nv.Type == 1 || nv.Type == 2 {
					_, bc := BusinessService.GetBusinessById(ni.ID)
					if bc > 0 {
						// 复制
						nai := NavigationList{
							ID:    nv.ID,
							Type:  nv.Type,
							Name:  nv.Name,
							Data:  ni,
							Count: nv.Count,
						}
						navigationList = append(navigationList, nai)
					} else {
						NavigationService.Delete(nv.ID)
					}
				} else if nv.Type == 3 {
					_, wc := WarningConfigService.GetWarningConfigById(ni.ID)
					if wc > 0 {
						nai := NavigationList{
							ID:    nv.ID,
							Type:  nv.Type,
							Name:  nv.Name,
							Data:  ni,
							Count: nv.Count,
						}
						navigationList = append(navigationList, nai)
					} else {
						NavigationService.Delete(nv.ID)
					}
				} else if nv.Type == 4 {
					_, dc := DashBoardService.GetDashBoardByCondition(ni.BusinessID, ni.ChartID)
					if dc > 0 {
						nai := NavigationList{
							ID:    nv.ID,
							Type:  nv.Type,
							Name:  nv.Name,
							Data:  ni,
							Count: nv.Count,
						}
						navigationList = append(navigationList, nai)
					} else {
						NavigationService.Delete(nv.ID)
					}
				}
			}
		}
	}
	if len(navigationList) == 0 {
		navigationList = []NavigationList{}
	}
	response.SuccessWithDetailed(200, "success", navigationList, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
