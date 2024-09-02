package dal

import (
	"context"
	"fmt"
	"time"

	global "project/global"
	model "project/internal/model"
	query "project/query"

	"github.com/go-basic/uuid"
	"github.com/sirupsen/logrus"
)

func CreateOTAUpgradeTaskWithDetail(req *model.CreateOTAUpgradeTaskReq) ([]*model.OtaUpgradeTaskDetail, error) {

	var task = model.OtaUpgradeTask{}
	var taskDetail = []*model.OtaUpgradeTaskDetail{}

	t := time.Now().UTC()
	taskId := uuid.New()

	task.ID = taskId
	task.Name = req.Name
	task.OtaUpgradePackageID = req.OTAUpgradePackageId
	task.Description = req.Description
	task.CreatedAt = t
	task.Remark = req.Remark

	for _, v := range req.DeviceIdList {
		detail := &model.OtaUpgradeTaskDetail{}
		detail.ID = uuid.New()
		detail.DeviceID = v
		detail.Status = 1
		detail.UpdatedAt = &t
		detail.OtaUpgradeTaskID = taskId
		taskDetail = append(taskDetail, detail)
	}

	tx := query.Use(global.DB).Begin()

	if tx.Error != nil {
		return nil, tx.Error
	}

	if err := tx.OtaUpgradeTask.Create(&task); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.OtaUpgradeTaskDetail.CreateInBatches(taskDetail, len(taskDetail)); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return taskDetail, nil

}

func DeleteOTAUpgradeTask(id string) error {
	_, err := query.OtaUpgradeTask.Where(query.OtaUpgradeTask.ID.Eq(id)).Delete()
	return err
}

func GetOtaUpgradeTaskListByPage(p *model.GetOTAUpgradeTaskListByPageReq) (int64, []map[string]interface{}, error) {
	// 初始化SQL WHERE子句和参数
	whereClause := "WHERE t.ota_upgrade_package_id = ?"
	params := []interface{}{p.OTAUpgradePackageId}

	// 构建查询总数的SQL
	countSQL := `SELECT COUNT(*) FROM ota_upgrade_tasks t ` + whereClause

	// 查询总数
	var totalCount int64
	err := global.DB.Raw(countSQL, params...).Scan(&totalCount).Error
	if err != nil {
		return 0, nil, err
	}

	// 如果没有数据或分页参数不合法，直接返回
	if totalCount == 0 || p.Page <= 0 || p.PageSize <= 0 {
		return 0, []map[string]interface{}{}, nil
	}

	// 构建数据查询的SQL
	dataSQL := `SELECT t.*, 
                       (SELECT COUNT(*) 
                        FROM ota_upgrade_task_details d 
                        WHERE d.ota_upgrade_task_id = t.id) AS device_count 
                FROM ota_upgrade_tasks t ` + whereClause +
		" ORDER BY t.created_at DESC LIMIT ? OFFSET ?"

	// 添加分页参数
	params = append(params, p.PageSize, (p.Page-1)*p.PageSize)

	// 查询数据
	var tasks []map[string]interface{}
	err = global.DB.Raw(dataSQL, params...).Scan(&tasks).Error
	if err != nil {
		return 0, nil, err
	}

	return totalCount, tasks, nil
}

func GetOtaUpgradeTaskDetailListByPage(p *model.GetOTAUpgradeTaskDetailReq) (int64, interface{}, interface{}, error) {

	var count int64
	type StatusCount struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	detailDataMap := make([]map[string]interface{}, 0)
	// 查询统计类信息
	statsResult := make([]StatusCount, 0)
	statsData := query.Device
	otaTaskDetail := query.OtaUpgradeTaskDetail

	queryBuilder := statsData.WithContext(context.Background())
	queryBuilder = queryBuilder.Join(otaTaskDetail, otaTaskDetail.DeviceID.EqCol(statsData.ID))
	if p.DeviceName != nil {
		queryBuilder = queryBuilder.Where(statsData.Name.Like(fmt.Sprintf("%%%s%%", *p.DeviceName)))
	}
	queryBuilder = queryBuilder.Where(otaTaskDetail.OtaUpgradeTaskID.Eq(p.OtaUpgradeTaskId))
	queryBuilder = queryBuilder.Select(otaTaskDetail.Status, otaTaskDetail.Status.Count()).Group(otaTaskDetail.Status)
	err := queryBuilder.Scan(&statsResult)
	if err != nil {
		logrus.Error(err)
		return count, nil, statsResult, err
	}

	// 查询详情
	detailData := query.Device
	detailDataBuilder := detailData.WithContext(context.Background())
	detailDataBuilder = detailDataBuilder.Join(otaTaskDetail, otaTaskDetail.DeviceID.EqCol(detailData.ID))

	// 模糊查询
	if p.DeviceName != nil && *p.DeviceName != "" {
		detailDataBuilder = detailDataBuilder.Where(detailData.Name.Like(fmt.Sprintf("%%%s%%", *p.DeviceName)))
	}

	// 模糊查询
	if p.TaskStatus != nil {
		detailDataBuilder = detailDataBuilder.Where(detailData.Name.Like(fmt.Sprintf("%%%s%%", *p.DeviceName)))
	}
	detailDataBuilder.Where(otaTaskDetail.OtaUpgradeTaskID.Eq(p.OtaUpgradeTaskId))

	// 分页
	if p.Page != 0 && p.PageSize != 0 {
		detailDataBuilder = detailDataBuilder.Limit(p.PageSize)
		detailDataBuilder = detailDataBuilder.Offset((p.Page - 1) * p.PageSize)
	}
	otaTask := query.OtaUpgradeTask
	otaPackage := query.OtaUpgradePackage

	detailDataBuilder = detailDataBuilder.Join(otaTask, otaTask.ID.EqCol(otaTaskDetail.OtaUpgradeTaskID))

	detailDataBuilder = detailDataBuilder.Join(otaPackage, otaPackage.ID.EqCol(otaTask.OtaUpgradePackageID))

	// 升级任务详情id、设备名称设备编号、设备名、设备版本号、升级包版本号、升级进度、更新时间、状态、状态详情
	detailDataBuilder = detailDataBuilder.Select(
		otaTaskDetail.ID, otaTaskDetail.OtaUpgradeTaskID, detailData.DeviceNumber, detailData.Name, detailData.CurrentVersion,
		otaPackage.Version, otaTaskDetail.Step, otaTaskDetail.UpdatedAt,
		otaTaskDetail.Status, otaTaskDetail.StatusDescription,
	)

	err = detailDataBuilder.Scan(&detailDataMap)
	if err != nil {
		logrus.Error(err)
		return count, detailDataMap, statsResult, err
	}
	count, err = detailDataBuilder.Count()
	return count, detailDataMap, statsResult, err

}
