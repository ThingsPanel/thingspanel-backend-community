// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.
// Code generated by gorm.io/gen. DO NOT EDIT.

package query

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"gorm.io/gen"
	"gorm.io/gen/field"

	"gorm.io/plugin/dbresolver"

	"project/internal/model"
)

func newDeviceModelEvent(db *gorm.DB, opts ...gen.DOOption) deviceModelEvent {
	_deviceModelEvent := deviceModelEvent{}

	_deviceModelEvent.deviceModelEventDo.UseDB(db, opts...)
	_deviceModelEvent.deviceModelEventDo.UseModel(&model.DeviceModelEvent{})

	tableName := _deviceModelEvent.deviceModelEventDo.TableName()
	_deviceModelEvent.ALL = field.NewAsterisk(tableName)
	_deviceModelEvent.ID = field.NewString(tableName, "id")
	_deviceModelEvent.DeviceTemplateID = field.NewString(tableName, "device_template_id")
	_deviceModelEvent.DataName = field.NewString(tableName, "data_name")
	_deviceModelEvent.DataIdentifier = field.NewString(tableName, "data_identifier")
	_deviceModelEvent.Param = field.NewString(tableName, "params")
	_deviceModelEvent.Description = field.NewString(tableName, "description")
	_deviceModelEvent.AdditionalInfo = field.NewString(tableName, "additional_info")
	_deviceModelEvent.CreatedAt = field.NewTime(tableName, "created_at")
	_deviceModelEvent.UpdatedAt = field.NewTime(tableName, "updated_at")
	_deviceModelEvent.Remark = field.NewString(tableName, "remark")
	_deviceModelEvent.TenantID = field.NewString(tableName, "tenant_id")

	_deviceModelEvent.fillFieldMap()

	return _deviceModelEvent
}

type deviceModelEvent struct {
	deviceModelEventDo

	ALL              field.Asterisk
	ID               field.String // id
	DeviceTemplateID field.String // 设备模板id
	DataName         field.String // 数据名称
	DataIdentifier   field.String // 数据标识符
	Param            field.String // 参数
	Description      field.String // 描述
	AdditionalInfo   field.String // 附加信息
	CreatedAt        field.Time   // 创建时间
	UpdatedAt        field.Time   // 更新时间
	Remark           field.String // 备注
	TenantID         field.String

	fieldMap map[string]field.Expr
}

func (d deviceModelEvent) Table(newTableName string) *deviceModelEvent {
	d.deviceModelEventDo.UseTable(newTableName)
	return d.updateTableName(newTableName)
}

func (d deviceModelEvent) As(alias string) *deviceModelEvent {
	d.deviceModelEventDo.DO = *(d.deviceModelEventDo.As(alias).(*gen.DO))
	return d.updateTableName(alias)
}

func (d *deviceModelEvent) updateTableName(table string) *deviceModelEvent {
	d.ALL = field.NewAsterisk(table)
	d.ID = field.NewString(table, "id")
	d.DeviceTemplateID = field.NewString(table, "device_template_id")
	d.DataName = field.NewString(table, "data_name")
	d.DataIdentifier = field.NewString(table, "data_identifier")
	d.Param = field.NewString(table, "params")
	d.Description = field.NewString(table, "description")
	d.AdditionalInfo = field.NewString(table, "additional_info")
	d.CreatedAt = field.NewTime(table, "created_at")
	d.UpdatedAt = field.NewTime(table, "updated_at")
	d.Remark = field.NewString(table, "remark")
	d.TenantID = field.NewString(table, "tenant_id")

	d.fillFieldMap()

	return d
}

func (d *deviceModelEvent) GetFieldByName(fieldName string) (field.OrderExpr, bool) {
	_f, ok := d.fieldMap[fieldName]
	if !ok || _f == nil {
		return nil, false
	}
	_oe, ok := _f.(field.OrderExpr)
	return _oe, ok
}

func (d *deviceModelEvent) fillFieldMap() {
	d.fieldMap = make(map[string]field.Expr, 11)
	d.fieldMap["id"] = d.ID
	d.fieldMap["device_template_id"] = d.DeviceTemplateID
	d.fieldMap["data_name"] = d.DataName
	d.fieldMap["data_identifier"] = d.DataIdentifier
	d.fieldMap["params"] = d.Param
	d.fieldMap["description"] = d.Description
	d.fieldMap["additional_info"] = d.AdditionalInfo
	d.fieldMap["created_at"] = d.CreatedAt
	d.fieldMap["updated_at"] = d.UpdatedAt
	d.fieldMap["remark"] = d.Remark
	d.fieldMap["tenant_id"] = d.TenantID
}

func (d deviceModelEvent) clone(db *gorm.DB) deviceModelEvent {
	d.deviceModelEventDo.ReplaceConnPool(db.Statement.ConnPool)
	return d
}

func (d deviceModelEvent) replaceDB(db *gorm.DB) deviceModelEvent {
	d.deviceModelEventDo.ReplaceDB(db)
	return d
}

type deviceModelEventDo struct{ gen.DO }

type IDeviceModelEventDo interface {
	gen.SubQuery
	Debug() IDeviceModelEventDo
	WithContext(ctx context.Context) IDeviceModelEventDo
	WithResult(fc func(tx gen.Dao)) gen.ResultInfo
	ReplaceDB(db *gorm.DB)
	ReadDB() IDeviceModelEventDo
	WriteDB() IDeviceModelEventDo
	As(alias string) gen.Dao
	Session(config *gorm.Session) IDeviceModelEventDo
	Columns(cols ...field.Expr) gen.Columns
	Clauses(conds ...clause.Expression) IDeviceModelEventDo
	Not(conds ...gen.Condition) IDeviceModelEventDo
	Or(conds ...gen.Condition) IDeviceModelEventDo
	Select(conds ...field.Expr) IDeviceModelEventDo
	Where(conds ...gen.Condition) IDeviceModelEventDo
	Order(conds ...field.Expr) IDeviceModelEventDo
	Distinct(cols ...field.Expr) IDeviceModelEventDo
	Omit(cols ...field.Expr) IDeviceModelEventDo
	Join(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo
	LeftJoin(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo
	RightJoin(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo
	Group(cols ...field.Expr) IDeviceModelEventDo
	Having(conds ...gen.Condition) IDeviceModelEventDo
	Limit(limit int) IDeviceModelEventDo
	Offset(offset int) IDeviceModelEventDo
	Count() (count int64, err error)
	Scopes(funcs ...func(gen.Dao) gen.Dao) IDeviceModelEventDo
	Unscoped() IDeviceModelEventDo
	Create(values ...*model.DeviceModelEvent) error
	CreateInBatches(values []*model.DeviceModelEvent, batchSize int) error
	Save(values ...*model.DeviceModelEvent) error
	First() (*model.DeviceModelEvent, error)
	Take() (*model.DeviceModelEvent, error)
	Last() (*model.DeviceModelEvent, error)
	Find() ([]*model.DeviceModelEvent, error)
	FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.DeviceModelEvent, err error)
	FindInBatches(result *[]*model.DeviceModelEvent, batchSize int, fc func(tx gen.Dao, batch int) error) error
	Pluck(column field.Expr, dest interface{}) error
	Delete(...*model.DeviceModelEvent) (info gen.ResultInfo, err error)
	Update(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	Updates(value interface{}) (info gen.ResultInfo, err error)
	UpdateColumn(column field.Expr, value interface{}) (info gen.ResultInfo, err error)
	UpdateColumnSimple(columns ...field.AssignExpr) (info gen.ResultInfo, err error)
	UpdateColumns(value interface{}) (info gen.ResultInfo, err error)
	UpdateFrom(q gen.SubQuery) gen.Dao
	Attrs(attrs ...field.AssignExpr) IDeviceModelEventDo
	Assign(attrs ...field.AssignExpr) IDeviceModelEventDo
	Joins(fields ...field.RelationField) IDeviceModelEventDo
	Preload(fields ...field.RelationField) IDeviceModelEventDo
	FirstOrInit() (*model.DeviceModelEvent, error)
	FirstOrCreate() (*model.DeviceModelEvent, error)
	FindByPage(offset int, limit int) (result []*model.DeviceModelEvent, count int64, err error)
	ScanByPage(result interface{}, offset int, limit int) (count int64, err error)
	Scan(result interface{}) (err error)
	Returning(value interface{}, columns ...string) IDeviceModelEventDo
	UnderlyingDB() *gorm.DB
	schema.Tabler
}

func (d deviceModelEventDo) Debug() IDeviceModelEventDo {
	return d.withDO(d.DO.Debug())
}

func (d deviceModelEventDo) WithContext(ctx context.Context) IDeviceModelEventDo {
	return d.withDO(d.DO.WithContext(ctx))
}

func (d deviceModelEventDo) ReadDB() IDeviceModelEventDo {
	return d.Clauses(dbresolver.Read)
}

func (d deviceModelEventDo) WriteDB() IDeviceModelEventDo {
	return d.Clauses(dbresolver.Write)
}

func (d deviceModelEventDo) Session(config *gorm.Session) IDeviceModelEventDo {
	return d.withDO(d.DO.Session(config))
}

func (d deviceModelEventDo) Clauses(conds ...clause.Expression) IDeviceModelEventDo {
	return d.withDO(d.DO.Clauses(conds...))
}

func (d deviceModelEventDo) Returning(value interface{}, columns ...string) IDeviceModelEventDo {
	return d.withDO(d.DO.Returning(value, columns...))
}

func (d deviceModelEventDo) Not(conds ...gen.Condition) IDeviceModelEventDo {
	return d.withDO(d.DO.Not(conds...))
}

func (d deviceModelEventDo) Or(conds ...gen.Condition) IDeviceModelEventDo {
	return d.withDO(d.DO.Or(conds...))
}

func (d deviceModelEventDo) Select(conds ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Select(conds...))
}

func (d deviceModelEventDo) Where(conds ...gen.Condition) IDeviceModelEventDo {
	return d.withDO(d.DO.Where(conds...))
}

func (d deviceModelEventDo) Order(conds ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Order(conds...))
}

func (d deviceModelEventDo) Distinct(cols ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Distinct(cols...))
}

func (d deviceModelEventDo) Omit(cols ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Omit(cols...))
}

func (d deviceModelEventDo) Join(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Join(table, on...))
}

func (d deviceModelEventDo) LeftJoin(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.LeftJoin(table, on...))
}

func (d deviceModelEventDo) RightJoin(table schema.Tabler, on ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.RightJoin(table, on...))
}

func (d deviceModelEventDo) Group(cols ...field.Expr) IDeviceModelEventDo {
	return d.withDO(d.DO.Group(cols...))
}

func (d deviceModelEventDo) Having(conds ...gen.Condition) IDeviceModelEventDo {
	return d.withDO(d.DO.Having(conds...))
}

func (d deviceModelEventDo) Limit(limit int) IDeviceModelEventDo {
	return d.withDO(d.DO.Limit(limit))
}

func (d deviceModelEventDo) Offset(offset int) IDeviceModelEventDo {
	return d.withDO(d.DO.Offset(offset))
}

func (d deviceModelEventDo) Scopes(funcs ...func(gen.Dao) gen.Dao) IDeviceModelEventDo {
	return d.withDO(d.DO.Scopes(funcs...))
}

func (d deviceModelEventDo) Unscoped() IDeviceModelEventDo {
	return d.withDO(d.DO.Unscoped())
}

func (d deviceModelEventDo) Create(values ...*model.DeviceModelEvent) error {
	if len(values) == 0 {
		return nil
	}
	return d.DO.Create(values)
}

func (d deviceModelEventDo) CreateInBatches(values []*model.DeviceModelEvent, batchSize int) error {
	return d.DO.CreateInBatches(values, batchSize)
}

// Save : !!! underlying implementation is different with GORM
// The method is equivalent to executing the statement: db.Clauses(clause.OnConflict{UpdateAll: true}).Create(values)
func (d deviceModelEventDo) Save(values ...*model.DeviceModelEvent) error {
	if len(values) == 0 {
		return nil
	}
	return d.DO.Save(values)
}

func (d deviceModelEventDo) First() (*model.DeviceModelEvent, error) {
	if result, err := d.DO.First(); err != nil {
		return nil, err
	} else {
		return result.(*model.DeviceModelEvent), nil
	}
}

func (d deviceModelEventDo) Take() (*model.DeviceModelEvent, error) {
	if result, err := d.DO.Take(); err != nil {
		return nil, err
	} else {
		return result.(*model.DeviceModelEvent), nil
	}
}

func (d deviceModelEventDo) Last() (*model.DeviceModelEvent, error) {
	if result, err := d.DO.Last(); err != nil {
		return nil, err
	} else {
		return result.(*model.DeviceModelEvent), nil
	}
}

func (d deviceModelEventDo) Find() ([]*model.DeviceModelEvent, error) {
	result, err := d.DO.Find()
	return result.([]*model.DeviceModelEvent), err
}

func (d deviceModelEventDo) FindInBatch(batchSize int, fc func(tx gen.Dao, batch int) error) (results []*model.DeviceModelEvent, err error) {
	buf := make([]*model.DeviceModelEvent, 0, batchSize)
	err = d.DO.FindInBatches(&buf, batchSize, func(tx gen.Dao, batch int) error {
		defer func() { results = append(results, buf...) }()
		return fc(tx, batch)
	})
	return results, err
}

func (d deviceModelEventDo) FindInBatches(result *[]*model.DeviceModelEvent, batchSize int, fc func(tx gen.Dao, batch int) error) error {
	return d.DO.FindInBatches(result, batchSize, fc)
}

func (d deviceModelEventDo) Attrs(attrs ...field.AssignExpr) IDeviceModelEventDo {
	return d.withDO(d.DO.Attrs(attrs...))
}

func (d deviceModelEventDo) Assign(attrs ...field.AssignExpr) IDeviceModelEventDo {
	return d.withDO(d.DO.Assign(attrs...))
}

func (d deviceModelEventDo) Joins(fields ...field.RelationField) IDeviceModelEventDo {
	for _, _f := range fields {
		d = *d.withDO(d.DO.Joins(_f))
	}
	return &d
}

func (d deviceModelEventDo) Preload(fields ...field.RelationField) IDeviceModelEventDo {
	for _, _f := range fields {
		d = *d.withDO(d.DO.Preload(_f))
	}
	return &d
}

func (d deviceModelEventDo) FirstOrInit() (*model.DeviceModelEvent, error) {
	if result, err := d.DO.FirstOrInit(); err != nil {
		return nil, err
	} else {
		return result.(*model.DeviceModelEvent), nil
	}
}

func (d deviceModelEventDo) FirstOrCreate() (*model.DeviceModelEvent, error) {
	if result, err := d.DO.FirstOrCreate(); err != nil {
		return nil, err
	} else {
		return result.(*model.DeviceModelEvent), nil
	}
}

func (d deviceModelEventDo) FindByPage(offset int, limit int) (result []*model.DeviceModelEvent, count int64, err error) {
	result, err = d.Offset(offset).Limit(limit).Find()
	if err != nil {
		return
	}

	if size := len(result); 0 < limit && 0 < size && size < limit {
		count = int64(size + offset)
		return
	}

	count, err = d.Offset(-1).Limit(-1).Count()
	return
}

func (d deviceModelEventDo) ScanByPage(result interface{}, offset int, limit int) (count int64, err error) {
	count, err = d.Count()
	if err != nil {
		return
	}

	err = d.Offset(offset).Limit(limit).Scan(result)
	return
}

func (d deviceModelEventDo) Scan(result interface{}) (err error) {
	return d.DO.Scan(result)
}

func (d deviceModelEventDo) Delete(models ...*model.DeviceModelEvent) (result gen.ResultInfo, err error) {
	return d.DO.Delete(models)
}

func (d *deviceModelEventDo) withDO(do gen.Dao) *deviceModelEventDo {
	d.DO = *do.(*gen.DO)
	return d
}