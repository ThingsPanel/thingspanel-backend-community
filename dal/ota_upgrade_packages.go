package dal

import (
	"context"
	"fmt"

	model "project/model"
	query "project/query"

	"github.com/sirupsen/logrus"
	"gorm.io/gen"
)

// ALTER TABLE ota_upgrade_packages ALTER COLUMN additional_info SET DEFAULT '{}'::json;

func CreateOtaUpgradePackage(p *model.OtaUpgradePackage) error {
	return query.OtaUpgradePackage.Create(p)
}

func UpdateOtaUpgradePackage(p *model.OtaUpgradePackage) (gen.ResultInfo, error) {
	info, err := query.OtaUpgradePackage.Updates(p)
	return info, err
}

func DeleteOtaUpgradePackage(packageId string) error {
	info, err := query.OtaUpgradePackage.Where(query.OtaUpgradePackage.ID.Eq(packageId)).Delete()
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}
	return nil
}

func GetOtaUpgradePackageByID(id string) (*model.OtaUpgradePackage, error) {
	ota, err := query.OtaUpgradePackage.Where(query.OtaUpgradePackage.ID.Eq(id)).First()
	if err != nil {
		logrus.Error(err)
	}
	return ota, err
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
