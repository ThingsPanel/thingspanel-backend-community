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
-- 1. 将查询边界对齐到自然小时，避免非整点时间与状态小时桶无法关联
time_bounds AS (
    SELECT
        date_trunc('hour', $2::timestamptz) AS start_hour,
        date_trunc('hour', $3::timestamptz) AS end_hour
),
-- 2. 生成查询范围内所有整点小时序列
hour_series AS (
    SELECT generate_series AS hour_ts
    FROM time_bounds,
         generate_series(start_hour, end_hour, '1 hour') AS generate_series
),
-- 3. 设备总数
device_total AS (
    SELECT COUNT(*)::bigint AS total_cnt
    FROM devices
    WHERE tenant_id = $1
      AND created_at <= $3
),
-- 4. 每台设备在查询起点的最后状态，作为累计计算的基线
initial_status AS (
    SELECT DISTINCT ON (device_id)
        device_id,
        status,
        change_time,
        id
    FROM device_status_history
    WHERE tenant_id = $1
      AND change_time <= $2
    ORDER BY device_id, change_time DESC, id DESC
),
initial_online AS (
    SELECT COUNT(*) FILTER (WHERE status = 1)::bigint AS cnt
    FROM initial_status
),
-- 5. 合并查询起点状态和范围内事件，以便正确识别第一条事件是否真的改变状态
status_events AS (
    SELECT
        device_id, status, change_time, id, true AS is_initial
    FROM initial_status
    UNION ALL
    SELECT
        device_id, status, change_time, id, false AS is_initial
    FROM device_status_history
    WHERE tenant_id = $1
      AND change_time > $2
      AND change_time <= $3
),
-- 6. 标记每条事件之前的设备状态；无历史记录的设备默认离线
ordered_events AS (
    SELECT
        device_id,
        status,
        change_time,
        is_initial,
        LAG(status, 1, 0::smallint) OVER (
            PARTITION BY device_id ORDER BY change_time, id
        ) AS prev_status
    FROM status_events
),
-- 7. 每小时净状态变化；连续重复状态不会重复计数
hourly_delta AS (
    SELECT
        date_trunc('hour', change_time) AS hour_ts,
        SUM(
            CASE
                WHEN status = prev_status THEN 0
                WHEN status = 1 THEN 1
                ELSE -1
            END
        )::bigint AS status_delta
    FROM ordered_events
    WHERE NOT is_initial
    GROUP BY date_trunc('hour', change_time)
),
-- 8. 合并小时序列、初始在线数和每小时净变化
merged AS (
    SELECT
        s.hour_ts,
        (SELECT cnt FROM initial_online) AS init_online,
        COALESCE(h.status_delta, 0)::bigint AS status_delta
    FROM hour_series s
    LEFT JOIN hourly_delta h ON h.hour_ts = s.hour_ts
),
-- 9. 初始在线数加每小时净变化的累计和，得到各小时在线数
with_online AS (
    SELECT
        hour_ts,
        GREATEST(
            init_online + SUM(status_delta) OVER (
                ORDER BY hour_ts ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
            ),
            0
        )::bigint AS cur_online
    FROM merged
)
-- 10. 最终输出：在线 = 累计在线数；离线 = 总数 - 在线数
SELECT
    w.hour_ts                              AS timestamp,
    t.total_cnt                            AS device_total,
    w.cur_online                           AS device_online,
    (t.total_cnt - w.cur_online)::bigint   AS device_offline
FROM with_online w
CROSS JOIN device_total t
ORDER BY w.hour_ts ASC;
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
