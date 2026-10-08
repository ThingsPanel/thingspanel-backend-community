package dal

import (
	"context"
	"encoding/json"
	"fmt"

	model "project/internal/model"
	query "project/internal/query"
	global "project/pkg/global"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
	"gorm.io/gorm"
)

// ALTER TABLE ota_upgrade_packages ALTER COLUMN additional_info SET DEFAULT '{}'::json;

func CreateOtaUpgradePackage(p *model.OtaUpgradePackage, verifyObject func() error) error {
	var metadata struct {
		ObjectKey string `json:"tosObjectKey"`
	}
	if p.AdditionalInfo == nil || json.Unmarshal([]byte(*p.AdditionalInfo), &metadata) != nil || metadata.ObjectKey == "" {
		return fmt.Errorf("OTA TOS object key is required")
	}
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", metadata.ObjectKey).Error; err != nil {
			return err
		}
		if err := verifyObject(); err != nil {
			return err
		}
		return tx.Create(p).Error
	})
}

func UpdateOtaUpgradePackage(p *model.OtaUpgradePackage, tenantID string) (gen.ResultInfo, error) {
	info, err := query.OtaUpgradePackage.Where(query.OtaUpgradePackage.ID.Eq(p.ID), query.OtaUpgradePackage.TenantID.Eq(tenantID)).Updates(p)
	return info, err
}

func GetOtaUpgradePackageByIDAndTenant(id, tenantID string) (*model.OtaUpgradePackage, error) {
	var ota model.OtaUpgradePackage
	err := global.DB.Where("id = ? AND tenant_id = ?", id, tenantID).First(&ota).Error
	return &ota, err
}

func DeleteOtaUpgradePackageAndTOS(id, tenantID, objectKey string, deleteObject func() error) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", objectKey).Error; err != nil {
			return err
		}
		var references int64
		if err := tx.Model(&model.OtaUpgradePackage{}).Where("additional_info->>'tosObjectKey' = ?", objectKey).Count(&references).Error; err != nil {
			return err
		}
		result := tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&model.OtaUpgradePackage{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("no data deleted")
		}
		if references <= 1 {
			return deleteObject()
		}
		return nil
	})
}

func GetOtaUpgradePackageListByPage(p *model.GetOTAUpgradePackageLisyByPageReq, tenantId string) (int64, interface{}, error) {
	q := query.OtaUpgradePackage
	var count int64
	var packageList []model.GetOTAUpgradeTaskListByPageRsp
	queryBuilder := q.WithContext(context.Background())
	queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantId))
	if p.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", p.Name)))
	}

	if p.DeviceConfigID != "" {
		queryBuilder = queryBuilder.Where(q.DeviceConfigID.Eq(p.DeviceConfigID))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return count, packageList, err
	}

	if p.Page != 0 && p.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(p.PageSize)
		queryBuilder = queryBuilder.Offset((p.Page - 1) * p.PageSize)
	}

	d := query.DeviceConfig
	err = queryBuilder.Select(q.ALL, d.Name.As("device_config_name")).
		LeftJoin(d, d.ID.EqCol(q.DeviceConfigID)).
		Order(q.CreatedAt.Desc()).
		Scan(&packageList)
	if err != nil {
		logrus.Error(err)
		return count, packageList, err
	}
	return count, packageList, err
}
