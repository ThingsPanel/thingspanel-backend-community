package service

import (
	"context"
	"fmt"
	"time"

	dal "project/internal/dal"
	model "project/internal/model"
	query "project/internal/query"
	common "project/pkg/common"
	utils "project/pkg/utils"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type Board struct{}

func (p *Board) CreateBoard(ctx context.Context, CreateBoardReq *model.CreateBoardReq) (*model.Board, error) {
	var (
		board = model.Board{}
		db    = dal.BoardQuery{}
	)

	board.ID = uuid.New()
	board.Name = CreateBoardReq.Name
	if CreateBoardReq.Config != nil && !IsJSON(*CreateBoardReq.Config) {
		return nil, fmt.Errorf("config is not a valid JSON")
	}
	board.Config = CreateBoardReq.Config
	board.MenuFlag = &CreateBoardReq.MenuFlag
	board.Description = CreateBoardReq.Description
	board.Remark = CreateBoardReq.Remark
	board.UpdatedAt = time.Now().UTC()
	board.CreatedAt = time.Now().UTC()
	board.TenantID = CreateBoardReq.TenantID
	board.HomeFlag = CreateBoardReq.HomeFlag
	// 一个租户的首页看板只能存在一个
	if CreateBoardReq.HomeFlag == "Y" {
		err := db.UpdateHomeFlagN(ctx, CreateBoardReq.TenantID)
		if err != nil {
			logrus.Error(err)
			return nil, err
		}
	}
	boardInfo, err := db.Create(ctx, &board)
	if err != nil {
		logrus.Error(err)
	}

	return boardInfo, err
}

func (p *Board) UpdateBoard(ctx context.Context, UpdateBoardReq *model.UpdateBoardReq) (*model.Board, error) {
	var db = dal.BoardQuery{}
	var board = model.Board{}
	board.ID = UpdateBoardReq.Id
	board.Name = UpdateBoardReq.Name
	// 校验是否json格式字符串
	if UpdateBoardReq.Config != nil && !IsJSON(*UpdateBoardReq.Config) {
		return nil, fmt.Errorf("config is not a valid JSON")
	}
	board.Config = UpdateBoardReq.Config
	board.HomeFlag = UpdateBoardReq.HomeFlag
	board.MenuFlag = &UpdateBoardReq.MenuFlag
	board.Description = UpdateBoardReq.Description
	board.Remark = UpdateBoardReq.Remark
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
					return nil, err
				}
			}
		}
		err := dal.UpdateBoard(&board)
		if err != nil {
			logrus.Error(err)
			return nil, err
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
		// 没有id则新增，但是需要判断是否有首页看板，如果有则不允许新增
		if board.HomeFlag == "Y" {
			_, err := db.First(ctx, query.Board.TenantID.Eq(UpdateBoardReq.TenantID), query.Board.HomeFlag.Eq("Y"))
			if err != nil {
				logrus.Error(err)
			} else {
				return nil, fmt.Errorf("home board already exists")
			}
		}
		boardInfo, err := db.Create(ctx, &board)
		if err != nil {
			logrus.Error(err)
		}
		return boardInfo, err
	}
	return &board, nil
}

func (p *Board) DeleteBoard(id string) error {
	err := dal.DeleteBoard(id)
	return err
}

func (p *Board) GetBoardListByPage(Params *model.GetBoardListByPageReq, U *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.GetBoardListByPage(Params, U.TenantID)
	if err != nil {
		return nil, err
	}
	boardListRsp := make(map[string]interface{})
	boardListRsp["total"] = total
	boardListRsp["list"] = list

	return boardListRsp, err
}

func (p *Board) GetBoard(id string, U *utils.UserClaims) (interface{}, error) {
	board, err := dal.GetBoard(id, U.TenantID)
	if err != nil {
		return nil, err
	}

	return board, err
}

func (p *Board) GetBoardListByTenantId(tenantid string) (interface{}, error) {
	_, data, err := dal.GetBoardListByTenantId(tenantid)
	if err != nil {
		return nil, err
	}
	return data, err
}

// GetDeviceTotal
// @AUTHOR:zxq
// @DATE: 2024-03-01 19:04
// @DESCRIPTIONS: 获得设备总数
func (p *Board) GetDeviceTotal(ctx context.Context, authority string, tenantID string) (int64, error) {
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

	return total, err
}

// GetDevice
// @AUTHOR:zxq
// @DATE: 2024-03-04 09:04
// @DESCRIPTIONS: 获得设备总数/激活数
func (p *Board) GetDevice(ctx context.Context) (data *model.GetBoardDeviceRes, err error) {
	var (
		total, on int64
		device    = query.Device
		db        = dal.DeviceQuery{}
	)

	total, err = db.Count(ctx)
	if err != nil {
		logrus.Error(ctx, "[GetDevice]Device count failed:", err)
		return
	}
	on, err = db.CountByWhere(ctx, device.ActivateFlag.Eq("active"))
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

// GetDeviceByTenantID
// @AUTHOR:zxq
// @DATE: 2024-03-04 09:04
// @DESCRIPTIONS: 获得设备总数/激活数
func (p *Board) GetDeviceByTenantID(ctx context.Context, tenantID string) (data *model.GetBoardDeviceRes, err error) {
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
