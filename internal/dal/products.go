package dal

import (
	"context"
	"fmt"

	model "project/internal/model"
	query "project/internal/query"

	"github.com/sirupsen/logrus"
)

func CreateProduct(p *model.Product) error {
	return query.Product.Create(p)
}

func UpdateProduct(p *model.Product) error {
	_, err := query.Product.Updates(p)
	return err
}

func DeleteProduct(id string) error {
	info, err := query.Product.Where(query.Product.ID.Eq(id)).Delete()
	if err != nil {
		return err
	}
	if info.RowsAffected == 0 {
		return fmt.Errorf("no data deleted")
	}
	return nil
}

func GetProductByID(id string) (*model.Product, error) {
	return query.Product.Where(query.Product.ID.Eq(id)).First()
}

func GetProductListByPage(req *model.GetProductListByPageReq, tenantID string) (int64, []model.ProductList, error) {
	q := query.Product
	queryBuilder := q.WithContext(context.Background())
	if tenantID != "" {
		queryBuilder = queryBuilder.Where(q.TenantID.Eq(tenantID))
	}
	if req.Name != nil && *req.Name != "" {
		queryBuilder = queryBuilder.Where(q.Name.Like(fmt.Sprintf("%%%s%%", *req.Name)))
	}
	if req.ProductModel != nil && *req.ProductModel != "" {
		queryBuilder = queryBuilder.Where(q.ProductModel.Like(fmt.Sprintf("%%%s%%", *req.ProductModel)))
	}
	if req.ProductType != nil && *req.ProductType != "" {
		queryBuilder = queryBuilder.Where(q.ProductType.Eq(*req.ProductType))
	}

	count, err := queryBuilder.Count()
	if err != nil {
		logrus.Error(err)
		return 0, nil, err
	}
	if req.Page != 0 && req.PageSize != 0 {
		queryBuilder = queryBuilder.Limit(req.PageSize).Offset((req.Page - 1) * req.PageSize)
	}

	d := query.DeviceConfig
	var list []model.ProductList
	err = queryBuilder.Select(q.ALL, d.Name.As("device_config_name")).
		LeftJoin(d, d.ID.EqCol(q.DeviceConfigID)).
		Order(q.CreatedAt.Desc()).
		Scan(&list)
	if err != nil {
		logrus.Error(err)
		return count, nil, err
	}
	return count, list, nil
}

func CountDevicesByProductID(productID string) (active int64, inactive int64, err error) {
	q := query.Device
	active, err = q.WithContext(context.Background()).Where(q.ProductID.Eq(productID), q.ActivateFlag.Eq("active")).Count()
	if err != nil {
		return 0, 0, err
	}
	inactive, err = q.WithContext(context.Background()).Where(q.ProductID.Eq(productID), q.ActivateFlag.Neq("active")).Count()
	return active, inactive, err
}

func DeleteInactiveDevicesByProductID(productID string) error {
	q := query.Device
	_, err := q.WithContext(context.Background()).Where(q.ProductID.Eq(productID), q.ActivateFlag.Neq("active")).Delete()
	return err
}

func DeviceNumberExists(deviceNumber string) (bool, error) {
	count, err := query.Device.WithContext(context.Background()).Where(query.Device.DeviceNumber.Eq(deviceNumber)).Count()
	return count > 0, err
}
