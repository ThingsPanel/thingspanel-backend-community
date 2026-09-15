package api

import (
	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
)

type ProductApi struct{}

// @Router /api/v1/product [post]
func (*ProductApi) CreateProduct(c *gin.Context) {
	var req model.CreateProductReq
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Product.CreateProduct(&req, claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}

// @Router /api/v1/product [put]
func (*ProductApi) UpdateProduct(c *gin.Context) {
	var req model.UpdateProductReq
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	if err := service.GroupApp.Product.UpdateProduct(&req, claims); err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// @Router /api/v1/product/{id} [delete]
func (*ProductApi) DeleteProduct(c *gin.Context) {
	claims := c.MustGet("claims").(*utils.UserClaims)
	if err := service.GroupApp.Product.DeleteProduct(c.Param("id"), claims); err != nil {
		c.Error(err)
		return
	}
	c.Set("data", nil)
}

// @Router /api/v1/product [get]
func (*ProductApi) HandleProductListByPage(c *gin.Context) {
	var req model.GetProductListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	claims := c.MustGet("claims").(*utils.UserClaims)
	data, err := service.GroupApp.Product.GetProductListByPage(&req, claims)
	if err != nil {
		c.Error(err)
		return
	}
	c.Set("data", data)
}
