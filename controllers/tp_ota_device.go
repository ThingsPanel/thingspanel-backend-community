package controllers

import (
	gvalid "ThingsPanel-Go/initialize/validate"
	"ThingsPanel-Go/models"
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	valid "ThingsPanel-Go/validate"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/beego/beego/v2/core/validation"
	beego "github.com/beego/beego/v2/server/web"
	context2 "github.com/beego/beego/v2/server/web/context"
	"github.com/howeyc/crc16"
)

type TpOtaDeviceController struct {
	beego.Controller
}

// 列表
// 增加状态分类
func (c *TpOtaDeviceController) List() {
	reqData := valid.TpOtaDevicePaginationValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	isSuccess, d, t := TpOtaDeviceService.GetTpOtaDeviceList(reqData)
	if !isSuccess {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	datamap := make(map[string]interface{})
	datamap["list"] = d
	success, count := TpOtaDeviceService.GetTpOtaDeviceStatusCount(reqData)
	if !success {
		utils.SuccessWithMessage(1000, "查询失败", (*context2.Context)(c.Ctx))
		return
	}
	datamap["statuscount"] = count
	dd := valid.RspTpOtaDevicePaginationValidate{
		CurrentPage: reqData.CurrentPage,
		Data:        datamap,
		Total:       t,
		PerPage:     reqData.PerPage,
	}
	utils.SuccessWithDetailed(200, "success", dd, map[string]string{}, (*context2.Context)(c.Ctx))

}

// 新增
func (c *TpOtaDeviceController) Add() {
	reqData := valid.AddTpOtaDeviceValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	id := utils.GetUuid()
	TpOtaDevice := models.TpOtaDevice{
		Id:               id,
		DeviceId:         reqData.DeviceId,
		CurrentVersion:   reqData.CurrentVersion,
		TargetVersion:    reqData.TargetVersion,
		UpgradeProgress:  reqData.UpgradeProgress,
		StatusUpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		UpgradeStatus:    reqData.UpgradeStatus,
		StatusDetail:     reqData.StatusDetail,
	}
	d, rsp_err := TpOtaDeviceService.AddTpOtaDevice(TpOtaDevice)
	if rsp_err == nil {
		utils.SuccessWithDetailed(200, "success", d, map[string]string{}, (*context2.Context)(c.Ctx))
	} else {
		var err string
		isTrue := strings.Contains(rsp_err.Error(), "23505")
		if isTrue {
			err = "批次编号不能重复！"
		} else {
			err = rsp_err.Error()
		}
		utils.SuccessWithMessage(400, err, (*context2.Context)(c.Ctx))
	}
}

//修改状态
func (c *TpOtaDeviceController) ModfiyUpdate() {
	reqData := valid.TpOtaDeviceIdValidate{}
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &reqData)
	if err != nil {
		fmt.Println("参数解析失败", err.Error())
	}
	v := validation.Validation{}
	status, _ := v.Valid(reqData)
	if !status {
		for _, err := range v.Errors {
			// 获取字段别称
			alias := gvalid.GetAlias(reqData, err.Field)
			message := strings.Replace(err.Message, err.Field, alias, 1)
			utils.SuccessWithMessage(1000, message, (*context2.Context)(c.Ctx))
			break
		}
		return
	}
	if reqData.Id == "" && reqData.OtaTaskId == "" {
		utils.SuccessWithMessage(1000, "id与任务id不能全部为空", (*context2.Context)(c.Ctx))
		return
	}
	var TpOtaDeviceService services.TpOtaDeviceService
	rsp_err := TpOtaDeviceService.ModfiyUpdateDevice(reqData)
	if rsp_err == nil {
		utils.SuccessWithMessage(200, "success", (*context2.Context)(c.Ctx))
	} else {
		utils.SuccessWithMessage(400, rsp_err.Error(), (*context2.Context)(c.Ctx))
	}
}

//升级包下载
func (c *TpOtaDeviceController) Download() {
	filepath := c.Ctx.Input.Param(":splat")
	fmt.Println("filepath:", filepath)
	if filepath == "" {
		utils.SuccessWithMessage(1000, "参数错误", (*context2.Context)(c.Ctx))
		return
	}
	//判断文件是否存在
	if !utils.FileExist(filepath) {
		utils.SuccessWithMessage(1000, "文件不存在", (*context2.Context)(c.Ctx))
		return
	}

	//检查是否存在Range头部信息
	rangeHeader := c.Ctx.Input.Header("Range")
	//检查是否存在crc16Method头部信息
	crc16Method := c.Ctx.Input.Header("Crc16-Method")

	//如果不存在则直接下载
	if rangeHeader == "" {
		c.Ctx.Output.Download(filepath)
	}
	//发送文件部分内容
	c.serveRangeFile(filepath, rangeHeader, crc16Method)

}

//断点续传下载
func (c *TpOtaDeviceController) serveRangeFile(filePath string, rangeHeader, crc16Method string) {
	//解析Range头部信息
	rangeStr := strings.Replace(rangeHeader, "bytes=", "", 1)
	rangeParts := strings.Split(rangeStr, "-")
	if len(rangeParts) != 2 {
		c.Abort("416")
	}

	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		c.Abort("416")
	}

	file, err := os.Open(filePath)
	if err != nil {
		c.Abort("500")
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		c.Abort("500")
	}

	fileSize := fileInfo.Size()

	if rangeParts[1] == "" {
		rangeParts[1] = fmt.Sprintf("%d", fileSize-1)
	}
	end, err := strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		c.Abort("416")
	}

	if start >= fileSize || end >= fileSize {
		c.Abort("400")
	}

	contentLength := end - start + 1

	c.Ctx.Output.Header("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Ctx.Output.Header("Accept-Ranges", "bytes")
	c.Ctx.Output.Header("Content-Length", fmt.Sprintf("%d", contentLength))
	c.Ctx.Output.Header("Content-Type", filePath[len(filePath)-3:])
	c.Ctx.Output.SetStatus(http.StatusPartialContent)

	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		c.Abort("500")
	}
	// 创建一个缓冲区
	buffer := make([]byte, contentLength)

	// 从文件中读取数据到缓冲区
	_, err = file.Read(buffer)
	if err != nil {
		c.Abort("500")
	}
	switch crc16Method {
	case "CCITT":
		// 计算CRC16校验码
		crcValue := crc16.ChecksumCCITT(buffer)
		// 将校验码添加到HTTP响应的头部中
		c.Ctx.Output.Header("X-CRC16", fmt.Sprintf("%04x", crcValue))
		// 将缓冲区数据写入响应
		c.Ctx.ResponseWriter.Write(buffer)

	case "MODBUS":
		// 计算CRC16校验码
		crcValue := crc16.ChecksumMBus(buffer)
		// 将校验码添加到HTTP响应的头部中
		c.Ctx.Output.Header("X-CRC16", fmt.Sprintf("%04x", crcValue))

		// 将缓冲区数据写入响应
		c.Ctx.ResponseWriter.Write(buffer)
	default:
		// 计算CRC16-IBM校验码
		crcValue := crc16.ChecksumIBM(buffer)

		// 将校验码添加到HTTP响应的头部中
		c.Ctx.Output.Header("X-CRC16", fmt.Sprintf("%04x", crcValue))

		// 将缓冲区数据写入响应
		c.Ctx.ResponseWriter.Write(buffer)
	}
}
