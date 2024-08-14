package service

import (
	"context"
	"project/dal"
	model "project/model"
	utils "project/utils"
	"time"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

type ExpectedData struct{}

// 创建预期数据
func (e *ExpectedData) Create(ctx context.Context, req *model.CreateExpectedDataReq, userClaims *utils.UserClaims) (*model.ExpectedData, error) {
	// 创建预期数据
	ed := &model.ExpectedData{
		ID:         uuid.New(),
		DeviceID:   req.DeviceID,
		SendType:   req.SendType,
		Payload:    req.Payload,
		CreatedAt:  time.Now(),
		Status:     "pending",
		ExpiryTime: req.Expiry,
		Label:      req.Label,
		TenantID:   userClaims.TenantID,
	}
	err := dal.ExpectedDataDal{}.Create(ctx, ed)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	// 查询预期数据
	ed, err = dal.ExpectedDataDal{}.GetByID(ctx, ed.ID)
	if err != nil {
		logrus.Error(err)
		return nil, err
	}

	return ed, nil

}

// 删除预期数据
func (e *ExpectedData) Delete(ctx context.Context, id string) error {
	return dal.ExpectedDataDal{}.Delete(ctx, id)
}

// 分页查询
func (e *ExpectedData) PageList(ctx context.Context, req *model.GetExpectedDataPageReq, userClaims *utils.UserClaims) (map[string]interface{}, error) {
	total, list, err := dal.ExpectedDataDal{}.PageList(ctx, req, userClaims.TenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"total": total,
		"list":  list,
	}, nil
}
