package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	query "project/internal/query"
	common "project/pkg/common"
	"project/pkg/errcode"
	global "project/pkg/global"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type Board struct{}

func (*Board) CreateBoard(ctx context.Context, CreateBoardReq *model.CreateBoardReq) (*model.Board, error) {
	var (
		board = model.Board{}
		db    = dal.BoardQuery{}
	)

	board.ID = uuid.New()
	board.Name = CreateBoardReq.Name
	if CreateBoardReq.Config != nil && !IsJSON(*CreateBoardReq.Config) {
		return nil, errcode.NewWithMessage(errcode.CodeParamError, "config is not a valid JSON")
	}
	board.Config = CreateBoardReq.Config
	board.MenuFlag = &CreateBoardReq.MenuFlag
	board.Description = CreateBoardReq.Description
	board.Remark = CreateBoardReq.Remark
	board.UpdatedAt = time.Now().UTC()
	board.CreatedAt = time.Now().UTC()
	board.TenantID = CreateBoardReq.TenantID
	board.HomeFlag = CreateBoardReq.HomeFlag
	board.VisType = CreateBoardReq.VisType
	// 一个租户的首页看板只能存在一个
	if CreateBoardReq.HomeFlag == "Y" {
		err := db.UpdateHomeFlagN(ctx, CreateBoardReq.TenantID)
		if err != nil {
			logrus.Error(err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
	}
	boardInfo, err := db.Create(ctx, &board)
	if err != nil {
		logrus.Error(err)
		err = errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return boardInfo, err
}

func (*Board) UpdateBoard(ctx context.Context, UpdateBoardReq *model.UpdateBoardReq) (*model.Board, error) {
	db := dal.BoardQuery{}
	board := model.Board{}
	board.ID = UpdateBoardReq.Id
	board.Name = UpdateBoardReq.Name
	// 校验是否json格式字符串
	if UpdateBoardReq.Config != nil && !IsJSON(*UpdateBoardReq.Config) {
		return nil, errcode.WithVars(100002, map[string]interface{}{
			"field": "config",
			"error": "config is not a valid JSON",
		})
	}
	board.Config = UpdateBoardReq.Config
	board.HomeFlag = UpdateBoardReq.HomeFlag
	board.MenuFlag = &UpdateBoardReq.MenuFlag
	board.Description = UpdateBoardReq.Description
	board.Remark = UpdateBoardReq.Remark
	board.VisType = UpdateBoardReq.VisType
	board.UpdatedAt = time.Now().UTC()
	if UpdateBoardReq.Id != "" {
		if board.HomeFlag == "Y" {
			_, err := db.First(ctx, query.Board.TenantID.Eq(UpdateBoardReq.TenantID), query.Board.HomeFlag.Eq("Y"), query.Board.ID.Neq(UpdateBoardReq.Id))
			if err != nil {
				logrus.Error(err)
			} else {
				// 修改为首页看板时，需要将其他首页看板修改为非首页看板
				err := db.UpdateHomeFlagN(ctx, UpdateBoardReq.TenantID)
				if err != nil {
					logrus.Error(err)
					return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
						"sql_error": err.Error(),
					})
				}
			}
		}
		err := dal.UpdateBoard(&board)
		if err != nil {
			logrus.Error(err)
			return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}

	} else {
		// name不能为空
		if board.Name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if board.HomeFlag == "" {
			board.HomeFlag = "N"
		}
		board.ID = uuid.New()
		board.TenantID = UpdateBoardReq.TenantID
		board.VisType = UpdateBoardReq.VisType
		// 没有id则新增，但是需要判断是否有首页看板，如果有则不允许新增
		if board.HomeFlag == "Y" {
			_, err := db.First(ctx, query.Board.TenantID.Eq(UpdateBoardReq.TenantID), query.Board.HomeFlag.Eq("Y"))
			if err != nil {
				logrus.Error(err)
			} else {
				return nil, errcode.New(203004)
			}
		}
		boardInfo, err := db.Create(ctx, &board)
		if err != nil {
			logrus.Error(err)
			err = errcode.WithData(errcode.CodeDBError, map[string]interface{}{
				"sql_error": err.Error(),
			})
		}
		return boardInfo, err
	}
	return &board, nil
}

func (*Board) DeleteBoard(id string) error {
	err := dal.DeleteBoard(id)
	if err != nil {
		return errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return err
}

func (*Board) GetBoardListByPage(Params *model.GetBoardListByPageReq, U *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetBoardListByPage(Params, U.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	boardListRsp := make(map[string]interface{})
	boardListRsp["total"] = total
	boardListRsp["list"] = list

	return boardListRsp, err
}

func (*Board) GetBoard(id string, U *utils.UserClaims) (interface{}, error) {
	board, err := dal.GetBoard(id, U.TenantID)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return board, err
}

func (*Board) GetBoardListByTenantId(tenantid string) (interface{}, error) {
	_, data, err := dal.GetBoardListByTenantId(tenantid)
	if err != nil {
		return nil, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}
	return data, err
}

// GetDeviceTotal
// @AUTHOR:zxq
// @DATE: 2024-03-01 19:04
// @DESCRIPTIONS: 获得设备总数
func (*Board) GetDeviceTotal(ctx context.Context, authority string, tenantID string) (int64, error) {
	var (
		total int64
		err   error
		db    = dal.DeviceQuery{}
	)
	if common.CheckUserIsAdmin(authority) {
		total, err = db.Count(ctx)
	} else {
		total, err = db.CountByTenantID(ctx, tenantID)
	}
	if err != nil {
		return 0, errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
	}

	return total, err
}

// GetDevice
// @AUTHOR:zxq
// @DATE: 2024-03-04 09:04
// @DESCRIPTIONS: 获得设备总数/激活数
func (*Board) GetDevice(ctx context.Context, U *utils.UserClaims) (data *model.GetBoardDeviceRes, err error) {
	var (
		total, on int64
		device    = query.Device
		db        = dal.DeviceQuery{}
	)

	if !common.CheckUserIsAdmin(U.Authority) {
		total, err = db.CountByTenantID(ctx, U.TenantID)
	} else {
		total, err = db.Count(ctx)
	}
	if err != nil {
		logrus.Error(ctx, "[GetDevice]Device count failed:", err)
		err = errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
		return
	}
	if !common.CheckUserIsAdmin(U.Authority) {
		on, err = db.CountByWhere(ctx, device.ActivateFlag.Eq("active"), device.TenantID.Eq(U.TenantID))
	} else {
		on, err = db.CountByWhere(ctx, device.ActivateFlag.Eq("active"))
	}
	if err != nil {
		logrus.Error(ctx, "[GetDevice]Device count/on failed:", err)
		err = errcode.WithData(errcode.CodeDBError, map[string]interface{}{
			"sql_error": err.Error(),
		})
		return
	}
	data = &model.GetBoardDeviceRes{
		DeviceTotal: total,
		DeviceOn:    on,
	}
	return
}

// GetDeviceByTenantID
// @AUTHOR:zxq
// @DATE: 2024-03-04 09:04
// @DESCRIPTIONS: 获得已激活的设备总数/在线数
func (*Board) GetDeviceByTenantID(ctx context.Context, tenantID string) (data *model.GetBoardDeviceRes, err error) {
	var (
		total, on int64
		device    = query.Device
		db        = dal.DeviceQuery{}
	)

	total, err = db.CountByWhere(ctx, device.TenantID.Eq(tenantID), device.ActivateFlag.Neq("inactive"))
	if err != nil {
		logrus.Error(ctx, "[GetDevice]Device count failed:", err)
		return
	}
	//
	on, err = db.CountByWhere(ctx, device.ActivateFlag.Eq("active"), device.TenantID.Eq(tenantID), device.IsOnline.Eq(1))
	if err != nil {
		logrus.Error(ctx, "[GetDevice]Device count/on failed:", err)
		return
	}
	data = &model.GetBoardDeviceRes{
		DeviceTotal: total,
		DeviceOn:    on,
	}
	return
}

// GetDeviceTrend 获取设备在线趋势
func (*Device) GetDeviceTrend(ctx context.Context, tenantID string) (*model.DeviceTrendRes, error) {
	// 获取当前时间和24小时前的时间
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)

	// 需要查询的日期列表
	dates := []string{
		yesterday.Format("2006-01-02"),
		now.Format("2006-01-02"),
	}

	var allPoints []model.DeviceTrendPoint

	// 遍历每个日期
	for _, date := range dates {
		// 构建Redis key
		key := fmt.Sprintf("device_stats:%s:%s", tenantID, date)

		// 从Redis获取该日期的所有统计数据
		statsJsonList, err := global.REDIS.LRange(ctx, key, 0, -1).Result()
		if err != nil {
			logrus.Errorf("从Redis获取设备统计数据失败: %v", err)
			return nil, errcode.WithData(errcode.CodeCacheError, map[string]interface{}{
				"error": err.Error(),
				"key":   key,
			})
		}

		// 解析每条统计数据
		for _, statsJson := range statsJsonList {
			var statsData struct {
				DeviceTotal int64     `json:"device_total"`
				DeviceOn    int64     `json:"device_on"`
				Timestamp   time.Time `json:"timestamp"`
			}

			if err := json.Unmarshal([]byte(statsJson), &statsData); err != nil {
				logrus.Errorf("解析设备统计数据失败: %v", err)
				continue
			}

			// 只取24小时内的数据
			if statsData.Timestamp.Before(yesterday) {
				continue
			}

			point := model.DeviceTrendPoint{
				Timestamp:     statsData.Timestamp,
				DeviceTotal:   statsData.DeviceTotal,
				DeviceOnline:  statsData.DeviceOn,
				DeviceOffline: statsData.DeviceTotal - statsData.DeviceOn,
			}

			allPoints = append(allPoints, point)
		}
	}

	// 按时间排序
	sort.Slice(allPoints, func(i, j int) bool {
		return allPoints[i].Timestamp.Before(allPoints[j].Timestamp)
	})

	// 如果没有数据，尝试获取当前状态作为最新点
	if len(allPoints) == 0 {
		currentStats, err := GroupApp.Board.GetDeviceByTenantID(ctx, tenantID)
		if err != nil {
			return nil, err
		}

		point := model.DeviceTrendPoint{
			Timestamp:     now,
			DeviceTotal:   currentStats.DeviceTotal,
			DeviceOnline:  currentStats.DeviceOn,
			DeviceOffline: currentStats.DeviceTotal - currentStats.DeviceOn,
		}

		allPoints = append(allPoints, point)
	}

	return &model.DeviceTrendRes{
		Points: allPoints,
	}, nil
}
