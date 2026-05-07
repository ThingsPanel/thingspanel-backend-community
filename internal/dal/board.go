package dal

import (
	"context"
	"errors"
	"fmt"
	"time"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"

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

	if boards.VisType != nil && *boards.VisType != "" {
		queryBuilder = queryBuilder.Where(q.VisType.Eq(*boards.VisType))
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
	boardsList, err := queryBuilder.Select(q.ID, q.Name, q.HomeFlag, q.MenuFlag, q.UpdatedAt, q.CreatedAt, q.Description, q.Remark, q.TenantID, q.VisType).Find()
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

// GetDeviceTrend 获取设备在线趋势（按小时聚合）
// tenantID: 租户ID
// startTime: 查询起始时间（Unix时间戳，秒），nil则默认当前时间-48h
// endTime: 查询结束时间（Unix时间戳，秒），nil则默认当前时间
func GetDeviceTrend(tenantID string, startTime, endTime *int64) ([]model.DeviceTrendPoint, error) {
	now := time.Now()
	if endTime == nil {
		t := now.Unix()
		endTime = &t
	}
	if startTime == nil {
		t := now.Add(-48 * time.Hour).Unix()
		startTime = &t
	}

	startTimeUTC := time.Unix(*startTime, 0).UTC()
	endTimeUTC := time.Unix(*endTime, 0).UTC()

	var results []model.DeviceTrendPoint

	sql := `
WITH
-- 1. 取每台设备每小时最后一次状态变更（基于change_time的自然小时）
latest_hourly AS (
    SELECT
        dsh.tenant_id,
        dsh.device_id,
        dsh.status,
        date_trunc('hour', dsh.change_time) AS hour_ts
    FROM device_status_history dsh
    INNER JOIN (
        SELECT
            tenant_id,
            device_id,
            hour_ts,
            MAX(id) AS max_id
        FROM (
            SELECT
                tenant_id,
                device_id,
                id,
                date_trunc('hour', change_time) AS hour_ts
            FROM device_status_history
            WHERE tenant_id = $1
              AND change_time >= $2
              AND change_time <= $3
        ) per_hour
        GROUP BY tenant_id, device_id, hour_ts
    ) latest ON dsh.id = latest.max_id
),
-- 2. 有变更记录的设备按时长聚合在线/离线数
with_history AS (
    SELECT
        tenant_id,
        hour_ts,
        COUNT(*) FILTER (WHERE status = 1) AS with_online,
        COUNT(*) FILTER (WHERE status = 0) AS with_offline
    FROM latest_hourly
    GROUP BY tenant_id, hour_ts
),
-- 3. 该租户在查询结束时间前已创建的设备总数
device_total_cnt AS (
    SELECT COUNT(*) AS total_cnt
    FROM devices
    WHERE tenant_id = $1
      AND created_at <= $3
),
-- 4. 有变更记录的设备去重总数
with_history_device_cnt AS (
    SELECT COUNT(DISTINCT device_id) AS cnt
    FROM latest_hourly
),
-- 5. 从未有过变更记录的设备数 = 设备总数 - 有记录的设备数
never_changed_cnt AS (
    SELECT GREATEST(0,
        (SELECT total_cnt FROM device_total_cnt) -
        (SELECT cnt FROM with_history_device_cnt)
    ) AS cnt
)
-- 6. 合并
SELECT
    h.hour_ts                                            AS timestamp,
    t.total_cnt                                           AS device_total,
    COALESCE(h.with_online, 0)::bigint                   AS device_online,
    (COALESCE(h.with_offline, 0) + n.cnt)::bigint        AS device_offline
FROM with_history h
CROSS JOIN device_total_cnt t
CROSS JOIN never_changed_cnt n
ORDER BY h.hour_ts ASC;
`
	err := global.DB.Raw(sql, tenantID, startTimeUTC, endTimeUTC).Scan(&results).Error
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"tenant_id":  tenantID,
			"startTime":  startTimeUTC,
			"endTime":    endTimeUTC,
		}).Error("GetDeviceTrend query failed")
		return nil, err
	}

	return results, nil
}

// 给新增的租户新增一个默认的首页看板

func (BoardQuery) CreateDefaultBoard(ctx context.Context, tenantid string) error {
	var (
		board  = query.Board
		config = `[{"x":9,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1745327924610429,"data":{"cardId":"alarm-count","type":"builtin","title":"告警数量","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{"metricsOptions":[],"metricsOptionsFetched":false}],"deviceCount":1}},"moved":false},{"x":3,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1745306021843058,"data":{"cardId":"off-num","type":"builtin","title":"离线设备数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":0,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1745296008998001,"data":{"cardId":"access-num","type":"builtin","title":"设备总数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":6,"y":0,"w":3,"h":2,"minW":2,"minH":2,"i":1745306022634299,"data":{"cardId":"on-num","type":"builtin","title":"在线设备数","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":9,"y":2,"w":3,"h":5,"minW":2,"minH":2,"i":1745511461442040,"data":{"cardId":"app-download","type":"builtin","title":"下载移动端","config":{},"layout":{"w":2,"h":2,"minW":2,"minH":2},"basicSettings":{},"dataSource":{"origin":"device","isSupportTimeRange":true,"dataTimeRange":"1h","isSupportAggregate":true,"dataAggregateRange":"1m","systemSource":[],"deviceSource":[]}},"moved":false},{"x":3,"y":2,"w":2,"h":5,"minW":2,"minH":2,"i":1745499419664080,"data":{"cardId":"recently-visited","type":"builtin","title":"card.recentlyVisited.title","config":{},"layout":{"w":3,"h":2,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":5,"y":2,"w":4,"h":5,"minW":2,"minH":2,"i":1745306025963299,"data":{"cardId":"trend-online","type":"builtin","title":"设备在线趋势","config":{},"layout":{"w":4,"h":3,"minH":2,"minW":2},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false},{"x":0,"y":2,"w":3,"h":5,"minW":2,"minH":2,"i":1745374614338702,"data":{"cardId":"operation-guide","type":"builtin","title":"操作向导","config":{"guideList":[{"titleKey":"card.operationGuideCard.guideItems.addDevice.title","descriptionKey":"card.operationGuideCard.guideItems.addDevice.description","link":"/device/manage"},{"titleKey":"card.operationGuideCard.guideItems.configureDevice.title","descriptionKey":"card.operationGuideCard.guideItems.configureDevice.description"},{"titleKey":"card.operationGuideCard.guideItems.createDashboard.title","descriptionKey":"card.operationGuideCard.guideItems.createDashboard.description"}]},"layout":{"w":3,"h":5,"minW":2,"minH":2},"basicSettings":{},"dataSource":{"origin":"system","isSupportTimeRange":false,"dataTimeRange":"","isSupportAggregate":false,"dataAggregateRange":"","systemSource":[],"deviceSource":[]}},"moved":false},{"x":6,"y":7,"w":3,"h":6,"minW":2,"minH":2,"i":1745420206359165,"data":{"cardId":"reported-data","type":"builtin","title":"cards.reportedData","config":{},"layout":{"w":2,"h":2,"minW":2,"minH":2},"basicSettings":{},"dataSource":{"origin":"device","isSupportTimeRange":true,"dataTimeRange":"1h","isSupportAggregate":true,"dataAggregateRange":"1m","systemSource":[],"deviceSource":[]}},"moved":false},{"x":0,"y":7,"w":6,"h":6,"minW":2,"minH":2,"i":1745502189663242,"data":{"cardId":"alarm-info","type":"builtin","title":"cards.alarmInfo.title","config":{},"layout":{"w":2,"h":2,"minW":2,"minH":2},"basicSettings":{},"dataSource":{"origin":"device","isSupportTimeRange":true,"dataTimeRange":"1h","isSupportAggregate":true,"dataAggregateRange":"1m","systemSource":[],"deviceSource":[]}},"moved":false},{"x":9,"y":7,"w":3,"h":6,"minW":2,"minH":1,"i":1745511464685393,"data":{"cardId":"version-info","type":"builtin","title":"版本信息","config":{},"layout":{"w":3,"h":1,"minW":2,"minH":1},"basicSettings":{},"dataSource":{"origin":"system","systemSource":[{}],"deviceSource":[{}]}},"moved":false}]`
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
