package dal

import (
	"context"

	"project/pkg/global"
)

func InsertOTAProgressLog(id, detailID, deviceID string, step *int16, status int16, desc, payload string) error {
	return global.DB.WithContext(context.Background()).Exec(
		`INSERT INTO ota_upgrade_progress_logs (id, task_detail_id, device_id, steps, status, description, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, detailID, deviceID, step, status, desc, payload,
	).Error
}

type OTAProgressLogRow struct {
	ID           string `gorm:"column:id" json:"id"`
	TaskDetailID string `gorm:"column:task_detail_id" json:"task_detail_id"`
	DeviceID     string `gorm:"column:device_id" json:"device_id"`
	Steps        *int16 `gorm:"column:steps" json:"steps"`
	Status       int16  `gorm:"column:status" json:"status"`
	Description  string `gorm:"column:description" json:"description"`
	Payload      string `gorm:"column:payload" json:"payload"`
	CreatedAt    string `gorm:"column:created_at" json:"created_at"`
}

func ListOTAProgressLogs(detailID, tenantID string) ([]OTAProgressLogRow, error) {
	var rows []OTAProgressLogRow
	err := global.DB.WithContext(context.Background()).Raw(
		`SELECT l.id, l.task_detail_id, l.device_id, l.steps, l.status, l.description, l.payload, l.created_at::text
		 FROM ota_upgrade_progress_logs l
		 JOIN ota_upgrade_task_details d ON d.id = l.task_detail_id
		 JOIN ota_upgrade_tasks t ON t.id = d.ota_upgrade_task_id
		 JOIN ota_upgrade_packages p ON p.id = t.ota_upgrade_package_id
		 WHERE l.task_detail_id = ? AND p.tenant_id = ?
		 ORDER BY l.created_at ASC`,
		detailID, tenantID,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []OTAProgressLogRow{}
	}
	return rows, nil
}
