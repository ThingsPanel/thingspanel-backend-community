package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	model "project/internal/model"
	service "project/internal/service"
	"project/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/howeyc/crc16"
)

type OTAApi struct{}

// CreateOTAUpgradePackage
// @Router   /api/v1/ota/package [post]
func (*OTAApi) CreateOTAUpgradePackage(c *gin.Context) {
	var req model.CreateOTAUpgradePackageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	err := service.GroupApp.OTA.CreateOTAUpgradePackage(&req, userClaims.TenantID)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create successfully", nil)
}

// DeleteOTAUpgradePackage
// @Router   /api/v1/ota/package/{id} [delete]
func (*OTAApi) DeleteOTAUpgradePackage(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.OTA.DeleteOTAUpgradePackage(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete successfully", nil)
}

// UpdateOTAUpgradePackage
// @Router   /api/v1/ota/package/ [put]
func (*OTAApi) UpdateOTAUpgradePackage(c *gin.Context) {
	var req model.UpdateOTAUpgradePackageReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.OTA.UpdateOTAUpgradePackage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update successfully", nil)
}

// GetOTAUpgradePackageByPage
// @Router   /api/v1/ota/package [get]
func (*OTAApi) HandleOTAUpgradePackageByPage(c *gin.Context) {
	var req model.GetOTAUpgradePackageLisyByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	var userClaims = c.MustGet("claims").(*utils.UserClaims)
	list, err := service.GroupApp.OTA.GetOTAUpgradePackageListByPage(&req, userClaims)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}

	SuccessHandler(c, "Get list successfully", list)
}

// CreateOTAUpgradeTask
// @Router   /api/v1/ota/task [post]
func (*OTAApi) CreateOTAUpgradeTask(c *gin.Context) {
	var req model.CreateOTAUpgradeTaskReq
	if !BindAndValidate(c, &req) {
		return
	}

	err := service.GroupApp.OTA.CreateOTAUpgradeTask(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Create successfully", nil)
}

// DeleteOTAUpgradeTask
// @Router   /api/v1/ota/task/{id} [delete]
func (*OTAApi) DeleteOTAUpgradeTask(c *gin.Context) {
	id := c.Param("id")
	err := service.GroupApp.OTA.DeleteOTAUpgradeTask(id)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Delete successfully", nil)
}

// GetOTAUpgradeTaskByPage
// @Router   /api/v1/ota/task [get]
func (*OTAApi) HandleOTAUpgradeTaskByPage(c *gin.Context) {
	var req model.GetOTAUpgradeTaskListByPageReq
	if !BindAndValidate(c, &req) {
		return
	}
	list, err := service.GroupApp.OTA.GetOTAUpgradeTaskListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "get successfully", list)
}

// GetOTAUpgradeTaskDetailByPage
// @Router   /api/v1/ota/task/detail [get]
func (*OTAApi) HandleOTAUpgradeTaskDetailByPage(c *gin.Context) {
	var req model.GetOTAUpgradeTaskDetailReq
	if !BindAndValidate(c, &req) {
		return
	}
	list, err := service.GroupApp.OTA.GetOTAUpgradeTaskDetailListByPage(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Get successfully", list)

}

// UpdateOTAUpgradeTaskStatus 更新升级任务状态
// @Router   /api/v1/ota/task/detail [put]
func (*OTAApi) UpdateOTAUpgradeTaskStatus(c *gin.Context) {
	var req model.UpdateOTAUpgradeTaskStatusReq
	if !BindAndValidate(c, &req) {
		return
	}
	err := service.GroupApp.OTA.UpdateOTAUpgradeTaskStatus(&req)
	if err != nil {
		ErrorHandler(c, http.StatusInternalServerError, err)
		return
	}
	SuccessHandler(c, "Update successfully", nil)
}

// GET /api/v1/ota/download/{filepath}
func (*OTAApi) DownloadOTAUpgradePackage(c *gin.Context) {
	filePath := "./files/upgradePackage/" + c.Param("path") + "/" + c.Param("file")

	if !utils.FileExist(filePath) {
		ErrorHandler(c, http.StatusNotFound, errors.New("file not found"))
		return
	}

	rangeHeader := c.GetHeader("Range")
	crc16Method := c.GetHeader("Crc16-Method")

	if rangeHeader == "" {
		c.File(filePath)
		return
	}

	//发送文件部分内容
	serveRangeFile(filePath, rangeHeader, crc16Method, c)
}

func serveRangeFile(filePath, rangeHeader, crc16Method string, c *gin.Context) {
	rangeStr := strings.Replace(rangeHeader, "bytes=", "", 1)
	rangeParts := strings.Split(rangeStr, "-")
	if len(rangeParts) != 2 {
		c.AbortWithError(http.StatusRequestedRangeNotSatisfiable, errors.New("invalid range"))
	}

	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		c.AbortWithError(http.StatusRequestedRangeNotSatisfiable, errors.New("invalid range"))
	}

	file, err := os.Open(filePath)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
	}

	fileSize := fileInfo.Size()

	if rangeParts[1] == "" {
		rangeParts[1] = fmt.Sprintf("%d", fileSize-1)
	}
	end, err := strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		c.AbortWithStatus(http.StatusRequestedRangeNotSatisfiable)
	}

	if start >= fileSize || end >= fileSize {
		c.AbortWithStatus(http.StatusBadRequest)
	}

	contentLength := end - start + 1

	c.Writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	c.Writer.Header().Set("Accept-Ranges", "bytes")
	c.Writer.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
	c.Writer.Header().Set("Content-Type", filePath[len(filePath)-3:])
	c.Writer.Flush()

	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
	}
	// 创建一个缓冲区
	buffer := make([]byte, contentLength)

	// 从文件中读取数据到缓冲区
	_, err = file.Read(buffer)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
	}
	switch crc16Method {
	case "CCITT":
		// 计算CRC16校验码
		crcValue := crc16.ChecksumCCITT(buffer)
		// 将校验码添加到HTTP响应的头部中
		c.Writer.Header().Set("X-CRC16", fmt.Sprintf("%04x", crcValue))
		// 将缓冲区数据写入响应
		c.Writer.Write(buffer)
		c.Writer.Flush()
	case "MODBUS":
		// 计算CRC16校验码
		crcValue := crc16.ChecksumMBus(buffer)
		// 将校验码添加到HTTP响应的头部中
		c.Writer.Header().Set("X-CRC16", fmt.Sprintf("%04x", crcValue))

		// 将缓冲区数据写入响应
		c.Writer.Write(buffer)
		c.Writer.Flush()
	default:
		// 计算CRC16-IBM校验码
		crcValue := crc16.ChecksumIBM(buffer)

		// 将校验码添加到HTTP响应的头部中
		c.Writer.Header().Set("X-CRC16", fmt.Sprintf("%04x", crcValue))

		// 将缓冲区数据写入响应
		c.Writer.Write(buffer)
		c.Writer.Flush()
	}
	file.Sync()
}
