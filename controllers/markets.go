package controllers

import (
	"ThingsPanel-Go/services"
	response "ThingsPanel-Go/utils"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type MarketsController struct {
	beego.Controller
}

type Marketextension struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Version     string            `json:"version"`
	Author      string            `json:"author"`
	Email       string            `json:"email"`
	Widgets     []Widgetextension `json:"widgets"`
}

type Widgetextension struct {
	Thumbnail string `json:"thumbnail"`
	Template  string `json:"template"`
}

// 列表
func (this *MarketsController) List() {
	var ms []Marketextension
	var AssetService services.AssetService
	el := AssetService.Extension()
	if len(el) > 0 {
		for _, ev := range el {
			wl := AssetService.Widget(ev.Key)
			var wi []Widgetextension
			if len(wl) > 0 {
				for _, wv := range wl {
					i := Widgetextension{
						Thumbnail: wv.Thumbnail,
						Template:  wv.Template,
					}
					wi = append(wi, i)
				}
			}
			if len(wi) == 0 {
				wi = []Widgetextension{}
			}
			mi := Marketextension{
				Type:        ev.Type,
				Name:        ev.Name,
				Description: ev.Description,
				Version:     ev.Version,
				Author:      ev.Author,
				Email:       ev.Email,
				Widgets:     wi,
			}
			ms = append(ms, mi)
		}
	}
	if len(ms) == 0 {
		ms = []Marketextension{}
	}
	response.SuccessWithDetailed(200, "success", ms, map[string]string{}, (*context2.Context)(this.Ctx))
	return
}
