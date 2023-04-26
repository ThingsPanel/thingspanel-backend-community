package controllers

import (
	response "ThingsPanel-Go/utils"

	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
)

type TpDataTransponController struct {
	beego.Controller
}

func (TpDataTransponController *TpDataTransponController) List() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}

func (TpDataTransponController *TpDataTransponController) Add() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}

func (TpDataTransponController *TpDataTransponController) Detail() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}

func (TpDataTransponController *TpDataTransponController) Edit() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}

func (TpDataTransponController *TpDataTransponController) Delete() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}

func (TpDataTransponController *TpDataTransponController) Switch() {
	response.Success(200, (*context2.Context)(TpDataTransponController.Ctx))
}
