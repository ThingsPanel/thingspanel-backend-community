package dal

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "project/internal/model"
	query "project/internal/query"

	"gorm.io/gen"
	"gorm.io/gorm"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

func CreateBoard(boards *model.Board) error {
	if _, err := query.Board.Where(query.Board.HomeFlag.Eq("Y"), query.Board.TenantID.Eq(boards.TenantID)).First(); err == nil {
		return fmt.Errorf("首页看板已存在")
	}
	return query.Board.Create(boards)
}

func UpdateBoard(boards *model.Board) error {
	p := query.Board
	r, err := query.Board.Where(p.ID.Eq(boards.ID)).Updates(boards)
	if err != nil {
		logrus.Error(err)
		return err
	}
	if r.RowsAffected == 0 {
		return fmt.Errorf("no data updated")
	}
	return err
}

func DeleteBoard(id string) error {
	r, err := query.Board.Where(query.Board.ID.Eq(id)).Delete()
	// 错误的id接口也返回成功
	if r.RowsAffected == 0 {
		return nil
	}
	if err != nil {
		logrus.Error(err)
	}
	return err
}

func GetBoardListByPage(boards *model.GetBoardListByPageReq, tenantId string) (int64, interface{}, error) {
	q := query.Board
	var count int64
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantId))

	if boards.Name != nil && *boards.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *boards.Name)))
	}

	if boards.HomeFlag != nil && *boards.HomeFlag != "" {
		queryBuilder = queryBuilder.Where(q.HomeFlag.Eq(*boards.HomeFlag))
	}
	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}
	if boards.Page != 0 && boards.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(boards.PageSize)
		queryBuilder = queryBuilder.Offset((boards.Page - 1) * boards.PageSize)
	}
	queryBuilder = queryBuilder.Order(q.CreatedAt.Desc())
	boardsList, err := queryBuilder.Select(q.ID, q.Name, q.HomeFlag, q.MenuFlag, q.UpdatedAt, q.CreatedAt, q.Description, q.Remark, q.TenantID).Find()
	if err != nil {
		logrus.Error(err)
		return count, boardsList, err
	}

	return count, boardsList, err
}

func GetBoard(id string, tenantId string) (interface{}, error) {
	p := query.Board
	board, err := query.Board.Where(p.ID.Eq(id)).Where(p.TenantID.Eq(tenantId)).Select().First()
	if err != nil {
		logrus.Error(err)
	}
	return board, err
}

func GetBoardListByTenantId(tenantid string) (int64, interface{}, error) {
	q := query.Board
	var count int64
	queryBuilder := q.WithContext(context.Background())
	boardsList, err := queryBuilder.Where(q.TenantID.Eq(tenantid), q.HomeFlag.Eq("Y")).Select().First()
	if err != nil {
		// 如果没有首页看板，返回空
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return count, nil, nil
		}
		logrus.Error(err)
		return count, boardsList, err
	}
	count, err = queryBuilder.Count()
	return count, boardsList, err
}

type BoardQuery struct{}

func (BoardQuery) Create(ctx context.Context, info *model.Board) (*model.Board, error) {
	var (
		board = query.Board
		err   error
	)
	if err = board.WithContext(ctx).Create(info); err != nil {
		logrus.Error(ctx, "[BoardQuery]First failed:", err)
	}
	return info, err
}

func (BoardQuery) First(ctx context.Context, option ...gen.Condition) (info *model.Board, err error) {
	board := query.Board
	info, err = board.WithContext(ctx).Where(option...).First()
	if err != nil {
		logrus.Error(ctx, "[BoardQuery]First failed:", err)
	}
	return info, err
}

// 将租户其他的首页看板设置为非首页
func (BoardQuery) UpdateHomeFlagN(ctx context.Context, tenantid string) error {
	var (
		board = query.Board
		err   error
	)
	if _, err := board.WithContext(ctx).Where(query.Board.TenantID.Eq(tenantid), query.Board.HomeFlag.Eq("Y")).Updates(map[string]interface{}{"home_flag": "N"}); err != nil {
		logrus.Error(ctx, "update failed:", err)
	}
	return err
}

// 给新增的租户新增一个默认的首页看板

func (BoardQuery) CreateDefaultBoard(ctx context.Context, tenantid string) error {
	var (
		board  = query.Board
		config = `[{"x":9,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1743208919659553,"data":{"cardId":"on-num","type":"builtin","title":"在线设备数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{"metricsOptions":[],"metricsOptionsFetched":false}],"deviceCount":1}},"moved":false},{"x":3,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1743208917510422,"data":{"cardId":"news-num","type":"builtin","title":"消息总数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{"showTitle":false,"title":"消息总数"},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{"metricsOptions":[],"metricsOptionsFetched":false}],"deviceCount":1}},"moved":false},{"x":0,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1743208916745868,"data":{"cardId":"access-num","type":"builtin","title":"访问量","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":6,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1743208918343576,"data":{"cardId":"off-num","type":"builtin","title":"离线设备数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{"metricsOptions":[],"metricsOptionsFetched":false}],"deviceCount":1}},"moved":false},{"x":0,"y":2,"w":9,"h":6,"minW":2,"minH":2,"i":1743208921010016,"data":{"cardId":"trend-online","type":"builtin","title":"设备在线趋势","config":{},"layout":{"w":4,"h":3,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false}]`
	)
	// 根据上面sql语句，创建默认首页看板
	err := board.WithContext(ctx).Create(&model.Board{
		ID:        uuid.New(),
		Name:      "Home",
		Config:    &config,
		TenantID:  tenantid,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		HomeFlag:  "Y",
		Remark:    nil,
	})
	if err != nil {
		logrus.Error(ctx, "[BoardQuery]CreateDefaultBoard failed:", err)
	}
	return err
}
